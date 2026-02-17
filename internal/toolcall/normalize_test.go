package toolcall

import "testing"

func TestNormalizeArgsJSON(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "{}"},
		{"{\"a\":1}", "{\"a\":1}"},
		{"[1,2]", "{\"value\":[1,2]}"},
		{"\"x\"", "{\"value\":\"x\"}"},
	}
	for _, c := range cases {
		if got := NormalizeArgsJSON(c.in); got != c.want {
			t.Fatalf("NormalizeArgsJSON(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestEnsureCallID(t *testing.T) {
	if got := EnsureCallID("gemini", 2); got != "tc_gemini_2" {
		t.Fatalf("EnsureCallID()=%q", got)
	}
}

func TestTrackerResolveByNameAndRepair(t *testing.T) {
	tr := NewTracker("gemini")
	id1 := tr.RegisterCall("lookup", "")
	if id1 != "tc_gemini_1" {
		t.Fatalf("fallback id = %q", id1)
	}
	_, _, _, ok := tr.ResolveResultID("lookup", "")
	if !ok {
		t.Fatal("expected resolve by single pending call")
	}
}

func TestMapChoiceFromGeminiMode(t *testing.T) {
	if got, ok := MapChoiceFromGeminiMode("ANY"); !ok || got != "required" {
		t.Fatalf("MapChoiceFromGeminiMode(ANY)=(%q,%v)", got, ok)
	}
}

func TestValidateInvariants(t *testing.T) {
	valid := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_1"}]},{"role":"tool","tool_call_id":"call_1"}]}`)
	if err := ValidateToolResultReferences(valid); err != nil {
		t.Fatalf("ValidateToolResultReferences() err=%v", err)
	}
	if err := ValidateOpenAIAdjacency(valid); err != nil {
		t.Fatalf("ValidateOpenAIAdjacency() err=%v", err)
	}

	invalid := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_1"}]},{"role":"user","content":"oops"}]}`)
	if err := ValidateOpenAIAdjacency(invalid); err == nil {
		t.Fatal("expected adjacency error")
	}
}
