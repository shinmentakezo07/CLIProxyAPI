package toolcall

import (
	"context"
	"encoding/json"
	"time"
)

type Handler func(ctx context.Context, input map[string]any) (any, error)

type Definition struct {
	Name          string
	Version       string
	Description   string
	Timeout       time.Duration
	SideEffecting bool
	InputSchema   *ObjectSchema
	Handler       Handler
}

type CallRequest struct {
	RequestID          string
	ExecutionSessionID string
	ResponseID         string
	CallID             string
	ToolName           string
	RawArguments       string
}

type ErrorCode string

const (
	ErrorCodeInvalidRequest        ErrorCode = "invalid_request"
	ErrorCodeUnknownTool           ErrorCode = "unknown_tool"
	ErrorCodeValidationError       ErrorCode = "validation_error"
	ErrorCodeTimeout               ErrorCode = "timeout"
	ErrorCodeCanceled              ErrorCode = "canceled"
	ErrorCodeHandlerError          ErrorCode = "handler_error"
	ErrorCodeInternalError         ErrorCode = "internal_error"
	ErrorCodePanic                 ErrorCode = "panic"
	ErrorCodeDuplicateInFlight     ErrorCode = "duplicate_in_flight"
	ErrorCodeDuplicateCallConflict ErrorCode = "duplicate_call_id_conflict"
)

type ToolError struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable"`
}

func (e *ToolError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

type EnvelopeMeta struct {
	ToolName         string `json:"tool_name,omitempty"`
	ToolVersion      string `json:"tool_version,omitempty"`
	CallID           string `json:"call_id,omitempty"`
	ArgsHash         string `json:"args_hash,omitempty"`
	LatencyMS        int64  `json:"latency_ms,omitempty"`
	Duplicate        bool   `json:"duplicate,omitempty"`
	ReplayCached     bool   `json:"replay_cached,omitempty"`
	Conflict         bool   `json:"conflict,omitempty"`
	TimedOut         bool   `json:"timed_out,omitempty"`
	Canceled         bool   `json:"canceled,omitempty"`
	ValidationFailed bool   `json:"validation_failed,omitempty"`
}

type Envelope struct {
	OK    bool         `json:"ok"`
	Data  any          `json:"data"`
	Error *ToolError   `json:"error"`
	Meta  EnvelopeMeta `json:"meta"`
}

type Status string

const (
	StatusStarted   Status = "started"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusTimedOut  Status = "timeout"
	StatusCanceled  Status = "canceled"
	StatusDuplicate Status = "duplicate"
)

type Record struct {
	CallID    string
	ToolName  string
	ArgsHash  string
	Status    Status
	StartedAt time.Time
	EndedAt   time.Time
	ErrorCode ErrorCode
	Envelope  Envelope
}

type Result struct {
	Envelope  Envelope
	Record    Record
	Duplicate bool
	Validated map[string]any
}

type EventType string

const (
	EventStart         EventType = "start"
	EventValidateStart EventType = "validate_start"
	EventValidateDone  EventType = "validate_done"
	EventExecuteStart  EventType = "execute_start"
	EventExecuteDone   EventType = "execute_done"
	EventFinish        EventType = "finish"
	EventDuplicate     EventType = "duplicate"
)

type Event struct {
	Type        EventType
	Request     CallRequest
	ToolName    string
	ToolVersion string
	ArgsHash    string
	Status      Status
	ErrorCode   ErrorCode
	LatencyMS   int64
	Duplicate   bool
	Message     string
}

type Hook interface {
	OnToolEvent(ctx context.Context, ev Event)
}

type HookFunc func(context.Context, Event)

func (f HookFunc) OnToolEvent(ctx context.Context, ev Event) {
	if f != nil {
		f(ctx, ev)
	}
}

type NoopHook struct{}

func (NoopHook) OnToolEvent(context.Context, Event) {}

type HookChain []Hook

func (hc HookChain) OnToolEvent(ctx context.Context, ev Event) {
	for _, h := range hc {
		if h == nil {
			continue
		}
		h.OnToolEvent(ctx, ev)
	}
}

func ComposeHooks(hooks ...Hook) Hook {
	filtered := make([]Hook, 0, len(hooks))
	for _, h := range hooks {
		if h != nil {
			filtered = append(filtered, h)
		}
	}
	switch len(filtered) {
	case 0:
		return NoopHook{}
	case 1:
		return filtered[0]
	default:
		return HookChain(filtered)
	}
}

type FunctionCallOutputItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

func BuildFunctionCallOutputItem(callID string, envelope Envelope) ([]byte, error) {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	item := FunctionCallOutputItem{
		Type:   "function_call_output",
		CallID: callID,
		Output: string(payload),
	}
	return json.Marshal(item)
}

type ResponsesWebsocketRequest struct {
	Type  string            `json:"type"`
	Input []json.RawMessage `json:"input"`
}

func BuildResponseAppendWithFunctionCallOutput(callID string, envelope Envelope) ([]byte, error) {
	item, err := BuildFunctionCallOutputItem(callID, envelope)
	if err != nil {
		return nil, err
	}
	req := ResponsesWebsocketRequest{
		Type:  "response.append",
		Input: []json.RawMessage{json.RawMessage(item)},
	}
	return json.Marshal(req)
}
