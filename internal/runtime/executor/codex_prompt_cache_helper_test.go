package executor

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func resetCodexCacheForTest() {
	codexCacheMu.Lock()
	codexCacheMap = make(map[string]codexCache)
	codexCacheMu.Unlock()
}

func readRequestBodyForTest(t *testing.T, reqBody io.Reader) []byte {
	t.Helper()
	data, err := io.ReadAll(reqBody)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	return data
}

func TestApplyCodexPromptCache_ClaudeCachesByModelAndUserID(t *testing.T) {
	resetCodexCacheForTest()

	reqPayload := []byte(`{"metadata":{"user_id":"user-123"}}`)
	rawJSON := []byte(`{"input":[]}`)

	out, cacheID, headers := applyCodexPromptCache(nil, sdktranslator.FromString("claude"), reqPayload, "gpt-5", rawJSON, false)
	if cacheID == "" {
		t.Fatal("expected non-empty cache ID")
	}
	if got := gjson.GetBytes(out, "prompt_cache_key").String(); got != cacheID {
		t.Fatalf("prompt_cache_key = %q, want %q", got, cacheID)
	}
	if got := headers.Get("Conversation_id"); got != cacheID {
		t.Fatalf("Conversation_id = %q, want %q", got, cacheID)
	}
	if got := headers.Get("Session_id"); got != cacheID {
		t.Fatalf("Session_id = %q, want %q", got, cacheID)
	}

	_, cacheID2, _ := applyCodexPromptCache(nil, sdktranslator.FromString("claude"), reqPayload, "gpt-5", []byte(`{"input":[1]}`), false)
	if cacheID2 != cacheID {
		t.Fatalf("expected stable cache ID for same model+user: first=%q second=%q", cacheID, cacheID2)
	}

	_, cacheIDOtherModel, _ := applyCodexPromptCache(nil, sdktranslator.FromString("claude"), reqPayload, "gpt-4.1", []byte(`{"input":[]}`), false)
	if cacheIDOtherModel == cacheID {
		t.Fatalf("expected different cache ID for different model, got %q", cacheIDOtherModel)
	}
}

func TestApplyCodexPromptCache_OpenAIResponseUsesPromptCacheKey(t *testing.T) {
	resetCodexCacheForTest()

	reqPayload := []byte(`{"prompt_cache_key":"pck-123"}`)
	rawJSON := []byte(`{"input":[]}`)

	out, cacheID, headers := applyCodexPromptCache(nil, sdktranslator.FromString("openai-response"), reqPayload, "gpt-5", rawJSON, false)
	if cacheID != "pck-123" {
		t.Fatalf("cacheID = %q, want %q", cacheID, "pck-123")
	}
	if got := gjson.GetBytes(out, "prompt_cache_key").String(); got != "pck-123" {
		t.Fatalf("prompt_cache_key = %q, want %q", got, "pck-123")
	}
	if got := headers.Get("Conversation_id"); got != "pck-123" {
		t.Fatalf("Conversation_id = %q, want %q", got, "pck-123")
	}
	if got := headers.Get("Session_id"); got != "pck-123" {
		t.Fatalf("Session_id = %q, want %q", got, "pck-123")
	}
}

func TestApplyCodexPromptCache_OpenAIResponseRetentionDoesNotDisableExplicitKey(t *testing.T) {
	resetCodexCacheForTest()

	reqPayload := []byte(`{"prompt_cache_key":"pck-retained","prompt_cache_retention":"off"}`)
	rawJSON := []byte(`{"input":[]}`)

	out, cacheID, headers := applyCodexPromptCache(nil, sdktranslator.FromString("openai-response"), reqPayload, "gpt-5", rawJSON, false)
	if cacheID != "pck-retained" {
		t.Fatalf("cacheID = %q, want %q", cacheID, "pck-retained")
	}
	if got := gjson.GetBytes(out, "prompt_cache_key").String(); got != "pck-retained" {
		t.Fatalf("prompt_cache_key = %q, want %q", got, "pck-retained")
	}
	if got := headers.Get("Conversation_id"); got != "pck-retained" {
		t.Fatalf("Conversation_id = %q, want %q", got, "pck-retained")
	}
	if got := headers.Get("Session_id"); got != "pck-retained" {
		t.Fatalf("Session_id = %q, want %q", got, "pck-retained")
	}
}

func TestApplyCodexPromptCache_NoCacheInputLeavesPayloadUnchanged(t *testing.T) {
	resetCodexCacheForTest()

	rawJSON := []byte(`{"input":[]}`)
	out, cacheID, headers := applyCodexPromptCache(nil, sdktranslator.FromString("codex"), []byte(`{"input":[]}`), "gpt-5", rawJSON, false)
	if cacheID != "" {
		t.Fatalf("cacheID = %q, want empty", cacheID)
	}
	if len(headers) != 0 {
		t.Fatalf("headers = %#v, want empty", headers)
	}
	if !bytes.Equal(out, rawJSON) {
		t.Fatalf("payload changed unexpectedly: got=%s want=%s", string(out), string(rawJSON))
	}
	if gjson.GetBytes(out, "prompt_cache_key").Exists() {
		t.Fatalf("unexpected prompt_cache_key in payload: %s", string(out))
	}
}

