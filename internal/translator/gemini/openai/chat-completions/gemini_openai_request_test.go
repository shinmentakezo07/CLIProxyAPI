package chat_completions

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestToGemini_PreservesToolOrder(t *testing.T) {
	in := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"f1","arguments":"{}"}},{"id":"call_2","type":"function","function":{"name":"f2","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"r1"},{"role":"tool","tool_call_id":"call_2","content":"r2"}]}`)
	out := ConvertOpenAIRequestToGemini("gemini-2.5-pro", in, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.functionCall.name").String(); got != "f1" {
		t.Fatalf("first tool name=%q", got)
	}
	if got := gjson.GetBytes(out, "contents.1.parts.0.functionResponse.name").String(); got != "f1" {
		t.Fatalf("first response name=%q", got)
	}
}
