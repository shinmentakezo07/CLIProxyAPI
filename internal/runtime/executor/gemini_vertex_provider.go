package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	vertexauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/vertex"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	vertexAPIVersion = "v1"
)

// GeminiVertexProvider implements ProviderConfig for Vertex AI
type GeminiVertexProvider struct {
	cfg *config.Config
}

func (p *GeminiVertexProvider) GetIdentifier() string {
	return "vertex"
}

func (p *GeminiVertexProvider) GetCredentials(auth *cliproxyauth.Auth) (apiKey, baseURL string) {
	// Try API key first
	apiKey, baseURL = vertexAPICreds(auth)
	if apiKey != "" {
		if baseURL == "" {
			baseURL = "https://generativelanguage.googleapis.com"
		}
		return apiKey, baseURL
	}

	// Fall back to service account - return empty apiKey to signal service account auth
	projectID, location, _, err := vertexCreds(auth)
	if err != nil {
		return "", ""
	}
	baseURL = vertexBaseURL(location)
	return "", baseURL
}

func (p *GeminiVertexProvider) GetEndpoint(baseURL, model, action string, stream bool) string {
	// Check if using service account (need project/location in URL)
	if strings.Contains(baseURL, "aiplatform.googleapis.com") {
		// Service account path - need to extract project/location from context
		// This will be handled in the executor
		return ""
	}

	// API key path - simpler URL
	vertexAction := getVertexAction(model, stream)
	if action == "countTokens" {
		vertexAction = "countTokens"
	}
	url := fmt.Sprintf("%s/%s/publishers/google/models/%s:%s", baseURL, vertexAPIVersion, model, vertexAction)
	return url
}

func (p *GeminiVertexProvider) ApplyHeaders(req *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool) {
	req.Header.Set("Content-Type", "application/json")

	if apiKey != "" {
		req.Header.Set("x-goog-api-key", apiKey)
		req.Header.Del("Authorization")
	}
	// Authorization header for service account is set separately in executor

	applyGeminiHeaders(req, auth)
}

func (p *GeminiVertexProvider) GetTranslatorFormat() string {
	return "gemini"
}

func (p *GeminiVertexProvider) TransformRequestBody(body []byte, model string, stream bool) ([]byte, error) {
	// Handle Imagen models with special request format
	if isImagenModel(model) {
		return convertToImagenRequest(body)
	}

	// Standard Gemini transformation
	body = fixGeminiImageAspectRatio(model, body)
	body, _ = sjson.SetBytes(body, "model", model)
	body, _ = sjson.DeleteBytes(body, "session_id")
	return body, nil
}

func (p *GeminiVertexProvider) TransformResponseBody(body []byte) []byte {
	// No transformation needed for standard responses
	return body
}

func (p *GeminiVertexProvider) ParseUsage(data []byte, stream bool) usageDetail {
	if stream {
		if detail, ok := parseGeminiStreamUsage(data); ok {
			return detail
		}
		return usageDetail{}
	}
	return parseGeminiUsage(data)
}

// GetServiceAccountToken retrieves OAuth2 token for service account
func (p *GeminiVertexProvider) GetServiceAccountToken(ctx context.Context, auth *cliproxyauth.Auth, saJSON []byte) (string, error) {
	if httpClient := newProxyAwareHTTPClient(ctx, p.cfg, auth, 0); httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	}
	creds, err := google.CredentialsFromJSON(ctx, saJSON, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return "", fmt.Errorf("vertex executor: parse service account json failed: %w", err)
	}
	tok, err := creds.TokenSource.Token()
	if err != nil {
		return "", fmt.Errorf("vertex executor: get access token failed: %w", err)
	}
	return tok.AccessToken, nil
}

// GetServiceAccountEndpoint builds the full Vertex AI endpoint with project/location
func (p *GeminiVertexProvider) GetServiceAccountEndpoint(projectID, location, model, action string, stream bool) string {
	baseURL := vertexBaseURL(location)
	vertexAction := getVertexAction(model, stream)
	if action == "countTokens" {
		vertexAction = "countTokens"
	}
	return fmt.Sprintf("%s/%s/projects/%s/locations/%s/publishers/google/models/%s:%s",
		baseURL, vertexAPIVersion, projectID, location, model, vertexAction)
}

// Helper functions

func isImagenModel(model string) bool {
	lowerModel := strings.ToLower(model)
	return strings.Contains(lowerModel, "imagen")
}

func getVertexAction(model string, isStream bool) string {
	if isImagenModel(model) {
		return "predict"
	}
	if isStream {
		return "streamGenerateContent"
	}
	return "generateContent"
}

