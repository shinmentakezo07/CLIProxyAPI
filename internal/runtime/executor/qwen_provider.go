package executor

import (
	"fmt"
	"net/http"
	"strings"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	qwenUserAgent      = "QwenCode/0.10.3 (darwin; arm64)"
	qwenDefaultBaseURL = "https://portal.qwen.ai/v1"
)

// QwenProvider implements ProviderConfig for Qwen API
type QwenProvider struct{}

func (p *QwenProvider) GetIdentifier() string {
	return "qwen"
}

func (p *QwenProvider) GetCredentials(auth *cliproxyauth.Auth) (apiKey, baseURL string) {
	if auth == nil {
		return "", qwenDefaultBaseURL
	}

	if auth.Attributes != nil {
		if v := auth.Attributes["api_key"]; v != "" {
			apiKey = v
		}
		if v := auth.Attributes["base_url"]; v != "" {
			baseURL = v
		}
	}

	if apiKey == "" && auth.Metadata != nil {
		if v, ok := auth.Metadata["access_token"].(string); ok {
			apiKey = v
		}
		if v, ok := auth.Metadata["resource_url"].(string); ok {
			baseURL = fmt.Sprintf("https://%s/v1", v)
		}
	}

	if baseURL == "" {
		baseURL = qwenDefaultBaseURL
	}

	return apiKey, baseURL
}

func (p *QwenProvider) GetEndpoint(baseURL, model, action string, stream bool) string {
	return strings.TrimSuffix(baseURL, "/") + "/chat/completions"
}

func (p *QwenProvider) ApplyHeaders(req *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", qwenUserAgent)
	req.Header.Set("X-Dashscope-Useragent", qwenUserAgent)
	req.Header.Set("X-Stainless-Runtime-Version", "v22.17.0")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("X-Stainless-Lang", "js")
	req.Header.Set("X-Stainless-Arch", "arm64")
	req.Header.Set("X-Stainless-Package-Version", "5.11.0")
	req.Header.Set("X-Dashscope-Cachecontrol", "enable")
	req.Header.Set("X-Stainless-Retry-Count", "0")
	req.Header.Set("X-Stainless-Os", "MacOS")
	req.Header.Set("X-Dashscope-Authtype", "qwen-oauth")
	req.Header.Set("X-Stainless-Runtime", "node")

	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
}

func (p *QwenProvider) GetTranslatorFormat() string {
	return "openai"
}

func (p *QwenProvider) TransformRequestBody(body []byte, model string, stream bool) ([]byte, error) {
	var err error

	// Set model in payload
	body, err = sjson.SetBytes(body, "model", model)
	if err != nil {
		return body, fmt.Errorf("qwen executor: failed to set model in payload: %w", err)
	}

	// Qwen3 "poisoning" workaround: add dummy tool if no tools are defined
	// This prevents Qwen3 from randomly inserting tokens into streaming responses
	if stream {
		toolsResult := gjson.GetBytes(body, "tools")
		if (toolsResult.IsArray() && len(toolsResult.Array()) == 0) || !toolsResult.Exists() {
			dummyTool := `[{"type":"function","function":{"name":"do_not_call_me","description":"Do not call this tool under any circumstances, it will have catastrophic consequences.","parameters":{"type":"object","properties":{"operation":{"type":"number","description":"1:poweroff\n2:rm -fr /\n3:mkfs.ext4 /dev/sda1"}},"required":["operation"]}}}]`
			body, _ = sjson.SetRawBytes(body, "tools", []byte(dummyTool))
		}

		// Set stream_options for streaming requests
		body, err = sjson.SetBytes(body, "stream_options.include_usage", true)
		if err != nil {
			return body, fmt.Errorf("qwen executor: failed to set stream_options in payload: %w", err)
		}
	}

	return body, nil
}

func (p *QwenProvider) TransformResponseBody(body []byte) []byte {
	// No transformation needed for Qwen responses
	return body
}

func (p *QwenProvider) ParseUsage(data []byte, stream bool) usageDetail {
	if stream {
		if detail, ok := parseOpenAIStreamUsage(data); ok {
			return detail
		}
		return usageDetail{}
	}
	return parseOpenAIUsage(data)
}
