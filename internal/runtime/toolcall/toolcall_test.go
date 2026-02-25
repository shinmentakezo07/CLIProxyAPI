package toolcall

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryRegisterAndDuplicate(t *testing.T) {
	r := NewRegistry()
	def := Definition{
		Name: "search_docs",
		Handler: func(ctx context.Context, input map[string]any) (any, error) {
			return "ok", nil
		},
	}
	if err := r.Register(def); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if err := r.Register(def); err == nil {
		t.Fatal("expected duplicate register error")
	}
	if names := r.Names(); len(names) != 1 || names[0] != "search_docs" {
		t.Fatalf("unexpected names: %#v", names)
	}
}

func TestObjectSchemaValidateRaw(t *testing.T) {
	schema := &ObjectSchema{
		Fields: map[string]FieldSchema{
			"query": {Type: FieldTypeString, Required: true},
			"limit": {Type: FieldTypeInteger, MinInt: IntPtr(1), MaxInt: IntPtr(10)},
			"mode":  {Type: FieldTypeString, Enum: []string{"fast", "deep"}},
		},
		AdditionalAllowed: false,
	}

	_, err := schema.ValidateRaw(`{"query":123,"limit":0,"extra":true,"mode":"x"}`)
	if err == nil {
		t.Fatal("expected validation error")
	}
	verr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if len(verr.Issues) < 4 {
		t.Fatalf("expected multiple issues, got %#v", verr.Issues)
	}

	parsed, err := schema.ValidateRaw(`{"query":"ws retry","limit":3,"mode":"fast"}`)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if got := parsed["query"].(string); got != "ws retry" {
		t.Fatalf("query = %q", got)
	}
}

func TestRuntimeExecuteSuccessAndBuildFunctionCallOutput(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(Definition{
		Name: "search_docs",
		InputSchema: &ObjectSchema{
			Fields: map[string]FieldSchema{
				"query": {Type: FieldTypeString, Required: true},
			},
		},
		Handler: func(ctx context.Context, input map[string]any) (any, error) {
			return map[string]any{"echo": input["query"]}, nil
		},
	})

	rt := &Runtime{Registry: r, Store: NewMemoryStore(), DefaultTimeout: 2 * time.Second}
	res := rt.Execute(context.Background(), CallRequest{CallID: "call-1", ToolName: "search_docs", RawArguments: `{"query":"hello"}`})
	if !res.Envelope.OK {
		t.Fatalf("expected success envelope, got error: %+v", res.Envelope.Error)
	}
	if res.Record.Status != StatusSucceeded {
		t.Fatalf("status = %s", res.Record.Status)
	}
	if got := res.Validated["query"].(string); got != "hello" {
		t.Fatalf("validated query = %q", got)
	}

	itemBytes, err := BuildFunctionCallOutputItem("call-1", res.Envelope)
	if err != nil {
		t.Fatalf("BuildFunctionCallOutputItem error: %v", err)
	}
	var item FunctionCallOutputItem
	if err := json.Unmarshal(itemBytes, &item); err != nil {
		t.Fatalf("unmarshal item: %v", err)
	}
	if item.Type != "function_call_output" || item.CallID != "call-1" {
		t.Fatalf("unexpected item: %+v", item)
	}
	if item.Output == "" {
		t.Fatal("expected non-empty output payload")
	}
}

func TestRuntimeValidationErrorDoesNotCallHandler(t *testing.T) {
	var called atomic.Int32
	r := NewRegistry()
	r.MustRegister(Definition{
		Name: "search_docs",
		InputSchema: &ObjectSchema{
			Fields: map[string]FieldSchema{
				"query": {Type: FieldTypeString, Required: true},
			},
		},
		Handler: func(ctx context.Context, input map[string]any) (any, error) {
			called.Add(1)
			return nil, nil
		},
	})
	rt := &Runtime{Registry: r, Store: NewMemoryStore()}
	res := rt.Execute(context.Background(), CallRequest{CallID: "call-2", ToolName: "search_docs", RawArguments: `{"query":1}`})
	if res.Envelope.OK {
		t.Fatal("expected validation failure")
	}
	if res.Envelope.Error == nil || res.Envelope.Error.Code != ErrorCodeValidationError {
		t.Fatalf("unexpected error: %+v", res.Envelope.Error)
	}
	if !res.Envelope.Meta.ValidationFailed {
		t.Fatal("expected validation_failed meta flag")
	}
	if got := called.Load(); got != 0 {
		t.Fatalf("handler called %d times", got)
	}
}

