package toolcall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type DuplicateCompletedPolicy string

const (
	DuplicateCompletedReturnCached DuplicateCompletedPolicy = "return_cached"
	DuplicateCompletedFail         DuplicateCompletedPolicy = "fail"
)

type Runtime struct {
	Registry                 *Registry
	Store                    Store
	DefaultTimeout           time.Duration
	Hook                     Hook
	Now                      func() time.Time
	DuplicateCompletedPolicy DuplicateCompletedPolicy
}

func (rt *Runtime) Execute(ctx context.Context, req CallRequest) Result {
	if ctx == nil {
		ctx = context.Background()
	}
	nowFn := rt.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	start := nowFn()
	req.CallID = strings.TrimSpace(req.CallID)
	req.ToolName = strings.TrimSpace(req.ToolName)

	invalid := validateCallRequest(req)
	if invalid != nil {
		res := failureResult(req, start, nowFn(), StatusFailed, invalid, EnvelopeMeta{})
		rt.emit(ctx, Event{Type: EventFinish, Request: req, ToolName: req.ToolName, Status: res.Record.Status, ErrorCode: invalid.Code, LatencyMS: res.Envelope.Meta.LatencyMS})
		return res
	}

	def, ok := rt.lookup(req.ToolName)
	if !ok {
		err := &ToolError{Code: ErrorCodeUnknownTool, Message: fmt.Sprintf("unknown tool %q", req.ToolName), Retryable: false}
		res := failureResult(req, start, nowFn(), StatusFailed, err, EnvelopeMeta{ToolName: req.ToolName})
		rt.completeStore(res.Record)
		rt.emit(ctx, Event{Type: EventFinish, Request: req, ToolName: req.ToolName, Status: res.Record.Status, ErrorCode: err.Code, LatencyMS: res.Envelope.Meta.LatencyMS})
		return res
	}

	argsHash := hashArgs(req.RawArguments)
	seed := Record{
		CallID:    req.CallID,
		ToolName:  def.Name,
		ArgsHash:  argsHash,
		Status:    StatusStarted,
		StartedAt: start,
	}
	if dup, okDup := rt.handleDuplicate(ctx, req, seed, nowFn); okDup {
		return dup
	}

	rt.emit(ctx, Event{
		Type:        EventStart,
		Request:     req,
		ToolName:    def.Name,
		ToolVersion: def.Version,
		ArgsHash:    argsHash,
		Status:      StatusStarted,
	})
	rt.emit(ctx, Event{Type: EventValidateStart, Request: req, ToolName: def.Name, ToolVersion: def.Version, ArgsHash: argsHash, Status: StatusStarted})

	validated, err := validateAgainstDefinition(def, req.RawArguments)
	if err != nil {
		te := validationToolError(err)
		end := nowFn()
		res := failureResult(req, start, end, StatusFailed, te, EnvelopeMeta{ToolName: def.Name, ToolVersion: def.Version, ArgsHash: argsHash, ValidationFailed: true})
		res.Record.ToolName = def.Name
		res.Record.ArgsHash = seed.ArgsHash
		rt.completeStore(res.Record)
		rt.emit(ctx, Event{Type: EventValidateDone, Request: req, ToolName: def.Name, ToolVersion: def.Version, ArgsHash: argsHash, Status: StatusFailed, ErrorCode: te.Code, Message: te.Message})
		rt.emit(ctx, Event{Type: EventFinish, Request: req, ToolName: def.Name, ToolVersion: def.Version, ArgsHash: argsHash, Status: res.Record.Status, ErrorCode: te.Code, LatencyMS: res.Envelope.Meta.LatencyMS})
		return res
	}
	rt.emit(ctx, Event{Type: EventValidateDone, Request: req, ToolName: def.Name, ToolVersion: def.Version, ArgsHash: argsHash, Status: StatusSucceeded})

	timeout := def.Timeout
	if timeout <= 0 {
		timeout = rt.DefaultTimeout
	}
	execCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	rt.emit(ctx, Event{Type: EventExecuteStart, Request: req, ToolName: def.Name, ToolVersion: def.Version, ArgsHash: argsHash, Status: StatusStarted})
	data, execErr := rt.callHandler(execCtx, def.Handler, validated)
	end := nowFn()
	if execErr != nil {
		te, status, meta := classifyHandlerError(def, req.CallID, argsHash, start, end, execErr)
		res := failureResult(req, start, end, status, te, meta)
		res.Record.ToolName = def.Name
		res.Record.ArgsHash = seed.ArgsHash
		rt.completeStore(res.Record)
		rt.emit(ctx, Event{Type: EventExecuteDone, Request: req, ToolName: def.Name, ToolVersion: def.Version, ArgsHash: argsHash, Status: status, ErrorCode: te.Code, LatencyMS: res.Envelope.Meta.LatencyMS, Message: te.Message})
		rt.emit(ctx, Event{Type: EventFinish, Request: req, ToolName: def.Name, ToolVersion: def.Version, ArgsHash: argsHash, Status: res.Record.Status, ErrorCode: te.Code, LatencyMS: res.Envelope.Meta.LatencyMS})
		return res
	}

	latency := end.Sub(start).Milliseconds()
	env := Envelope{
		OK:   true,
		Data: data,
		Meta: EnvelopeMeta{ToolName: def.Name, ToolVersion: def.Version, CallID: req.CallID, ArgsHash: argsHash, LatencyMS: latency},
	}
	rec := Record{
		CallID:    req.CallID,
		ToolName:  def.Name,
		ArgsHash:  seed.ArgsHash,
		Status:    StatusSucceeded,
		StartedAt: start,
		EndedAt:   end,
		Envelope:  env,
	}
	res := Result{Envelope: env, Record: rec, Validated: validated}
	rt.completeStore(rec)
	rt.emit(ctx, Event{Type: EventExecuteDone, Request: req, ToolName: def.Name, ToolVersion: def.Version, ArgsHash: argsHash, Status: StatusSucceeded, LatencyMS: latency})
	rt.emit(ctx, Event{Type: EventFinish, Request: req, ToolName: def.Name, ToolVersion: def.Version, ArgsHash: argsHash, Status: rec.Status, LatencyMS: latency})
	return res
}

