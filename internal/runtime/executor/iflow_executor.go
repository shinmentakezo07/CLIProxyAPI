package executor

import (
	"context"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

// IFlowExecutor is a compatibility wrapper delegating to IFlowExecutorRefactored.
type IFlowExecutor struct {
	ref *IFlowExecutorRefactored
}

// NewIFlowExecutor creates a legacy-compatible IFlow executor wrapper.
func NewIFlowExecutor(cfg *config.Config) *IFlowExecutor {
	return &IFlowExecutor{ref: NewIFlowExecutorRefactored(cfg)}
}

func (e *IFlowExecutor) Identifier() string { return e.ref.Identifier() }

func (e *IFlowExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	return e.ref.PrepareRequest(req, auth)
}

func (e *IFlowExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	return e.ref.HttpRequest(ctx, auth, req)
}

func (e *IFlowExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.ref.Execute(ctx, auth, req, opts)
}

func (e *IFlowExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return e.ref.ExecuteStream(ctx, auth, req, opts)
}

func (e *IFlowExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.ref.CountTokens(ctx, auth, req, opts)
}

func (e *IFlowExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return e.ref.Refresh(ctx, auth)
}
