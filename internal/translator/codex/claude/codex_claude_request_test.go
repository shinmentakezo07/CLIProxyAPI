package claude

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertClaudeRequestToCodex_ToolUseConversion(t *testing.T) {
	in := []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"lookup","input":{"path":"/tmp"}}]}]}`)
	out := ConvertClaudeRequestToCodex("gpt-5", in, false)
	if got := gjson.GetBytes(out, "input.0.type").String(); got != "function_call" {
		t.Fatalf("input.0.type=%q", got)
	}
	if got := gjson.GetBytes(out, "input.0.call_id").String(); got != "call_1" {
		t.Fatalf("call_id=%q", got)
	}
}