func convertImagenToGeminiResponse(data []byte, model string) []byte {
	predictions := gjson.GetBytes(data, "predictions")
	if !predictions.Exists() || !predictions.IsArray() {
		return data
	}

	parts := make([]map[string]any, 0)
	for _, pred := range predictions.Array() {
		imageData := pred.Get("bytesBase64Encoded").String()
		mimeType := pred.Get("mimeType").String()
		if mimeType == "" {
			mimeType = "image/png"
		}
		if imageData != "" {
			parts = append(parts, map[string]any{
				"inlineData": map[string]any{
					"mimeType": mimeType,
					"data":     imageData,
				},
			})
		}
	}

	responseId := fmt.Sprintf("imagen-%d", time.Now().UnixNano())
	response := map[string]any{
		"candidates": []map[string]any{{
			"content": map[string]any{
				"parts": parts,
				"role":  "model",
			},
			"finishReason": "STOP",
		}},
		"responseId":   responseId,
		"modelVersion": model,
		"usageMetadata": map[string]any{
			"promptTokenCount":     0,
			"candidatesTokenCount": 0,
			"totalTokenCount":      0,
		},
	}

	result, err := json.Marshal(response)
	if err != nil {
		return data
	}
	return result
}

func convertToImagenRequest(payload []byte) ([]byte, error) {
	prompt := ""

	contentsText := gjson.GetBytes(payload, "contents.0.parts.0.text")
	if contentsText.Exists() {
		prompt = contentsText.String()
	}

	if prompt == "" {
		messagesText := gjson.GetBytes(payload, "messages.#.content")
		if messagesText.Exists() && messagesText.IsArray() {
			for _, msg := range messagesText.Array() {
				if msg.String() != "" {
					prompt = msg.String()
					break
				}
			}
		}
	}

	if prompt == "" {
		directPrompt := gjson.GetBytes(payload, "prompt")
		if directPrompt.Exists() {
			prompt = directPrompt.String()
		}
	}

	if prompt == "" {
		return nil, fmt.Errorf("imagen: no prompt found in request")
	}

	imagenReq := map[string]any{
		"instances": []map[string]any{
			{"prompt": prompt},
		},
		"parameters": map[string]any{
			"sampleCount": 1,
		},
	}

	if aspectRatio := gjson.GetBytes(payload, "aspectRatio"); aspectRatio.Exists() {
		imagenReq["parameters"].(map[string]any)["aspectRatio"] = aspectRatio.String()
	}
	if sampleCount := gjson.GetBytes(payload, "sampleCount"); sampleCount.Exists() {
		imagenReq["parameters"].(map[string]any)["sampleCount"] = int(sampleCount.Int())
	}
	if negativePrompt := gjson.GetBytes(payload, "negativePrompt"); negativePrompt.Exists() {
		imagenReq["instances"].([]map[string]any)[0]["negativePrompt"] = negativePrompt.String()
	}

	return json.Marshal(imagenReq)
}

func vertexCreds(a *cliproxyauth.Auth) (projectID, location string, serviceAccountJSON []byte, err error) {
	if a == nil || a.Metadata == nil {
		return "", "", nil, fmt.Errorf("vertex executor: missing auth metadata")
	}
	if v, ok := a.Metadata["project_id"].(string); ok {
		projectID = strings.TrimSpace(v)
	}
	if projectID == "" {
		if v, ok := a.Metadata["project"].(string); ok {
			projectID = strings.TrimSpace(v)
		}
	}
	if projectID == "" {
		return "", "", nil, fmt.Errorf("vertex executor: missing project_id in credentials")
	}
	if v, ok := a.Metadata["location"].(string); ok && strings.TrimSpace(v) != "" {
		location = strings.TrimSpace(v)
	} else {
		location = "us-central1"
	}
	var sa map[string]any
	if raw, ok := a.Metadata["service_account"].(map[string]any); ok {
		sa = raw
	}
	if sa == nil {
		return "", "", nil, fmt.Errorf("vertex executor: missing service_account in credentials")
	}
	normalized, err := vertexauth.NormalizeServiceAccountMap(sa)
	if err != nil {
		return "", "", nil, fmt.Errorf("vertex executor: %w", err)
	}
	saJSON, err := json.Marshal(normalized)
	if err != nil {
		return "", "", nil, fmt.Errorf("vertex executor: marshal service_account failed: %w", err)
	}
	return projectID, location, saJSON, nil
}

func vertexAPICreds(a *cliproxyauth.Auth) (apiKey, baseURL string) {
	if a == nil {
		return "", ""
	}
	if a.Attributes != nil {
		apiKey = a.Attributes["api_key"]
		baseURL = a.Attributes["base_url"]
	}
	if apiKey == "" && a.Metadata != nil {
		if v, ok := a.Metadata["access_token"].(string); ok {
			apiKey = v
		}
	}
	return
}

func vertexBaseURL(location string) string {
	loc := strings.TrimSpace(location)
	if loc == "" {
		loc = "us-central1"
	} else if loc == "global" {
		return "https://aiplatform.googleapis.com"
	}
	return fmt.Sprintf("https://%s-aiplatform.googleapis.com", loc)
}

func applyGeminiHeaders(req *http.Request, auth *cliproxyauth.Auth) {
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
}
