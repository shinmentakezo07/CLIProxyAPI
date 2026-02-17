package toolcall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenHelpers(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return strings.TrimSpace(string(b))
	}
	if got, want := NormalizeArgsJSON("{not-json"), read("normalize_args_malformed.golden"); got != want {
		t.Fatalf("NormalizeArgsJSON malformed mismatch: got=%s want=%s", got, want)
	}
	if got, want := EnsureCallID("gemini", 1), read("fallback_id_gemini.golden"); got != want {
		t.Fatalf("EnsureCallID mismatch: got=%s want=%s", got, want)
	}
	valid := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_1"}]},{"role":"tool","tool_call_id":"call_1"}]}`)
	if err := ValidateOpenAIAdjacency(valid); err != nil {
		t.Fatalf("expected %s adjacency, got %v", read("adjacency_valid.golden"), err)
	}
}
