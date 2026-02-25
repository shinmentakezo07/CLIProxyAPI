package pipeline

import (
	"context"
	"net/http"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

// Context encapsulates execution state shared across middleware, translators, and executors.
type Context struct {
	// Request encapsulates the provider facing request payload.
	Request cliproxyexecutor.Request
	// Options carries execution flags (streaming, headers, etc.).
	Options cliproxyexecutor.Options
	// Auth references the credential selected for execution.
	Auth *cliproxyauth.Auth
	// Translator represents the pipeline responsible for schema adaptation.
	Translator *sdktranslator.Pipeline
	// HTTPClient allows middleware to customise the outbound transport per request.
	HTTPClient *http.Client
}

// Hook captures middleware callbacks around execution.
type Hook interface {
	BeforeExecute(ctx context.Context, execCtx *Context)
	AfterExecute(ctx context.Context, execCtx *Context, resp cliproxyexecutor.Response, err error)
	OnStreamChunk(ctx context.Context, execCtx *Context, chunk cliproxyexecutor.StreamChunk)
}

// NoopHook provides default no-op hook implementations.
type NoopHook struct{}

// BeforeExecute implements Hook.
func (NoopHook) BeforeExecute(context.Context, *Context) {}

// AfterExecute implements Hook.
func (NoopHook) AfterExecute(context.Context, *Context, cliproxyexecutor.Response, error) {}

// OnStreamChunk implements Hook.
func (NoopHook) OnStreamChunk(context.Context, *Context, cliproxyexecutor.StreamChunk) {}

// HookFunc aggregates optional hook implementations.
type HookFunc struct {
	Before func(context.Context, *Context)
	After  func(context.Context, *Context, cliproxyexecutor.Response, error)
	Stream func(context.Context, *Context, cliproxyexecutor.StreamChunk)
}

// BeforeExecute implements Hook.
func (h HookFunc) BeforeExecute(ctx context.Context, execCtx *Context) {
	if h.Before != nil {
		h.Before(ctx, execCtx)
	}
}

// AfterExecute implements Hook.
func (h HookFunc) AfterExecute(ctx context.Context, execCtx *Context, resp cliproxyexecutor.Response, err error) {
	if h.After != nil {
		h.After(ctx, execCtx, resp, err)
	}
}

// OnStreamChunk implements Hook.
func (h HookFunc) OnStreamChunk(ctx context.Context, execCtx *Context, chunk cliproxyexecutor.StreamChunk) {
	if h.Stream != nil {
		h.Stream(ctx, execCtx, chunk)
	}
}

// HookChain composes multiple hooks and invokes them in order.
type HookChain []Hook

// BeforeExecute implements Hook.
func (hooks HookChain) BeforeExecute(ctx context.Context, execCtx *Context) {
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		hook.BeforeExecute(ctx, execCtx)
	}
}

// AfterExecute implements Hook.
func (hooks HookChain) AfterExecute(ctx context.Context, execCtx *Context, resp cliproxyexecutor.Response, err error) {
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		hook.AfterExecute(ctx, execCtx, resp, err)
	}
}

// OnStreamChunk implements Hook.
func (hooks HookChain) OnStreamChunk(ctx context.Context, execCtx *Context, chunk cliproxyexecutor.StreamChunk) {
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		hook.OnStreamChunk(ctx, execCtx, chunk)
	}
}

// ComposeHooks compacts nil hooks and returns a single Hook implementation.
func ComposeHooks(hooks ...Hook) Hook {
	filtered := make([]Hook, 0, len(hooks))
	for _, hook := range hooks {
		if hook != nil {
			filtered = append(filtered, hook)
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

// RoundTripperProvider allows injection of custom HTTP transports per auth entry.
type RoundTripperProvider interface {
	RoundTripperFor(auth *cliproxyauth.Auth) http.RoundTripper
}

// RoundTripperProviderFunc adapts a function to RoundTripperProvider.
type RoundTripperProviderFunc func(auth *cliproxyauth.Auth) http.RoundTripper

// RoundTripperFor implements RoundTripperProvider.
func (f RoundTripperProviderFunc) RoundTripperFor(auth *cliproxyauth.Auth) http.RoundTripper {
	if f == nil {
		return nil
	}
	return f(auth)
}
