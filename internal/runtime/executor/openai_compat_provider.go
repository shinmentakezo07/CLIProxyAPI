package executor

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/sjson"
)

// OpenAICompatProvider implements ProviderConfig for OpenAI-compatible APIs
type OpenAICompatProvider struct {
	providerName string
}

// NewOpenAICompatProvider creates a provider for OpenAI-compatible APIs
func NewOpenAICompatProvider(providerName string) *OpenAICompatProvider {
	return &OpenAICompatProvider{providerName: providerName}
}

func (p *OpenAICompatProvider) GetIdentifier() string {
	return p.providerName
}

func (p *OpenAICompatProvider) GetCredentials(auth *cliproxyauth.Auth) (apiKey, baseURL string) {
	if auth == nil {
		return "", ""
	}
	if auth.Attributes != nil {
		baseURL = strings.TrimSpace(auth.Attributes["base_url"])
		apiKey = strings.TrimSpace(auth.Attributes["api_key"])
	}
	return apiKey, baseURL
}

func (p *OpenAICompatProvider) GetEndpoint(baseURL, model, action string, stream bool) string {
	return strings.TrimSuffix(baseURL, "/") + "/chat/completions"
}

func (p *OpenAICompatProvider) ApplyHeaders(req *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("User-Agent", "cli-proxy-openai-compat")

	// Apply custom headers from auth attributes
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)

	if stream {
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")
	} else {
		req.Header.Set("Accept", "application/json")
	}
}

func (p *OpenAICompatProvider) GetTranslatorFormat() string {
	return "openai"
}

func (p *OpenAICompatProvider) TransformRequestBody(body []byte, model string, stream bool) ([]byte, error) {
	// Set model in payload
	body, err := sjson.SetBytes(body, "model", model)
	if err != nil {
		return body, fmt.Errorf("openai compat executor: failed to set model in payload: %w", err)
	}

	return body, nil
}

func (p *OpenAICompatProvider) TransformResponseBody(body []byte) []byte {
	// No transformation needed for OpenAI-compatible responses
	return body
}

func (p *OpenAICompatProvider) ParseUsage(data []byte, stream bool) usageDetail {
	if stream {
		if detail, ok := parseOpenAIStreamUsage(data); ok {
			return detail
		}
		return usageDetail{}
	}
	return parseOpenAIUsage(data)
}
