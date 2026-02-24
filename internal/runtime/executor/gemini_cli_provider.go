package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/misc"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/geminicli"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	codeAssistEndpoint      = "https://cloudcode-pa.googleapis.com"
	codeAssistVersion       = "v1internal"
	geminiOAuthClientID     = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	geminiOAuthClientSecret = "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"
)

var geminiOAuthScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
}

// GeminiCLIProvider implements ProviderConfig for Gemini CLI (Cloud Code Assist)
type GeminiCLIProvider struct {
	cfg *config.Config
}

func (p *GeminiCLIProvider) GetIdentifier() string {
	return "gemini-cli"
}

func (p *GeminiCLIProvider) GetCredentials(auth *cliproxyauth.Auth) (apiKey, baseURL string) {
	// Gemini CLI uses OAuth2 tokens, not API keys
	// Return empty apiKey and the Code Assist endpoint
	return "", codeAssistEndpoint
}

func (p *GeminiCLIProvider) GetEndpoint(baseURL, model, action string, stream bool) string {
	if action == "countTokens" {
		return fmt.Sprintf("%s/%s:countTokens", baseURL, codeAssistVersion)
	}
	if stream {
		return fmt.Sprintf("%s/%s:streamGenerateContent", baseURL, codeAssistVersion)
	}
	return fmt.Sprintf("%s/%s:generateContent", baseURL, codeAssistVersion)
}

func (p *GeminiCLIProvider) ApplyHeaders(req *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool) {
	req.Header.Set("Content-Type", "application/json")

	// Apply Gemini CLI specific headers
	var ginHeaders http.Header
	if ginCtx, ok := req.Context().Value("gin").(*gin.Context); ok && ginCtx != nil && ginCtx.Request != nil {
		ginHeaders = ginCtx.Request.Header
	}

	misc.EnsureHeader(req.Header, ginHeaders, "User-Agent", "google-api-nodejs-client/9.15.1")
	misc.EnsureHeader(req.Header, ginHeaders, "X-Goog-Api-Client", "gl-node/22.17.0")
	misc.EnsureHeader(req.Header, ginHeaders, "Client-Metadata", "ideType=IDE_UNSPECIFIED,platform=PLATFORM_UNSPECIFIED,pluginType=GEMINI")

	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
}

func (p *GeminiCLIProvider) GetTranslatorFormat() string {
	return "gemini-cli"
}

func (p *GeminiCLIProvider) TransformRequestBody(body []byte, model string, stream bool) ([]byte, error) {
	// Fix image aspect ratio for specific model
	body = fixGeminiCLIImageAspectRatio(model, body)
	return body, nil
}

func (p *GeminiCLIProvider) TransformResponseBody(body []byte) []byte {
	// No transformation needed for Gemini CLI
	return body
}

func (p *GeminiCLIProvider) ParseUsage(data []byte, stream bool) usageDetail {
	if stream {
		if detail, ok := parseGeminiCLIStreamUsage(data); ok {
			return detail
		}
		return usageDetail{}
	}
	return parseGeminiCLIUsage(data)
}

// PrepareTokenSource creates an OAuth2 token source for Gemini CLI authentication
func (p *GeminiCLIProvider) PrepareTokenSource(ctx context.Context, auth *cliproxyauth.Auth) (oauth2.TokenSource, map[string]any, error) {
	metadata := geminiOAuthMetadata(auth)
	if auth == nil || metadata == nil {
		return nil, nil, fmt.Errorf("gemini-cli auth metadata missing")
	}

	var base map[string]any
	if tokenRaw, ok := metadata["token"].(map[string]any); ok && tokenRaw != nil {
		base = cloneMap(tokenRaw)
	} else {
		base = make(map[string]any)
	}

	var token oauth2.Token
	if len(base) > 0 {
		if raw, err := json.Marshal(base); err == nil {
			_ = json.Unmarshal(raw, &token)
		}
	}

	if token.AccessToken == "" {
		token.AccessToken = stringValue(metadata, "access_token")
	}
	if token.RefreshToken == "" {
		token.RefreshToken = stringValue(metadata, "refresh_token")
	}
	if token.TokenType == "" {
		token.TokenType = stringValue(metadata, "token_type")
	}
	if token.Expiry.IsZero() {
		if expiry := stringValue(metadata, "expiry"); expiry != "" {
			if ts, err := time.Parse(time.RFC3339, expiry); err == nil {
				token.Expiry = ts
			}
		}
	}

	conf := &oauth2.Config{
		ClientID:     geminiOAuthClientID,
		ClientSecret: geminiOAuthClientSecret,
		Scopes:       geminiOAuthScopes,
		Endpoint:     google.Endpoint,
	}

	ctxToken := ctx
	if httpClient := newProxyAwareHTTPClient(ctx, p.cfg, auth, 0); httpClient != nil {
		ctxToken = context.WithValue(ctxToken, oauth2.HTTPClient, httpClient)
	}

	src := conf.TokenSource(ctxToken, &token)
	currentToken, err := src.Token()
	if err != nil {
		return nil, nil, err
	}
	updateGeminiCLITokenMetadata(auth, base, currentToken)
	return oauth2.ReuseTokenSource(currentToken, src), base, nil
}