func (rt *Runtime) handleDuplicate(ctx context.Context, req CallRequest, seed Record, nowFn func() time.Time) (Result, bool) {
	if rt == nil || rt.Store == nil || req.CallID == "" {
		return Result{}, false
	}
	existing, duplicate := rt.Store.Begin(seed)
	if !duplicate {
		return Result{}, false
	}

	end := nowFn()
	if existing.ArgsHash != "" && seed.ArgsHash != "" && existing.ArgsHash != seed.ArgsHash {
		te := &ToolError{
			Code:      ErrorCodeDuplicateCallConflict,
			Message:   "same call_id received with different arguments",
			Retryable: false,
		}
		res := failureResult(req, seed.StartedAt, end, StatusDuplicate, te, EnvelopeMeta{
			ToolName:  seed.ToolName,
			ArgsHash:  seed.ArgsHash,
			Duplicate: true,
			Conflict:  true,
		})
		res.Record.ToolName = seed.ToolName
		res.Record.ArgsHash = seed.ArgsHash
		res.Duplicate = true
		rt.emit(ctx, Event{Type: EventDuplicate, Request: req, ToolName: seed.ToolName, ArgsHash: seed.ArgsHash, Status: StatusDuplicate, ErrorCode: te.Code, Duplicate: true, Message: te.Message})
		return res, true
	}
	if existing.Status != "" && existing.Status != StatusStarted {
		if rt.duplicateCompletedPolicy() == DuplicateCompletedFail {
			te := &ToolError{
				Code:      ErrorCodeDuplicateCallConflict,
				Message:   "duplicate completed tool call replay is disallowed",
				Retryable: false,
			}
			res := failureResult(req, seed.StartedAt, end, StatusDuplicate, te, EnvelopeMeta{ToolName: seed.ToolName, ArgsHash: seed.ArgsHash, Duplicate: true, Conflict: true})
			res.Record.ToolName = seed.ToolName
			res.Record.ArgsHash = seed.ArgsHash
			res.Duplicate = true
			rt.emit(ctx, Event{Type: EventDuplicate, Request: req, ToolName: seed.ToolName, ArgsHash: seed.ArgsHash, Status: StatusDuplicate, ErrorCode: te.Code, Duplicate: true, Message: te.Message})
			return res, true
		}
		env := existing.Envelope
		env.Meta.Duplicate = true
		env.Meta.ReplayCached = true
		env.Meta.CallID = req.CallID
		env.Meta.ArgsHash = seed.ArgsHash
		if env.Meta.ToolName == "" {
			env.Meta.ToolName = existing.ToolName
		}
		latency := end.Sub(seed.StartedAt).Milliseconds()
		if env.Meta.LatencyMS == 0 {
			env.Meta.LatencyMS = latency
		}
		res := Result{Envelope: env, Record: existing, Duplicate: true}
		rt.emit(ctx, Event{Type: EventDuplicate, Request: req, ToolName: existing.ToolName, ArgsHash: seed.ArgsHash, Status: StatusDuplicate, Duplicate: true})
		return res, true
	}

	te := &ToolError{Code: ErrorCodeDuplicateInFlight, Message: "duplicate tool call is already in progress", Retryable: true}
	res := failureResult(req, seed.StartedAt, end, StatusDuplicate, te, EnvelopeMeta{ToolName: seed.ToolName, ArgsHash: seed.ArgsHash, Duplicate: true})
	res.Record.ToolName = seed.ToolName
	res.Record.ArgsHash = seed.ArgsHash
	res.Duplicate = true
	rt.emit(ctx, Event{Type: EventDuplicate, Request: req, ToolName: seed.ToolName, ArgsHash: seed.ArgsHash, Status: StatusDuplicate, ErrorCode: te.Code, Duplicate: true, Message: te.Message})
	return res, true
}

