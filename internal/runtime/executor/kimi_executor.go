package executor

import (
	"context"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

// KimiExecutor is a compatibility wrapper delegating to KimiExecutorRefactored.
type KimiExecutor struct {
	ref *KimiExecutorRefactored
}

// NewKimiExecutor creates a legacy-compatible Kimi executor wrapper.
func NewKimiExecutor(cfg *config.Config) *KimiExecutor {
	return &KimiExecutor{ref: NewKimiExecutorRefactored(cfg)}
}

func (e *KimiExecutor) Identifier() string { return e.ref.Identifier() }

func (e *KimiExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	return e.ref.PrepareRequest(req, auth)
}

func (e *KimiExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	return e.ref.HttpRequest(ctx, auth, req)
}

func (e *KimiExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.ref.Execute(ctx, auth, req, opts)
}

func (e *KimiExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return e.ref.ExecuteStream(ctx, auth, req, opts)
}

func (e *KimiExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.ref.CountTokens(ctx, auth, req, opts)
}

func (e *KimiExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return e.ref.Refresh(ctx, auth)
}
