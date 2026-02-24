package executor

import (
	"context"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

// QwenExecutor is a compatibility wrapper delegating to QwenExecutorRefactored.
type QwenExecutor struct {
	ref *QwenExecutorRefactored
}

// NewQwenExecutor creates a legacy-compatible Qwen executor wrapper.
func NewQwenExecutor(cfg *config.Config) *QwenExecutor {
	return &QwenExecutor{ref: NewQwenExecutorRefactored(cfg)}
}

func (e *QwenExecutor) Identifier() string { return e.ref.Identifier() }

func (e *QwenExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	return e.ref.PrepareRequest(req, auth)
}

func (e *QwenExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	return e.ref.HttpRequest(ctx, auth, req)
}

func (e *QwenExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.ref.Execute(ctx, auth, req, opts)
}

func (e *QwenExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return e.ref.ExecuteStream(ctx, auth, req, opts)
}

func (e *QwenExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.ref.CountTokens(ctx, auth, req, opts)
}

func (e *QwenExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return e.ref.Refresh(ctx, auth)
}
