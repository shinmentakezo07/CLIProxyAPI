package executor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	iflowauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/iflow"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	iflowDefaultEndpoint = "/chat/completions"
	iflowUserAgent       = "iFlow-Cli"
)

// IFlowProvider implements ProviderConfig for iFlow API
type IFlowProvider struct{}

func (p *IFlowProvider) GetIdentifier() string {
	return "iflow"
}

func (p *IFlowProvider) GetCredentials(auth *cliproxyauth.Auth) (apiKey, baseURL string) {
	if auth == nil {
		return "", iflowauth.DefaultAPIBaseURL
	}

	if auth.Attributes != nil {
		if v := strings.TrimSpace(auth.Attributes["api_key"]); v != "" {
			apiKey = v
		}
		if v := strings.TrimSpace(auth.Attributes["base_url"]); v != "" {
			baseURL = v
		}
	}

	if apiKey == "" && auth.Metadata != nil {
		if v, ok := auth.Metadata["api_key"].(string); ok {
			apiKey = strings.TrimSpace(v)
		}
	}

	if baseURL == "" && auth.Metadata != nil {
		if v, ok := auth.Metadata["base_url"].(string); ok {
			baseURL = strings.TrimSpace(v)
		}
	}

	if baseURL == "" {
		baseURL = iflowauth.DefaultAPIBaseURL
	}

	return apiKey, baseURL
}

func (p *IFlowProvider) GetEndpoint(baseURL, model, action string, stream bool) string {
	return strings.TrimSuffix(baseURL, "/") + iflowDefaultEndpoint
}

func (p *IFlowProvider) ApplyHeaders(req *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", iflowUserAgent)

	// Generate session-id
	sessionID := "session-" + uuid.New().String()
	req.Header.Set("session-id", sessionID)

	// Generate timestamp and signature
	timestamp := time.Now().UnixMilli()
	req.Header.Set("x-iflow-timestamp", fmt.Sprintf("%d", timestamp))

	signature := createIFlowSignature(iflowUserAgent, sessionID, timestamp, apiKey)
	if signature != "" {
		req.Header.Set("x-iflow-signature", signature)
	}

	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
}

func (p *IFlowProvider) GetTranslatorFormat() string {
	return "openai"
}

func (p *IFlowProvider) TransformRequestBody(body []byte, model string, stream bool) ([]byte, error) {
	var err error

	// Set model in payload
	body, err = sjson.SetBytes(body, "model", model)
	if err != nil {
		return body, fmt.Errorf("iflow executor: failed to set model in payload: %w", err)
	}

	// Preserve reasoning_content in messages for models that support thinking
	body = preserveReasoningContentInMessages(body)

	// Ensure tools array exists for streaming to avoid provider quirks
	if stream {
		toolsResult := gjson.GetBytes(body, "tools")
		if toolsResult.Exists() && toolsResult.IsArray() && len(toolsResult.Array()) == 0 {
			body = ensureToolsArray(body)
		}
	}

	return body, nil
}

func (p *IFlowProvider) TransformResponseBody(body []byte) []byte {
	// No transformation needed for iFlow responses
	return body
}

func (p *IFlowProvider) ParseUsage(data []byte, stream bool) usageDetail {
	if stream {
		if detail, ok := parseOpenAIStreamUsage(data); ok {
			return detail
		}
		return usageDetail{}
	}
	return parseOpenAIUsage(data)
}

// createIFlowSignature generates HMAC-SHA256 signature for iFlow API requests.
// The signature payload format is: userAgent:sessionId:timestamp
func createIFlowSignature(userAgent, sessionID string, timestamp int64, apiKey string) string {
	if apiKey == "" {
		return ""
	}
	payload := fmt.Sprintf("%s:%s:%d", userAgent, sessionID, timestamp)
	h := hmac.New(sha256.New, []byte(apiKey))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

func ensureToolsArray(body []byte) []byte {
	placeholder := `[{"type":"function","function":{"name":"noop","description":"Placeholder tool to stabilise streaming","parameters":{"type":"object"}}}]`
	updated, err := sjson.SetRawBytes(body, "tools", []byte(placeholder))
	if err != nil {
		return body
	}
	return updated
}

// preserveReasoningContentInMessages checks if reasoning_content from assistant messages
// is preserved in conversation history for iFlow models that support thinking.
// This is helpful for multi-turn conversations where the model may benefit from seeing
// its previous reasoning to maintain coherent thought chains.
//
// For GLM-4.6/4.7 and MiniMax M2/M2.1, it is recommended to include the full assistant
// response (including reasoning_content) in message history for better context continuity.
func preserveReasoningContentInMessages(body []byte) []byte {
	model := strings.ToLower(gjson.GetBytes(body, "model").String())

	// Only apply to models that support thinking with history preservation
	needsPreservation := strings.HasPrefix(model, "glm-4") || strings.HasPrefix(model, "minimax-m2")

	if !needsPreservation {
		return body
	}

	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}

	// Check if any assistant message already has reasoning_content preserved
	hasReasoningContent := false
	messages.ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()
		if role == "assistant" {
			rc := msg.Get("reasoning_content")
			if rc.Exists() && rc.String() != "" {
				hasReasoningContent = true
				return false // stop iteration
			}
		}
		return true
	})

	// If reasoning content is already present, the messages are properly formatted
	// No need to modify - the client has correctly preserved reasoning in history
	if hasReasoningContent {
		log.Debugf("iflow executor: reasoning_content found in message history for %s", model)
	}

	return body
}