// UpdateTokenMetadata updates auth metadata with refreshed OAuth2 token
func (p *GeminiCLIProvider) UpdateTokenMetadata(auth *cliproxyauth.Auth, base map[string]any, tok *oauth2.Token) {
	updateGeminiCLITokenMetadata(auth, base, tok)
}

// GetProjectID extracts the project ID from auth metadata
func (p *GeminiCLIProvider) GetProjectID(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	if runtime := auth.Runtime; runtime != nil {
		if virtual, ok := runtime.(*geminicli.VirtualCredential); ok && virtual != nil {
			return strings.TrimSpace(virtual.ProjectID)
		}
	}
	return strings.TrimSpace(stringValue(auth.Metadata, "project_id"))
}

// GetFallbackModels returns the list of fallback models to try on rate limit
func (p *GeminiCLIProvider) GetFallbackModels(baseModel string) []string {
	models := cliPreviewFallbackOrder(baseModel)
	if len(models) == 0 || models[0] != baseModel {
		models = append([]string{baseModel}, models...)
	}
	return models
}

// Helper functions

func updateGeminiCLITokenMetadata(auth *cliproxyauth.Auth, base map[string]any, tok *oauth2.Token) {
	if auth == nil || tok == nil {
		return
	}
	merged := buildGeminiTokenMap(base, tok)
	fields := buildGeminiTokenFields(tok, merged)
	shared := geminicli.ResolveSharedCredential(auth.Runtime)
	if shared != nil {
		snapshot := shared.MergeMetadata(fields)
		if !geminicli.IsVirtual(auth.Runtime) {
			auth.Metadata = snapshot
		}
		return
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	for k, v := range fields {
		auth.Metadata[k] = v
	}
}

func buildGeminiTokenMap(base map[string]any, tok *oauth2.Token) map[string]any {
	merged := cloneMap(base)
	if merged == nil {
		merged = make(map[string]any)
	}
	if raw, err := json.Marshal(tok); err == nil {
		var tokenMap map[string]any
		if err = json.Unmarshal(raw, &tokenMap); err == nil {
			for k, v := range tokenMap {
				merged[k] = v
			}
		}
	}
	return merged
}

func buildGeminiTokenFields(tok *oauth2.Token, merged map[string]any) map[string]any {
	fields := make(map[string]any, 5)
	if tok.AccessToken != "" {
		fields["access_token"] = tok.AccessToken
	}
	if tok.TokenType != "" {
		fields["token_type"] = tok.TokenType
	}
	if tok.RefreshToken != "" {
		fields["refresh_token"] = tok.RefreshToken
	}
	if !tok.Expiry.IsZero() {
		fields["expiry"] = tok.Expiry.Format(time.RFC3339)
	}
	if len(merged) > 0 {
		fields["token"] = cloneMap(merged)
	}
	return fields
}

func geminiOAuthMetadata(auth *cliproxyauth.Auth) map[string]any {
	if auth == nil {
		return nil
	}
	if shared := geminicli.ResolveSharedCredential(auth.Runtime); shared != nil {
		if snapshot := shared.MetadataSnapshot(); len(snapshot) > 0 {
			return snapshot
		}
	}
	return auth.Metadata
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringValue(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		switch typed := v.(type) {
		case string:
			return typed
		case fmt.Stringer:
			return typed.String()
		}
	}
	return ""
}

func cliPreviewFallbackOrder(model string) []string {
	switch model {
	case "gemini-2.5-pro":
		return []string{
			// "gemini-2.5-pro-preview-05-06",
			// "gemini-2.5-pro-preview-06-05",
		}
	case "gemini-2.5-flash":
		return []string{
			// "gemini-2.5-flash-preview-04-17",
			// "gemini-2.5-flash-preview-05-20",
		}
	case "gemini-2.5-flash-lite":
		return []string{
			// "gemini-2.5-flash-lite-preview-06-17",
		}
	default:
		return nil
	}
}

func fixGeminiCLIImageAspectRatio(modelName string, rawJSON []byte) []byte {
	if modelName != "gemini-2.5-flash-image-preview" {
		return rawJSON
	}

	aspectRatioResult := gjson.GetBytes(rawJSON, "request.generationConfig.imageConfig.aspectRatio")
	if !aspectRatioResult.Exists() {
		return rawJSON
	}

	contents := gjson.GetBytes(rawJSON, "request.contents")
	contentArray := contents.Array()
	if len(contentArray) == 0 {
		return rawJSON
	}

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
		rawJSON, _ = sjson.DeleteBytes(rawJSON, "request.generationConfig.imageConfig")
		return rawJSON
	}

	emptyImageBase64ed, _ := util.CreateWhiteImageBase64(aspectRatioResult.String())
	emptyImagePart := `{"inlineData":{"mime_type":"image/png","data":""}}`
	emptyImagePart, _ = sjson.Set(emptyImagePart, "inlineData.data", emptyImageBase64ed)
	newPartsJson := `[]`
	newPartsJson, _ = sjson.SetRaw(newPartsJson, "-1", `{"text": "Based on the following requirements, create an image within the uploaded picture. The new content *MUST* completely cover the entire area of the original picture, maintaining its exact proportions, and *NO* blank areas should appear."}`)
	newPartsJson, _ = sjson.SetRaw(newPartsJson, "-1", emptyImagePart)

	parts := contentArray[0].Get("parts").Array()
	for j := 0; j < len(parts); j++ {
		newPartsJson, _ = sjson.SetRaw(newPartsJson, "-1", parts[j].Raw)
	}

	rawJSON, _ = sjson.SetRawBytes(rawJSON, "request.contents.0.parts", []byte(newPartsJson))
	rawJSON, _ = sjson.SetRawBytes(rawJSON, "request.generationConfig.responseModalities", []byte(`["IMAGE", "TEXT"]`))
	rawJSON, _ = sjson.DeleteBytes(rawJSON, "request.generationConfig.imageConfig")

	return rawJSON
}

