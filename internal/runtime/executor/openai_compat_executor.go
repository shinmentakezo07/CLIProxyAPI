package executor

import (
	"context"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

// OpenAICompatExecutor is a compatibility wrapper delegating to OpenAICompatExecutorRefactored.
type OpenAICompatExecutor struct {
	ref *OpenAICompatExecutorRefactored
}

// NewOpenAICompatExecutor creates a legacy-compatible OpenAI compatibility executor wrapper.
func NewOpenAICompatExecutor(provider string, cfg *config.Config) *OpenAICompatExecutor {
	return &OpenAICompatExecutor{ref: NewOpenAICompatExecutorRefactored(provider, cfg)}
}

func (e *OpenAICompatExecutor) Identifier() string { return e.ref.Identifier() }

func (e *OpenAICompatExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	return e.ref.PrepareRequest(req, auth)
}

func (e *OpenAICompatExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	return e.ref.HttpRequest(ctx, auth, req)
}

func (e *OpenAICompatExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.ref.Execute(ctx, auth, req, opts)
}

func (e *OpenAICompatExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return e.ref.ExecuteStream(ctx, auth, req, opts)
}

func (e *OpenAICompatExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.ref.CountTokens(ctx, auth, req, opts)
}

func (e *OpenAICompatExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return e.ref.Refresh(ctx, auth)
}
