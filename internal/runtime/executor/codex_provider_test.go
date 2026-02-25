package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func TestNormalizeCodexRequestBody(t *testing.T) {
	input := []byte(`{"previous_response_id":"a","prompt_cache_retention":"24h","safety_identifier":"id"}`)

	got, err := normalizeCodexRequestBody(input, "gpt-5", true)
	if err != nil {
		t.Fatalf("normalizeCodexRequestBody returned error: %v", err)
	}

	if model := gjson.GetBytes(got, "model").String(); model != "gpt-5" {
		t.Fatalf("expected model gpt-5, got %q", model)
	}
	if stream := gjson.GetBytes(got, "stream"); !stream.Exists() || !stream.Bool() {
		t.Fatalf("expected stream=true, got %s", stream.Raw)
	}
	if gjson.GetBytes(got, "instructions").String() != "" {
		t.Fatalf("expected instructions to default to empty string")
	}
	if gjson.GetBytes(got, "previous_response_id").Exists() {
		t.Fatalf("expected previous_response_id to be removed")
	}
	if gjson.GetBytes(got, "prompt_cache_retention").Exists() {
		t.Fatalf("expected prompt_cache_retention to be removed")
	}
	if gjson.GetBytes(got, "safety_identifier").Exists() {
		t.Fatalf("expected safety_identifier to be removed")
	}
}

func TestNormalizeCodexRequestBody_MapsReasoningEffortAlias(t *testing.T) {
	input := []byte(`{"reasoning_effort":"high"}`)

	got, err := normalizeCodexRequestBody(input, "gpt-5", true)
	if err != nil {
		t.Fatalf("normalizeCodexRequestBody returned error: %v", err)
	}

	if gotEffort := gjson.GetBytes(got, "reasoning.effort").String(); gotEffort != "high" {
		t.Fatalf("reasoning.effort = %q, want %q", gotEffort, "high")
	}
	if gjson.GetBytes(got, "reasoning_effort").Exists() {
		t.Fatalf("expected reasoning_effort alias to be removed")
	}
}

func TestCodexProviderApplyHeaders_ForwardsCodexHintHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Request.Header.Set("x-codex-turn-state", "turn-state")
	c.Request.Header.Set("x-codex-turn-metadata", "turn-meta")
	c.Request.Header.Set("x-responsesapi-include-timing-metrics", "1")

	req, err := http.NewRequestWithContext(contextWithGinForTest(c), http.MethodPost, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	p := NewCodexProvider(nil)
	p.ApplyHeaders(req, nil, "token", true)

	if got := req.Header.Get("x-codex-turn-state"); got != "turn-state" {
		t.Fatalf("x-codex-turn-state = %q, want %q", got, "turn-state")
	}
	if got := req.Header.Get("x-codex-turn-metadata"); got != "turn-meta" {
		t.Fatalf("x-codex-turn-metadata = %q, want %q", got, "turn-meta")
	}
	if got := req.Header.Get("x-responsesapi-include-timing-metrics"); got != "1" {
		t.Fatalf("x-responsesapi-include-timing-metrics = %q, want %q", got, "1")
	}
	if got := req.Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept = %q, want %q", got, "text/event-stream")
	}
}

func TestCodexCompletedEventPayload(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "valid completed event",
			data: []byte("data: {\"type\":\"response.completed\",\"response\":{}}"),
			want: true,
		},
		{
			name: "non-completed event",
			data: []byte("data: {\"type\":\"response.in_progress\"}"),
			want: false,
		},
		{
			name: "non-sse payload",
			data: []byte(`{"type":"response.completed"}`),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, ok := codexCompletedEventPayload(tt.data)
			if ok != tt.want {
				t.Fatalf("expected ok=%v, got %v", tt.want, ok)
			}
			if ok && gjson.GetBytes(payload, "type").String() != "response.completed" {
				t.Fatalf("expected response.completed payload, got %s", string(payload))
			}
		})
	}
}

func contextWithGinForTest(c *gin.Context) context.Context {
	if c == nil {
		return context.Background()
	}
	return context.WithValue(context.Background(), "gin", c)
}