func newGeminiStatusErr(statusCode int, body []byte) statusErr {
	err := statusErr{code: statusCode, msg: string(body)}
	if statusCode == http.StatusTooManyRequests {
		if retryAfter, parseErr := parseRetryDelay(body); parseErr == nil && retryAfter != nil {
			err.retryAfter = retryAfter
		}
	}
	return err
}

func parseRetryDelay(errorBody []byte) (*time.Duration, error) {
	details := gjson.GetBytes(errorBody, "error.details")
	if details.Exists() && details.IsArray() {
		for _, detail := range details.Array() {
			typeVal := detail.Get("@type").String()
			if typeVal == "type.googleapis.com/google.rpc.RetryInfo" {
				retryDelay := detail.Get("retryDelay").String()
				if retryDelay != "" {
					duration, err := time.ParseDuration(retryDelay)
					if err != nil {
						return nil, fmt.Errorf("failed to parse duration")
					}
					return &duration, nil
				}
			}
		}

		for _, detail := range details.Array() {
			typeVal := detail.Get("@type").String()
			if typeVal == "type.googleapis.com/google.rpc.ErrorInfo" {
				quotaResetDelay := detail.Get("metadata.quotaResetDelay").String()
				if quotaResetDelay != "" {
					duration, err := time.ParseDuration(quotaResetDelay)
					if err == nil {
						return &duration, nil
					}
				}
			}
		}
	}

	message := gjson.GetBytes(errorBody, "error.message").String()
	if message != "" {
		re := regexp.MustCompile(`after\s+(\d+)s\.?`)
		if matches := re.FindStringSubmatch(message); len(matches) > 1 {
			seconds, err := strconv.Atoi(matches[1])
			if err == nil {
				duration := time.Duration(seconds) * time.Second
				return &duration, nil
			}
		}
	}

	return nil, fmt.Errorf("no RetryInfo found")
}
