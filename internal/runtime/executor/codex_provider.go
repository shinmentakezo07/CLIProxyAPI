package executor

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/misc"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	codexClientVersion = "0.101.0"
	codexUserAgent     = "codex_cli_rs/0.101.0 (Mac OS 26.0.1; arm64) Apple_Terminal/464"
	codexDefaultURL    = "https://chatgpt.com/backend-api/codex"
)

var dataTag = []byte("data:")

// CodexProvider implements ProviderConfig for Codex API
type CodexProvider struct {
	cfg *config.Config
}

func NewCodexProvider(cfg *config.Config) *CodexProvider {
	return &CodexProvider{cfg: cfg}
}

func (p *CodexProvider) GetIdentifier() string {
	return "codex"
}

func (p *CodexProvider) GetCredentials(auth *cliproxyauth.Auth) (apiKey, baseURL string) {
	if auth == nil {
		return "", codexDefaultURL
	}

	if auth.Attributes != nil {
		apiKey = auth.Attributes["api_key"]
		baseURL = auth.Attributes["base_url"]
	}

	if apiKey == "" && auth.Metadata != nil {
		if v, ok := auth.Metadata["access_token"].(string); ok {
			apiKey = v
		}
	}

	if baseURL == "" {
		baseURL = codexDefaultURL
	}

	return apiKey, baseURL
}

func (p *CodexProvider) GetEndpoint(baseURL, model, action string, stream bool) string {
	return strings.TrimSuffix(baseURL, "/") + "/responses"
}

func (p *CodexProvider) ApplyHeaders(req *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	var ginHeaders http.Header
	if ginCtx, ok := req.Context().Value("gin").(*gin.Context); ok && ginCtx != nil && ginCtx.Request != nil {
		ginHeaders = ginCtx.Request.Header
	}

	misc.EnsureHeader(req.Header, ginHeaders, "Version", codexClientVersion)
	misc.EnsureHeader(req.Header, ginHeaders, "Session_id", uuid.NewString())
	misc.EnsureHeader(req.Header, ginHeaders, "User-Agent", codexUserAgent)
	misc.EnsureHeader(req.Header, ginHeaders, "x-codex-beta-features", "")
	misc.EnsureHeader(req.Header, ginHeaders, "x-codex-turn-state", "")
	misc.EnsureHeader(req.Header, ginHeaders, "x-codex-turn-metadata", "")
	misc.EnsureHeader(req.Header, ginHeaders, "x-responsesapi-include-timing-metrics", "")

	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("Connection", "Keep-Alive")

	// Check if using API key or OAuth
	isAPIKey := false
	if auth != nil && auth.Attributes != nil {
		if v := strings.TrimSpace(auth.Attributes["api_key"]); v != "" {
			isAPIKey = true
		}
	}

	if !isAPIKey {
		req.Header.Set("Originator", "codex_cli_rs")
		if auth != nil && auth.Metadata != nil {
			if accountID, ok := auth.Metadata["account_id"].(string); ok {
				req.Header.Set("Chatgpt-Account-Id", accountID)
			}
		}
	}

	// Apply custom headers from auth attributes
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
}

func (p *CodexProvider) GetTranslatorFormat() string {
	return "codex"
}

func (p *CodexProvider) TransformRequestBody(body []byte, model string, stream bool) ([]byte, error) {
	return normalizeCodexRequestBody(body, model, stream)
}

func normalizeCodexRequestBody(body []byte, model string, stream bool) ([]byte, error) {
	var err error

	// Set model in payload
	body, err = sjson.SetBytes(body, "model", model)
	if err != nil {
		return body, fmt.Errorf("codex executor: failed to set model in payload: %w", err)
	}

	// Set stream flag
	body, _ = sjson.SetBytes(body, "stream", stream)

	// Accept OpenAI-style alias and normalize it to Codex/Responses format.
	if !gjson.GetBytes(body, "reasoning.effort").Exists() {
		if effort := strings.TrimSpace(gjson.GetBytes(body, "reasoning_effort").String()); effort != "" {
			body, _ = sjson.SetBytes(body, "reasoning.effort", effort)
		}
	}
	body, _ = sjson.DeleteBytes(body, "reasoning_effort")

	// Delete Codex-specific fields that shouldn't be sent
	body, _ = sjson.DeleteBytes(body, "previous_response_id")
	body, _ = sjson.DeleteBytes(body, "prompt_cache_retention")
	body, _ = sjson.DeleteBytes(body, "safety_identifier")

	// Ensure instructions field exists
	if !gjson.GetBytes(body, "instructions").Exists() {
		body, _ = sjson.SetBytes(body, "instructions", "")
	}

	return body, nil
}

func (p *CodexProvider) TransformResponseBody(body []byte) []byte {
	// No transformation needed for Codex responses
	return body
}

func (p *CodexProvider) ParseUsage(data []byte, stream bool) usageDetail {
	// For Codex, usage is in response.completed events
	if payload, ok := codexCompletedEventPayload(data); ok {
		if detail, ok := parseCodexUsage(payload); ok {
			return detail
		}
	}
	return usageDetail{}
}

func codexCompletedEventPayload(data []byte) ([]byte, bool) {
	if !bytes.HasPrefix(data, dataTag) {
		return nil, false
	}
	payload := bytes.TrimSpace(data[len(dataTag):])
	if gjson.GetBytes(payload, "type").String() != "response.completed" {
		return nil, false
	}
	return payload, true
}
