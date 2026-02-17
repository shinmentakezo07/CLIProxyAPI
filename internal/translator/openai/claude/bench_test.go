package claude

import "testing"

func BenchmarkTranslateToolCalls_OpenAIToClaude(b *testing.B) {
	in := []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"lookup","input":{}},{"type":"tool_use","id":"call_2","name":"read","input":{"path":"README.md"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"[]"},{"type":"tool_result","tool_use_id":"call_2","content":"text"}]}]}`)
	for i := 0; i < b.N; i++ {
		_ = ConvertClaudeRequestToOpenAI("gpt-5", in, false)
	}
}
