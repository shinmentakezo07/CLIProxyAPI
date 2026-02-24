package executor

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	glEndpoint          = "https://generativelanguage.googleapis.com"
	glAPIVersion        = "v1beta"
	streamScanBuffer    = 52_428_800
	streamScannerBuffer = streamScanBuffer
)

// GeminiProvider implements ProviderConfig for Gemini API
type GeminiProvider struct{}

func (p *GeminiProvider) GetIdentifier() string {
	return "gemini"
}

func (p *GeminiProvider) GetCredentials(auth *cliproxyauth.Auth) (apiKey, baseURL string) {
	if auth == nil {
		return "", glEndpoint
	}

	var bearer string
	if auth.Attributes != nil {
		if v := auth.Attributes["api_key"]; v != "" {
			apiKey = v
		}
	}

	if auth.Metadata != nil {
		// GeminiTokenStorage.Token is a map that may contain access_token
		if v, ok := auth.Metadata["access_token"].(string); ok && v != "" {
			bearer = v
		}
		if token, ok := auth.Metadata["token"].(map[string]any); ok && token != nil {
			if v, ok2 := token["access_token"].(string); ok2 && v != "" {
				bearer = v
			}
		}
	}

	// Return bearer token as apiKey if no API key is present
	if apiKey == "" && bearer != "" {
		apiKey = "bearer:" + bearer
	}

	baseURL = glEndpoint
	if auth.Attributes != nil {
		if custom := strings.TrimSpace(auth.Attributes["base_url"]); custom != "" {
			baseURL = strings.TrimRight(custom, "/")
		}
	}

	return apiKey, baseURL
}

func (p *GeminiProvider) GetEndpoint(baseURL, model, action string, stream bool) string {
	if stream {
		return fmt.Sprintf("%s/%s/models/%s:streamGenerateContent?alt=sse", baseURL, glAPIVersion, model)
	}
	return fmt.Sprintf("%s/%s/models/%s:generateContent", baseURL, glAPIVersion, model)
}

func (p *GeminiProvider) ApplyHeaders(req *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool) {
	req.Header.Set("Content-Type", "application/json")

	// Handle dual authentication: API key OR bearer token
	if strings.HasPrefix(apiKey, "bearer:") {
		bearer := strings.TrimPrefix(apiKey, "bearer:")
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Del("x-goog-api-key")
	} else if apiKey != "" {
		req.Header.Set("x-goog-api-key", apiKey)
		req.Header.Del("Authorization")
	}

	// Apply custom headers from auth attributes
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
}

func (p *GeminiProvider) GetTranslatorFormat() string {
	return "gemini"
}

func (p *GeminiProvider) TransformRequestBody(body []byte, model string, stream bool) ([]byte, error) {
	var err error

	// Fix image aspect ratio for specific model
	body = fixGeminiImageAspectRatio(model, body)

	// Set model in payload
	body, err = sjson.SetBytes(body, "model", model)
	if err != nil {
		return body, fmt.Errorf("gemini executor: failed to set model in payload: %w", err)
	}

	// Delete session_id (not supported by Gemini API)
	body, _ = sjson.DeleteBytes(body, "session_id")

	return body, nil
}

func (p *GeminiProvider) TransformResponseBody(body []byte) []byte {
	// Filter SSE usage metadata for streaming responses
	return FilterSSEUsageMetadata(body)
}

func (p *GeminiProvider) ParseUsage(data []byte, stream bool) usageDetail {
	if stream {
		// For streaming, extract JSON payload from SSE format
		payload := jsonPayload(data)
		if len(payload) == 0 {
			return usageDetail{}
		}
		if detail, ok := parseGeminiStreamUsage(payload); ok {
			return detail
		}
		return usageDetail{}
	}
	return parseGeminiUsage(data)
}

func fixGeminiImageAspectRatio(modelName string, rawJSON []byte) []byte {
	if modelName != "gemini-2.5-flash-image-preview" {
		return rawJSON
	}

	aspectRatioResult := gjson.GetBytes(rawJSON, "generationConfig.imageConfig.aspectRatio")
	if !aspectRatioResult.Exists() {
		return rawJSON
	}

	contents := gjson.GetBytes(rawJSON, "contents")
	contentArray := contents.Array()
	if len(contentArray) == 0 {
		return rawJSON
	}

	// Check if any content has inline data
	hasInlineData := false
loopContent:
	for i := 0; i < len(contentArray); i++ {
		parts := contentArray[i].Get("parts").Array()
		for j := 0; j < len(parts); j++ {
			if parts[j].Get("inlineData").Exists() {
				hasInlineData = true
				break loopContent
			}
		}
	}

	if hasInlineData {
		rawJSON, _ = sjson.DeleteBytes(rawJSON, "generationConfig.imageConfig")
		return rawJSON
	}

	// Create empty white image with specified aspect ratio
	emptyImageBase64ed, _ := util.CreateWhiteImageBase64(aspectRatioResult.String())
	emptyImagePart := `{"inlineData":{"mime_type":"image/png","data":""}}`
	emptyImagePart, _ = sjson.Set(emptyImagePart, "inlineData.data", emptyImageBase64ed)

	// Build new parts array with instruction and empty image
	newPartsJson := `[]`
	newPartsJson, _ = sjson.SetRaw(newPartsJson, "-1", `{"text": "Based on the following requirements, create an image within the uploaded picture. The new content *MUST* completely cover the entire area of the original picture, maintaining its exact proportions, and *NO* blank areas should appear."}`)
	newPartsJson, _ = sjson.SetRaw(newPartsJson, "-1", emptyImagePart)

	// Append original parts
	parts := contentArray[0].Get("parts").Array()
	for j := 0; j < len(parts); j++ {
		newPartsJson, _ = sjson.SetRaw(newPartsJson, "-1", parts[j].Raw)
	}

	rawJSON, _ = sjson.SetRawBytes(rawJSON, "contents.0.parts", []byte(newPartsJson))
	rawJSON, _ = sjson.SetRawBytes(rawJSON, "generationConfig.responseModalities", []byte(`["IMAGE", "TEXT"]`))
	rawJSON, _ = sjson.DeleteBytes(rawJSON, "generationConfig.imageConfig")

	return rawJSON
}
