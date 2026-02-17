package gemini

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/toolcall"
	"github.com/tidwall/gjson"
)

func TestConvertGeminiRequestToOpenAI_ToolCallNormalization(t *testing.T) {
	input := []byte(`{
		"contents":[
			{"role":"model","parts":[{"functionCall":{"name":"toolA","args":"abc"}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"toolA","response":{"content":{"ok":true}}}}]}
		]
	}`)
	out := ConvertGeminiRequestToOpenAI("gpt-5", input, false)
	if got := gjson.GetBytes(out, "messages.0.tool_calls.0.id").String(); got != "tc_gemini_1" {
		t.Fatalf("tool id = %q", got)
	}
	if got := gjson.GetBytes(out, "messages.0.tool_calls.0.function.arguments").String(); got != "{\"value\":\"abc\"}" {
		t.Fatalf("arguments = %q", got)
	}
	if got := gjson.GetBytes(out, "messages.1.tool_call_id").String(); got != "tc_gemini_1" {
		t.Fatalf("tool response id = %q", got)
	}
}

func TestConvertGeminiRequestToOpenAI_ToolChoiceAndSchema(t *testing.T) {
	input := []byte(`{
		"toolConfig":{"functionCallingConfig":{"mode":"ANY"}},
		"tools":[{"functionDeclarations":[{"name":"x","parametersJsonSchema":{"type":"object"}}]}]
	}`)
	out := ConvertGeminiRequestToOpenAI("gpt-5", input, false)
	if got := gjson.GetBytes(out, "tool_choice").String(); got != "required" {
		t.Fatalf("tool_choice=%q", got)
	}
	if got := gjson.GetBytes(out, "tools.0.function.parameters.type").String(); got != "object" {
		t.Fatalf("schema type=%q", got)
	}
}

func TestConvertGeminiRequestToOpenAI_GoldenFixtures(t *testing.T) {
	t.Parallel()
	cases := []string{"missing_tool_call_id", "ambiguous_pending_calls", "malformed_arguments"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			inputPath := filepath.Join("..", "..", "testdata", "toolcalls", name+".json")
			goldenPath := filepath.Join("..", "..", "testdata", "toolcalls", name+".golden")
			in, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("read input: %v", err)
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			got := ConvertGeminiRequestToOpenAI("gpt-5", in, false)
			if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
				t.Fatalf("golden mismatch\nwant=%s\n got=%s", string(want), string(got))
			}
			if err := toolcall.ValidateToolResultReferences(got); name == "missing_tool_call_id" && err != nil {
				t.Fatalf("ValidateToolResultReferences: %v", err)
			}
		})
	}
}
