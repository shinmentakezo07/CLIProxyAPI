package executor

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestParseCodexAgentConfig(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want codexAgentMode
	}{
		{
			name: "cliproxy nested",
			body: []byte(`{"_cliproxy":{"agent_mode":"planner-reviewer"}}`),
			want: codexAgentModePlannerReviewer,
		},
		{
			name: "top-level alias underscore",
			body: []byte(`{"agent_mode":"planner_reviewer"}`),
			want: codexAgentModePlannerReviewer,
		},
		{
			name: "unknown mode",
			body: []byte(`{"_cliproxy":{"agent_mode":"foo"}}`),
			want: codexAgentModeNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCodexAgentConfig(tt.body)
			if got.Mode != tt.want {
				t.Fatalf("mode = %q, want %q", got.Mode, tt.want)
			}
		})
	}
}

func TestCodexAgentCompatibilityIssue(t *testing.T) {
	if issue := codexAgentCompatibilityIssue([]byte(`{"tools":[{"name":"x"}]}`)); !strings.Contains(issue, "tool") {
		t.Fatalf("expected tool compatibility issue, got %q", issue)
	}
	if issue := codexAgentCompatibilityIssue([]byte(`{"text":{"format":{"type":"json_schema"}}}`)); !strings.Contains(issue, "structured") {
		t.Fatalf("expected structured compatibility issue, got %q", issue)
	}
	if issue := codexAgentCompatibilityIssue([]byte(`{"input":[]}`)); issue != "" {
		t.Fatalf("unexpected issue for simple text request: %q", issue)
	}
}

func TestCodexBuildAgentPassBody(t *testing.T) {
	base := []byte(`{
		"model":"gpt-5",
		"stream":true,
		"reasoning":{"effort":"high"},
		"tools":[{"type":"function","name":"x"}],
		"_cliproxy":{"agent_mode":"planner-reviewer"},
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]
	}`)

	out, err := codexBuildAgentPassBody(base, "planner", "original task", "", "")
	if err != nil {
		t.Fatalf("codexBuildAgentPassBody error: %v", err)
	}

	if got := gjson.GetBytes(out, "tools"); got.Exists() {
		t.Fatalf("expected tools to be stripped, got %s", got.Raw)
	}
	if got := gjson.GetBytes(out, "_cliproxy"); got.Exists() {
		t.Fatalf("expected _cliproxy to be stripped, got %s", got.Raw)
	}
	if !gjson.GetBytes(out, "stream").Bool() {
		t.Fatalf("expected stream=true in internal pass body")
	}
	if got := gjson.GetBytes(out, "reasoning.effort").String(); got != "high" {
		t.Fatalf("reasoning.effort = %q, want %q", got, "high")
	}
	prompt := gjson.GetBytes(out, "input.0.content.0.text").String()
	if !strings.Contains(prompt, "original task") {
		t.Fatalf("expected planner prompt to include original task, got %q", prompt)
	}
	instructions := gjson.GetBytes(out, "instructions").String()
	if !strings.Contains(strings.ToLower(instructions), "planning agent") {
		t.Fatalf("expected planner instructions, got %q", instructions)
	}
}

func TestCodexExtractCompletedMessageAndReasoning(t *testing.T) {
	payload := []byte(`{
		"type":"response.completed",
		"response":{
			"output":[
				{"type":"reasoning","summary":[{"type":"summary_text","text":"check edge cases"}]},
				{"type":"message","content":[{"type":"output_text","text":"final answer"}]}
			]
		}
	}`)
	msg, reasoning := codexExtractCompletedMessageAndReasoning(payload)
	if msg != "final answer" {
		t.Fatalf("message = %q, want %q", msg, "final answer")
	}
	if reasoning != "check edge cases" {
		t.Fatalf("reasoning = %q, want %q", reasoning, "check edge cases")
	}
}

func TestCodexAgentPhaseInstructions_IncludeDeepEngineeringStandards(t *testing.T) {
	planner := codexAgentPhaseInstructions("planner")
	if !strings.Contains(planner, "internal planning agent") {
		t.Fatalf("planner instructions missing role text: %q", planner)
	}
	if !strings.Contains(planner, "Override Brevity") || !strings.Contains(planner, "Library & Framework Discipline") {
		t.Fatalf("planner instructions missing shared engineering standards: %q", planner)
	}
	if strings.Contains(planner, "Response format (normal mode)") {
		t.Fatalf("planner instructions should not include final response format block: %q", planner)
	}

	final := codexAgentPhaseInstructions("final")
	if !strings.Contains(final, "final responder") {
		t.Fatalf("final instructions missing role text: %q", final)
	}
	if !strings.Contains(final, "Response format (normal mode)") {
		t.Fatalf("final instructions missing response format block: %q", final)
	}
}