func TestRuntimeTimeout(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(Definition{
		Name:    "slow_tool",
		Timeout: 20 * time.Millisecond,
		Handler: func(ctx context.Context, input map[string]any) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	rt := &Runtime{Registry: r, Store: NewMemoryStore()}
	res := rt.Execute(context.Background(), CallRequest{CallID: "call-3", ToolName: "slow_tool", RawArguments: `{}`})
	if res.Envelope.OK {
		t.Fatal("expected timeout failure")
	}
	if res.Record.Status != StatusTimedOut {
		t.Fatalf("status = %s, want %s", res.Record.Status, StatusTimedOut)
	}
	if res.Envelope.Error == nil || res.Envelope.Error.Code != ErrorCodeTimeout {
		t.Fatalf("unexpected error: %+v", res.Envelope.Error)
	}
	if !res.Envelope.Meta.TimedOut {
		t.Fatal("expected timed_out meta flag")
	}
}

func TestRuntimeDuplicateCallIDDedupesExecution(t *testing.T) {
	var calls atomic.Int32
	r := NewRegistry()
	r.MustRegister(Definition{
		Name: "side_effect_tool",
		Handler: func(ctx context.Context, input map[string]any) (any, error) {
			calls.Add(1)
			return map[string]any{"done": true}, nil
		},
	})
	store := NewMemoryStore()
	rt := &Runtime{Registry: r, Store: store}

	first := rt.Execute(context.Background(), CallRequest{CallID: "call-dup", ToolName: "side_effect_tool", RawArguments: `{}`})
	second := rt.Execute(context.Background(), CallRequest{CallID: "call-dup", ToolName: "side_effect_tool", RawArguments: `{}`})

	if !first.Envelope.OK {
		t.Fatalf("first call failed: %+v", first.Envelope.Error)
	}
	if !second.Duplicate {
		t.Fatal("expected duplicate result")
	}
	if !second.Envelope.Meta.Duplicate {
		t.Fatal("expected duplicate meta flag")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("handler executed %d times, want 1", got)
	}
}

func TestRuntimeUnknownToolAndHandlerError(t *testing.T) {
	rt := &Runtime{Registry: NewRegistry(), Store: NewMemoryStore()}
	unknown := rt.Execute(context.Background(), CallRequest{CallID: "call-u", ToolName: "missing", RawArguments: `{}`})
	if unknown.Envelope.OK || unknown.Envelope.Error == nil || unknown.Envelope.Error.Code != ErrorCodeUnknownTool {
		t.Fatalf("unexpected unknown tool result: %+v", unknown.Envelope)
	}

	rt.Registry.MustRegister(Definition{
		Name: "boom",
		Handler: func(ctx context.Context, input map[string]any) (any, error) {
			return nil, errors.New("boom")
		},
	})
	failed := rt.Execute(context.Background(), CallRequest{CallID: "call-b", ToolName: "boom", RawArguments: `{}`})
	if failed.Envelope.OK || failed.Envelope.Error == nil || failed.Envelope.Error.Code != ErrorCodeHandlerError {
		t.Fatalf("unexpected handler error result: %+v", failed.Envelope)
	}
}

func TestRuntimeDuplicateCallIDConflictWhenArgsDiffer(t *testing.T) {
	var calls atomic.Int32
	r := NewRegistry()
	r.MustRegister(Definition{
		Name: "side_effect_tool",
		Handler: func(ctx context.Context, input map[string]any) (any, error) {
			calls.Add(1)
			return map[string]any{"done": true}, nil
		},
	})
	rt := &Runtime{Registry: r, Store: NewMemoryStore()}

	first := rt.Execute(context.Background(), CallRequest{CallID: "call-conflict", ToolName: "side_effect_tool", RawArguments: `{"v":1}`})
	second := rt.Execute(context.Background(), CallRequest{CallID: "call-conflict", ToolName: "side_effect_tool", RawArguments: `{"v":2}`})

	if !first.Envelope.OK {
		t.Fatalf("first call failed unexpectedly: %+v", first.Envelope.Error)
	}
	if !second.Duplicate {
		t.Fatal("expected duplicate conflict result")
	}
	if second.Envelope.Error == nil || second.Envelope.Error.Code != ErrorCodeDuplicateCallConflict {
		t.Fatalf("unexpected duplicate conflict error: %+v", second.Envelope.Error)
	}
	if !second.Envelope.Meta.Conflict {
		t.Fatal("expected conflict meta flag")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("handler executed %d times, want 1", got)
	}
}

func TestRuntimeRecoversHandlerPanic(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(Definition{
		Name: "panic_tool",
		Handler: func(ctx context.Context, input map[string]any) (any, error) {
			panic("boom panic")
		},
	})
	rt := &Runtime{Registry: r, Store: NewMemoryStore()}
	res := rt.Execute(context.Background(), CallRequest{CallID: "call-panic", ToolName: "panic_tool", RawArguments: `{}`})
	if res.Envelope.OK {
		t.Fatal("expected panic to be converted to error")
	}
	if res.Envelope.Error == nil || res.Envelope.Error.Code != ErrorCodePanic {
		t.Fatalf("unexpected error: %+v", res.Envelope.Error)
	}
}

func TestRuntimeCanceledClassification(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(Definition{
		Name: "cancel_tool",
		Handler: func(ctx context.Context, input map[string]any) (any, error) {
			return nil, context.Canceled
		},
	})
	rt := &Runtime{Registry: r, Store: NewMemoryStore()}
	res := rt.Execute(context.Background(), CallRequest{CallID: "call-cancel", ToolName: "cancel_tool", RawArguments: `{}`})
	if res.Envelope.OK {
		t.Fatal("expected canceled failure")
	}
	if res.Record.Status != StatusCanceled {
		t.Fatalf("status = %s, want %s", res.Record.Status, StatusCanceled)
	}
	if res.Envelope.Error == nil || res.Envelope.Error.Code != ErrorCodeCanceled {
		t.Fatalf("unexpected error: %+v", res.Envelope.Error)
	}
	if !res.Envelope.Meta.Canceled {
		t.Fatal("expected canceled meta flag")
	}
}

func TestStatsHookAndComposeHooks(t *testing.T) {
	stats := NewStatsHook()
	var seen atomic.Int32
	hook := ComposeHooks(stats, HookFunc(func(ctx context.Context, ev Event) {
		if ev.Type == EventFinish {
			seen.Add(1)
		}
	}))

	r := NewRegistry()
	r.MustRegister(Definition{
		Name: "echo",
		Handler: func(ctx context.Context, input map[string]any) (any, error) {
			return map[string]any{"ok": true}, nil
		},
	})
	rt := &Runtime{Registry: r, Store: NewMemoryStore(), Hook: hook}
	res := rt.Execute(context.Background(), CallRequest{CallID: "call-stats", ToolName: "echo", RawArguments: `{}`})
	if !res.Envelope.OK {
		t.Fatalf("unexpected failure: %+v", res.Envelope.Error)
	}

	snap := stats.Snapshot()
	if snap.TotalEvents == 0 {
		t.Fatal("expected events in stats")
	}
	if snap.ByEventType[EventStart] == 0 || snap.ByEventType[EventFinish] == 0 {
		t.Fatalf("missing expected event types: %#v", snap.ByEventType)
	}
	if snap.ByTool["echo"] == 0 {
		t.Fatalf("expected tool counter for echo: %#v", snap.ByTool)
	}
	if got := seen.Load(); got == 0 {
		t.Fatal("expected composed hook to receive finish event")
	}
}

func TestBuildResponseAppendWithFunctionCallOutput(t *testing.T) {
	env := Envelope{OK: true, Data: map[string]any{"x": 1}, Meta: EnvelopeMeta{CallID: "call-9", ToolName: "echo"}}
	payload, err := BuildResponseAppendWithFunctionCallOutput("call-9", env)
	if err != nil {
		t.Fatalf("build append payload error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("unmarshal append payload: %v", err)
	}
	if got := parsed["type"]; got != "response.append" {
		t.Fatalf("type = %#v, want response.append", got)
	}
	input, ok := parsed["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("unexpected input: %#v", parsed["input"])
	}
	item, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected item type: %#v", input[0])
	}
	if item["type"] != "function_call_output" || item["call_id"] != "call-9" {
		t.Fatalf("unexpected item: %#v", item)
	}
	if _, ok := item["output"].(string); !ok {
		t.Fatalf("expected string output, got %#v", item["output"])
	}
}