func (rt *Runtime) lookup(name string) (Definition, bool) {
	if rt == nil || rt.Registry == nil {
		return Definition{}, false
	}
	return rt.Registry.Get(name)
}

func (rt *Runtime) completeStore(rec Record) {
	if rt != nil && rt.Store != nil && rec.CallID != "" {
		rt.Store.Complete(rec)
	}
}

func (rt *Runtime) emit(ctx context.Context, ev Event) {
	if rt != nil && rt.Hook != nil {
		rt.Hook.OnToolEvent(ctx, ev)
	}
}

func validateCallRequest(req CallRequest) *ToolError {
	if strings.TrimSpace(req.CallID) == "" {
		return &ToolError{Code: ErrorCodeInvalidRequest, Message: "call_id is required", Retryable: false}
	}
	if strings.TrimSpace(req.ToolName) == "" {
		return &ToolError{Code: ErrorCodeInvalidRequest, Message: "tool_name is required", Retryable: false}
	}
	return nil
}

func validateAgainstDefinition(def Definition, rawArgs string) (map[string]any, error) {
	if def.InputSchema != nil {
		return def.InputSchema.ValidateRaw(rawArgs)
	}
	return ParseArgsObject(rawArgs)
}

func validationToolError(err error) *ToolError {
	if err == nil {
		return nil
	}
	return &ToolError{Code: ErrorCodeValidationError, Message: err.Error(), Retryable: false}
}

func classifyHandlerError(def Definition, callID string, argsHash string, start, end time.Time, err error) (*ToolError, Status, EnvelopeMeta) {
	latency := end.Sub(start).Milliseconds()
	meta := EnvelopeMeta{ToolName: def.Name, ToolVersion: def.Version, CallID: callID, ArgsHash: argsHash, LatencyMS: latency}
	if errors.Is(err, context.DeadlineExceeded) {
		meta.TimedOut = true
		return &ToolError{Code: ErrorCodeTimeout, Message: "tool execution timed out", Retryable: true}, StatusTimedOut, meta
	}
	if errors.Is(err, context.Canceled) {
		meta.Canceled = true
		return &ToolError{Code: ErrorCodeCanceled, Message: "tool execution canceled", Retryable: true}, StatusCanceled, meta
	}
	var te *ToolError
	if errors.As(err, &te) && te != nil {
		return te, StatusFailed, meta
	}
	return &ToolError{Code: ErrorCodeHandlerError, Message: err.Error(), Retryable: false}, StatusFailed, meta
}

func failureResult(req CallRequest, start, end time.Time, status Status, err *ToolError, meta EnvelopeMeta) Result {
	latency := end.Sub(start).Milliseconds()
	if meta.CallID == "" {
		meta.CallID = req.CallID
	}
	if meta.ToolName == "" {
		meta.ToolName = req.ToolName
	}
	if meta.ArgsHash == "" {
		meta.ArgsHash = hashArgs(req.RawArguments)
	}
	if meta.LatencyMS == 0 {
		meta.LatencyMS = latency
	}
	env := Envelope{OK: false, Data: nil, Error: err, Meta: meta}
	rec := Record{
		CallID:    req.CallID,
		ToolName:  req.ToolName,
		ArgsHash:  hashArgs(req.RawArguments),
		Status:    status,
		StartedAt: start,
		EndedAt:   end,
		Envelope:  env,
	}
	if err != nil {
		rec.ErrorCode = err.Code
	}
	return Result{Envelope: env, Record: rec}
}

func (rt *Runtime) duplicateCompletedPolicy() DuplicateCompletedPolicy {
	if rt == nil || rt.DuplicateCompletedPolicy == "" {
		return DuplicateCompletedReturnCached
	}
	return rt.DuplicateCompletedPolicy
}

func (rt *Runtime) callHandler(ctx context.Context, h Handler, input map[string]any) (_ any, err error) {
	if h == nil {
		return nil, &ToolError{Code: ErrorCodeInternalError, Message: "tool handler is nil", Retryable: false}
	}
	defer func() {
		if r := recover(); r != nil {
			err = &ToolError{
				Code:      ErrorCodePanic,
				Message:   fmt.Sprintf("tool handler panic: %v", r),
				Retryable: false,
			}
		}
	}()
	return h(ctx, input)
}

func hashArgs(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
