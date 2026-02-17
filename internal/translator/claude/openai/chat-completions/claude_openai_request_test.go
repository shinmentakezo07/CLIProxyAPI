package chat_completions

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestToClaude_ToolCallShape(t *testing.T) {
	in := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	out := ConvertOpenAIRequestToClaude("claude-3-7-sonnet", in, false)
	if got := gjson.GetBytes(out, "messages.0.content.0.type").String(); got != "tool_use" {
		t.Fatalf("messages.0.content.0.type=%q", got)
	}
	if got := gjson.GetBytes(out, "messages.1.content.0.type").String(); got != "tool_result" {
		t.Fatalf("messages.1.content.0.type=%q", got)
	}
}
