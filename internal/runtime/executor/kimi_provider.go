package executor

import (
	"fmt"
	"net/http"
	"strings"

	kimiauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/kimi"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// KimiProvider implements ProviderConfig for Kimi API
type KimiProvider struct{}

func (p *KimiProvider) GetIdentifier() string {
	return "kimi"
}

func (p *KimiProvider) GetCredentials(auth *cliproxyauth.Auth) (apiKey, baseURL string) {
	if auth == nil {
		return "", kimiauth.KimiAPIBaseURL
	}

	// Check metadata first (OAuth flow stores tokens here)
	if auth.Metadata != nil {
		if v, ok := auth.Metadata["access_token"].(string); ok && strings.TrimSpace(v) != "" {
			return v, kimiauth.KimiAPIBaseURL
		}
	}

	// Fallback to attributes (API key style)
	if auth.Attributes != nil {
		if v := auth.Attributes["access_token"]; v != "" {
			return v, kimiauth.KimiAPIBaseURL
		}
		if v := auth.Attributes["api_key"]; v != "" {
			return v, kimiauth.KimiAPIBaseURL
		}
	}

	return "", kimiauth.KimiAPIBaseURL
}

func (p *KimiProvider) GetEndpoint(baseURL, model, action string, stream bool) string {
	return baseURL + "/v1/chat/completions"
}

func (p *KimiProvider) ApplyHeaders(req *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool) {
	applyKimiHeadersWithAuth(req, apiKey, stream, auth)
}

func (p *KimiProvider) GetTranslatorFormat() string {
	return "openai"
}

func (p *KimiProvider) TransformRequestBody(body []byte, model string, stream bool) ([]byte, error) {
	// Strip kimi- prefix for upstream API
	upstreamModel := stripKimiPrefix(model)
	body, err := sjson.SetBytes(body, "model", upstreamModel)
	if err != nil {
		return body, fmt.Errorf("kimi executor: failed to set model in payload: %w", err)
	}

	// Normalize tool message links
	body, err = normalizeKimiToolMessageLinks(body)
	if err != nil {
		return body, err
	}

	// Set stream_options for streaming requests
	if stream {
		body, err = sjson.SetBytes(body, "stream_options.include_usage", true)
		if err != nil {
			return body, fmt.Errorf("kimi executor: failed to set stream_options in payload: %w", err)
		}
	}

	return body, nil
}

func (p *KimiProvider) TransformResponseBody(body []byte) []byte {
	// No transformation needed for Kimi responses
	return body
}

func (p *KimiProvider) ParseUsage(data []byte, stream bool) usageDetail {
	if stream {
		if detail, ok := parseOpenAIStreamUsage(data); ok {
			return detail
		}
		return usageDetail{}
	}
	return parseOpenAIUsage(data)
}

// stripKimiPrefix removes the "kimi-" prefix from model names for the upstream API.
func stripKimiPrefix(model string) string {
	model = strings.TrimSpace(model)
	if strings.HasPrefix(strings.ToLower(model), "kimi-") {
		return model[5:]
	}
	return model
}

func normalizeKimiToolMessageLinks(body []byte) ([]byte, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return body, nil
	}

	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body, nil
	}

	out := body
	pending := make([]string, 0)
	patched := 0
	patchedReasoning := 0
	ambiguous := 0
	latestReasoning := ""
	hasLatestReasoning := false

	removePending := func(id string) {
		for idx := range pending {
			if pending[idx] != id {
				continue
			}
			pending = append(pending[:idx], pending[idx+1:]...)
			return
		}
	}

	msgs := messages.Array()
	for msgIdx := range msgs {
		msg := msgs[msgIdx]
		role := strings.TrimSpace(msg.Get("role").String())
		switch role {
		case "assistant":
			reasoning := msg.Get("reasoning_content")
			if reasoning.Exists() {
				reasoningText := reasoning.String()
				if strings.TrimSpace(reasoningText) != "" {
					latestReasoning = reasoningText
					hasLatestReasoning = true
				}
			}

			toolCalls := msg.Get("tool_calls")
			if !toolCalls.Exists() || !toolCalls.IsArray() || len(toolCalls.Array()) == 0 {
				continue
			}

			if !reasoning.Exists() || strings.TrimSpace(reasoning.String()) == "" {
				reasoningText := fallbackAssistantReasoning(msg, hasLatestReasoning, latestReasoning)
				path := fmt.Sprintf("messages.%d.reasoning_content", msgIdx)
				next, err := sjson.SetBytes(out, path, reasoningText)
				if err != nil {
					return body, fmt.Errorf("kimi executor: failed to set assistant reasoning_content: %w", err)
				}
				out = next
				patchedReasoning++
			}

			for _, tc := range toolCalls.Array() {
				id := strings.TrimSpace(tc.Get("id").String())
				if id == "" {
					continue
				}
				pending = append(pending, id)
			}
		case "tool":
			toolCallID := strings.TrimSpace(msg.Get("tool_call_id").String())
			if toolCallID == "" {
				toolCallID = strings.TrimSpace(msg.Get("call_id").String())
				if toolCallID != "" {
					path := fmt.Sprintf("messages.%d.tool_call_id", msgIdx)
					next, err := sjson.SetBytes(out, path, toolCallID)
					if err != nil {
						return body, fmt.Errorf("kimi executor: failed to set tool_call_id from call_id: %w", err)
					}
					out = next
					patched++
				}
			}
			if toolCallID == "" {
				if len(pending) == 1 {
					toolCallID = pending[0]
					path := fmt.Sprintf("messages.%d.tool_call_id", msgIdx)
					next, err := sjson.SetBytes(out, path, toolCallID)
					if err != nil {
						return body, fmt.Errorf("kimi executor: failed to infer tool_call_id: %w", err)
					}
					out = next
					patched++
				} else if len(pending) > 1 {
					ambiguous++
				}
			}
			if toolCallID != "" {
				removePending(toolCallID)
			}
		}
	}

	return out, nil
}

func fallbackAssistantReasoning(msg gjson.Result, hasLatest bool, latest string) string {
	if hasLatest && strings.TrimSpace(latest) != "" {
		return latest
	}

	content := msg.Get("content")
	if content.Type == gjson.String {
		if text := strings.TrimSpace(content.String()); text != "" {
			return text
		}
	}
	if content.IsArray() {
		parts := make([]string, 0, len(content.Array()))
		for _, item := range content.Array() {
			text := strings.TrimSpace(item.Get("text").String())
			if text == "" {
				continue
			}
			parts = append(parts, text)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}

	return "[reasoning unavailable]"
}