func TestApplyCodexPromptCacheHeaders_SkipsWhenBodyEmpty(t *testing.T) {
	resetCodexCacheForTest()

	out, headers := applyCodexPromptCacheHeaders(
		nil,
		sdktranslator.FromString("claude"),
		[]byte(`{"metadata":{"user_id":"user-123"}}`),
		"gpt-5",
		[]byte{},
	)
	if len(out) != 0 {
		t.Fatalf("expected empty output body, got: %s", string(out))
	}
	if len(headers) != 0 {
		t.Fatalf("expected empty headers, got: %#v", headers)
	}
}

func TestApplyCodexPromptCache_CodexRetentionAndUserAutoCache(t *testing.T) {
	resetCodexCacheForTest()

	reqPayload := []byte(`{"user":"user-xyz","prompt_cache_retention":"24h"}`)
	rawJSON := []byte(`{"input":[]}`)

	out, cacheID, headers := applyCodexPromptCache(nil, sdktranslator.FromString("codex"), reqPayload, "gpt-5", rawJSON, false)
	if cacheID == "" {
		t.Fatal("expected non-empty cache ID")
	}
	if got := gjson.GetBytes(out, "prompt_cache_key").String(); got != cacheID {
		t.Fatalf("prompt_cache_key = %q, want %q", got, cacheID)
	}
	if got := headers.Get("Conversation_id"); got != cacheID {
		t.Fatalf("Conversation_id = %q, want %q", got, cacheID)
	}
	if got := headers.Get("Session_id"); got != cacheID {
		t.Fatalf("Session_id = %q, want %q", got, cacheID)
	}
}

func TestApplyCodexPromptCache_RetentionDisabledSkipsAutoCache(t *testing.T) {
	resetCodexCacheForTest()

	reqPayload := []byte(`{"user":"user-xyz","prompt_cache_retention":"off"}`)
	rawJSON := []byte(`{"input":[]}`)

	out, cacheID, headers := applyCodexPromptCache(nil, sdktranslator.FromString("codex"), reqPayload, "gpt-5", rawJSON, false)
	if cacheID != "" {
		t.Fatalf("cacheID = %q, want empty", cacheID)
	}
	if len(headers) != 0 {
		t.Fatalf("headers = %#v, want empty", headers)
	}
	if gjson.GetBytes(out, "prompt_cache_key").Exists() {
		t.Fatalf("prompt_cache_key should not be set when retention disables cache")
	}
}

func TestCodexPromptCacheEffectiveTTL_PreservesSubMinuteConfiguredTTL(t *testing.T) {
	ttl := codexPromptCacheEffectiveTTL(&config.Config{CodexPromptCache: config.CodexPromptCacheConfig{TTLSeconds: 7}}, nil)
	if ttl != 7*time.Second {
		t.Fatalf("ttl = %v, want %v", ttl, 7*time.Second)
	}
}

func TestCodexPromptCacheEffectiveTTL_PreservesSubMinuteRetentionTTL(t *testing.T) {
	reqPayload := []byte(`{"prompt_cache_retention":5}`)
	ttl := codexPromptCacheEffectiveTTL(nil, reqPayload)
	if ttl != 5*time.Second {
		t.Fatalf("ttl = %v, want %v", ttl, 5*time.Second)
	}
}

func TestCodexExecutorCacheHelper_AppliesUnifiedCacheLogic(t *testing.T) {
	resetCodexCacheForTest()

	exec := &CodexExecutorRefactored{}
	httpReq, err := exec.cacheHelper(
		context.Background(),
		sdktranslator.FromString("openai-response"),
		"https://example.com/responses",
		cliproxyexecutor.Request{
			Model:   "gpt-5",
			Payload: []byte(`{"prompt_cache_key":"pck-456"}`),
		},
		[]byte(`{"input":[]}`),
	)
	if err != nil {
		t.Fatalf("cacheHelper error: %v", err)
	}

	if got := httpReq.Header.Get("Conversation_id"); got != "pck-456" {
		t.Fatalf("Conversation_id = %q, want %q", got, "pck-456")
	}
	if got := httpReq.Header.Get("Session_id"); got != "pck-456" {
		t.Fatalf("Session_id = %q, want %q", got, "pck-456")
	}

	body := readRequestBodyForTest(t, httpReq.Body)
	if got := gjson.GetBytes(body, "prompt_cache_key").String(); got != "pck-456" {
		t.Fatalf("prompt_cache_key = %q, want %q", got, "pck-456")
	}
}
