package toolcall

import "testing"

func TestNormalizeArgumentsObjectString(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "{}"},
		{"{\"a\":1}", "{\"a\":1}"},
		{"[1,2]", "{\"value\":[1,2]}"},
		{"\"x\"", "{\"value\":\"x\"}"},
	}
	for _, c := range cases {
		if got := NormalizeArgumentsObjectString(c.in); got != c.want {
			t.Fatalf("NormalizeArgumentsObjectString(%q)=%q want %q", c.in, got, c.want)
		}
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
