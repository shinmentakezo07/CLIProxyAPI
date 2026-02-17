package chat_completions

import (
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertGeminiResponseToOpenAI_StreamTextAndReasoning(t *testing.T) {
	var param any
	raw := []byte(`{
		"responseId":"resp_1",
		"modelVersion":"gemini-3",
		"createTime":"2026-01-01T00:00:00Z",
		"candidates":[{
			"index":0,
			"content":{"parts":[
				{"text":"answer"},
				{"text":"deliberation","thought":true}
			]}
		}]
	}`)

	chunks := ConvertGeminiResponseToOpenAI(context.Background(), "", nil, nil, raw, &param)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	chunk := chunks[0]
	if got := gjson.Get(chunk, "choices.0.delta.content").String(); got != "answer" {
		t.Fatalf("content mismatch: got %q", got)
	}
	if got := gjson.Get(chunk, "choices.0.delta.reasoning_content").String(); got != "deliberation" {
		t.Fatalf("reasoning mismatch: got %q", got)
	}
	if got := gjson.Get(chunk, "choices.0.delta.role").String(); got != "assistant" {
		t.Fatalf("role mismatch: got %q", got)
	}
}

func TestConvertGeminiResponseToOpenAI_StreamFunctionCallAndInlineData(t *testing.T) {
	var param any
	raw := []byte(`{
		"responseId":"resp_2",
		"candidates":[{
			"index":1,
			"content":{"parts":[
				{"functionCall":{"name":"lookup","args":{"q":"x"}}},
				{"inlineData":{"data":"AAAA","mimeType":"image/png"}}
			]}
		}]
	}`)

	chunks := ConvertGeminiResponseToOpenAI(context.Background(), "", nil, nil, raw, &param)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	chunk := chunks[0]
	if got := gjson.Get(chunk, "choices.0.index").Int(); got != 1 {
		t.Fatalf("index mismatch: got %d", got)
	}
	if got := gjson.Get(chunk, "choices.0.finish_reason").String(); got != "tool_calls" {
		t.Fatalf("finish_reason mismatch: got %q", got)
	}
	if got := gjson.Get(chunk, "choices.0.delta.tool_calls.0.function.name").String(); got != "lookup" {
		t.Fatalf("function name mismatch: got %q", got)
	}
	if got := gjson.Get(chunk, "choices.0.delta.images.0.image_url.url").String(); got != "data:image/png;base64,AAAA" {
		t.Fatalf("image url mismatch: got %q", got)
	}
}
