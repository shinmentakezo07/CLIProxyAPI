package chat_completions

import (
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertCodexResponseToOpenAI_ToolCallStreamingSequence(t *testing.T) {
	originalToolName := "mcp__server__" + strings.Repeat("very_long_segment_", 6)
	shortName := buildShortNameMap([]string{originalToolName})[originalToolName]
	if shortName == "" {
		t.Fatal("expected shortened tool name")
	}

	originalRequest := []byte(`{"tools":[{"type":"function","function":{"name":"` + originalToolName + `"}}]}`)
	var param any

	created := ConvertCodexResponseToOpenAI(
		context.Background(),
		"gpt-5",
		originalRequest,
		nil,
		[]byte(`data: {"type":"response.created","response":{"id":"resp-1","created_at":123,"model":"gpt-5"}}`),
		&param,
	)
	if len(created) != 0 {
		t.Fatalf("response.created should not emit chunk, got %d", len(created))
	}

	added := ConvertCodexResponseToOpenAI(
		context.Background(),
		"gpt-5",
		originalRequest,
		nil,
		[]byte(`data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call-1","name":"`+shortName+`"}}`),
		&param,
	)
	if len(added) != 1 {
		t.Fatalf("response.output_item.added emitted %d chunks, want 1", len(added))
	}
	if got := gjson.Get(added[0], "choices.0.delta.tool_calls.0.id").String(); got != "call-1" {
		t.Fatalf("tool call id = %q, want call-1", got)
	}
	if got := gjson.Get(added[0], "choices.0.delta.tool_calls.0.function.name").String(); got != originalToolName {
		t.Fatalf("restored tool name = %q, want %q", got, originalToolName)
	}
	if got := gjson.Get(added[0], "choices.0.delta.role").String(); got != "assistant" {
		t.Fatalf("delta role = %q, want assistant", got)
	}

	argsDelta := ConvertCodexResponseToOpenAI(
		context.Background(),
		"gpt-5",
		originalRequest,
		nil,
		[]byte(`data: {"type":"response.function_call_arguments.delta","delta":"{\"q\":\"hel"}`),
		&param,
	)
	if len(argsDelta) != 1 {
		t.Fatalf("args delta emitted %d chunks, want 1", len(argsDelta))
	}
	if got := gjson.Get(argsDelta[0], "choices.0.delta.tool_calls.0.function.arguments").String(); got != `{"q":"hel` {
		t.Fatalf("args delta = %q", got)
	}

	argsDone := ConvertCodexResponseToOpenAI(
		context.Background(),
		"gpt-5",
		originalRequest,
		nil,
		[]byte(`data: {"type":"response.function_call_arguments.done","arguments":"{\"q\":\"hello\"}"}`),
		&param,
	)
	if len(argsDone) != 0 {
		t.Fatalf("args done should be suppressed after delta, got %d chunks", len(argsDone))
	}

	completed := ConvertCodexResponseToOpenAI(
		context.Background(),
		"gpt-5",
		originalRequest,
		nil,
		[]byte(`data: {"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`),
		&param,
	)
	if len(completed) != 1 {
		t.Fatalf("response.completed emitted %d chunks, want 1", len(completed))
	}
	if got := gjson.Get(completed[0], "choices.0.finish_reason").String(); got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
}

func TestConvertCodexResponseToOpenAI_OutputItemDoneFallback(t *testing.T) {
	originalRequest := []byte(`{"tools":[{"type":"function","function":{"name":"search_docs"}}]}`)
	var param any

	_ = ConvertCodexResponseToOpenAI(
		context.Background(),
		"gpt-5",
		originalRequest,
		nil,
		[]byte(`data: {"type":"response.created","response":{"id":"resp-2","created_at":456,"model":"gpt-5"}}`),
		&param,
	)

	fallback := ConvertCodexResponseToOpenAI(
		context.Background(),
		"gpt-5",
		originalRequest,
		nil,
		[]byte(`data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"call-9","name":"search_docs","arguments":"{\"q\":\"docs\"}"}}`),
		&param,
	)
	if len(fallback) != 1 {
		t.Fatalf("fallback emitted %d chunks, want 1", len(fallback))
	}
	if got := gjson.Get(fallback[0], "choices.0.delta.tool_calls.0.id").String(); got != "call-9" {
		t.Fatalf("tool call id = %q, want call-9", got)
	}
	if got := gjson.Get(fallback[0], "choices.0.delta.tool_calls.0.function.name").String(); got != "search_docs" {
		t.Fatalf("tool name = %q, want search_docs", got)
	}
	if got := gjson.Get(fallback[0], "choices.0.delta.tool_calls.0.function.arguments").String(); got != `{"q":"docs"}` {
		t.Fatalf("tool args = %q, want %q", got, `{"q":"docs"}`)
	}
}
