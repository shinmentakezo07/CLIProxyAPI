package pipeline

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

func TestHookFuncNilCallbacks(t *testing.T) {
	var h HookFunc
	h.BeforeExecute(context.Background(), nil)
	h.AfterExecute(context.Background(), nil, cliproxyexecutor.Response{}, nil)
	h.OnStreamChunk(context.Background(), nil, cliproxyexecutor.StreamChunk{})
}

func TestComposeHooksDispatchesInOrder(t *testing.T) {
	var calls []string
	appendCall := func(name string) { calls = append(calls, name) }

	h1 := HookFunc{
		Before: func(context.Context, *Context) { appendCall("h1.before") },
		After:  func(context.Context, *Context, cliproxyexecutor.Response, error) { appendCall("h1.after") },
		Stream: func(context.Context, *Context, cliproxyexecutor.StreamChunk) { appendCall("h1.stream") },
	}
	h2 := HookFunc{
		Before: func(context.Context, *Context) { appendCall("h2.before") },
		After:  func(context.Context, *Context, cliproxyexecutor.Response, error) { appendCall("h2.after") },
		Stream: func(context.Context, *Context, cliproxyexecutor.StreamChunk) { appendCall("h2.stream") },
	}

	composed := ComposeHooks(nil, h1, nil, h2)
	composed.BeforeExecute(context.Background(), &Context{})
	composed.OnStreamChunk(context.Background(), &Context{}, cliproxyexecutor.StreamChunk{Payload: []byte("x")})
	composed.AfterExecute(context.Background(), &Context{}, cliproxyexecutor.Response{Payload: []byte("y")}, errors.New("done"))

	want := []string{
		"h1.before", "h2.before",
		"h1.stream", "h2.stream",
		"h1.after", "h2.after",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestComposeHooksEmptyReturnsNoop(t *testing.T) {
	composed := ComposeHooks(nil, nil)
	if composed == nil {
		t.Fatalf("ComposeHooks returned nil")
	}
	composed.BeforeExecute(context.Background(), nil)
	composed.OnStreamChunk(context.Background(), nil, cliproxyexecutor.StreamChunk{})
	composed.AfterExecute(context.Background(), nil, cliproxyexecutor.Response{}, nil)
}

func TestRoundTripperProviderFunc(t *testing.T) {
	var nilFn RoundTripperProviderFunc
	if got := nilFn.RoundTripperFor(&cliproxyauth.Auth{ID: "a1"}); got != nil {
		t.Fatalf("nil adapter returned %#v, want nil", got)
	}

	transport := http.DefaultTransport
	var gotAuthID string
	adapter := RoundTripperProviderFunc(func(auth *cliproxyauth.Auth) http.RoundTripper {
		if auth != nil {
			gotAuthID = auth.ID
		}
		return transport
	})

	got := adapter.RoundTripperFor(&cliproxyauth.Auth{ID: "auth-123"})
	if got != transport {
		t.Fatalf("adapter returned %#v, want %#v", got, transport)
	}
	if gotAuthID != "auth-123" {
		t.Fatalf("auth ID = %q, want %q", gotAuthID, "auth-123")
	}
}
