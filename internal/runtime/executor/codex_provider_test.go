package executor

import (
	"testing"

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
