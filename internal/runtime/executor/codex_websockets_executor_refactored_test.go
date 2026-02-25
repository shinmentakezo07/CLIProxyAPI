package executor

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestBuildCodexWebsocketRequestBody_UsesCreateWithoutPreviousResponseID(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":[{"type":"message","id":"m1"}]}`)

	got := buildCodexWebsocketRequestBody(body, true)

	if gotType := gjson.GetBytes(got, "type").String(); gotType != "response.create" {
		t.Fatalf("type = %q, want response.create", gotType)
	}
	if gotModel := gjson.GetBytes(got, "model").String(); gotModel != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", gotModel)
	}
	if gotID := gjson.GetBytes(got, "input.0.id").String(); gotID != "m1" {
		t.Fatalf("input[0].id = %q, want m1", gotID)
	}
}

func TestBuildCodexWebsocketRequestBody_UsesAppendForIncrementalTurn(t *testing.T) {
	body := []byte(`{"previous_response_id":"resp-1","model":"gpt-5","input":[{"type":"function_call_output","call_id":"call-1","id":"tool-1"}]}`)

	got := buildCodexWebsocketRequestBody(body, true)

	if gotType := gjson.GetBytes(got, "type").String(); gotType != "response.append" {
		t.Fatalf("type = %q, want response.append", gotType)
	}
	if gjson.GetBytes(got, "previous_response_id").Exists() {
		t.Fatalf("previous_response_id should not be present in append request body")
	}
	if gjson.GetBytes(got, "model").Exists() {
		t.Fatalf("model should not be present in append request body")
	}
	if gotID := gjson.GetBytes(got, "input.0.id").String(); gotID != "tool-1" {
		t.Fatalf("input[0].id = %q, want tool-1", gotID)
	}
}

func TestBuildCodexWebsocketRequestBody_UsesAppendWithEmptyInputWhenMissingInput(t *testing.T) {
	body := []byte(`{"previous_response_id":"resp-1"}`)

	got := buildCodexWebsocketRequestBody(body, true)

	if gotType := gjson.GetBytes(got, "type").String(); gotType != "response.append" {
		t.Fatalf("type = %q, want response.append", gotType)
	}
	if count := len(gjson.GetBytes(got, "input").Array()); count != 0 {
		t.Fatalf("input len = %d, want 0", count)
	}
}

func TestBuildCodexWebsocketRequestBody_UsesCreateWhenAppendDisallowed(t *testing.T) {
	body := []byte(`{"previous_response_id":"resp-1","input":[{"type":"message","id":"m2"}]}`)

	got := buildCodexWebsocketRequestBody(body, false)

	if gotType := gjson.GetBytes(got, "type").String(); gotType != "response.create" {
		t.Fatalf("type = %q, want response.create", gotType)
	}
	if gotPrev := gjson.GetBytes(got, "previous_response_id").String(); gotPrev != "resp-1" {
		t.Fatalf("previous_response_id = %q, want resp-1", gotPrev)
	}
}
