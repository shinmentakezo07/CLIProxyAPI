package gemini

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkTranslateToolCalls_GeminiToOpenAI(b *testing.B) {
	in, err := os.ReadFile(filepath.Join("..", "..", "testdata", "toolcalls", "missing_tool_call_id.json"))
	if err != nil {
		b.Fatalf("read fixture: %v", err)
	}
	for i := 0; i < b.N; i++ {
		_ = ConvertGeminiRequestToOpenAI("gpt-5", in, false)
	}
}
