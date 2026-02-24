package executor

import (
	"context"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

// GeminiCLIExecutor is a compatibility wrapper over the refactored Gemini CLI executor.
// Behavior is preserved while provider-specific logic lives in gemini_cli_provider.go.
type GeminiCLIExecutor struct {
	ref *GeminiCLIExecutorRefactored
}

// NewGeminiCLIExecutor creates a Gemini CLI executor instance.
func NewGeminiCLIExecutor(cfg *config.Config) *GeminiCLIExecutor {
	return &GeminiCLIExecutor{ref: NewGeminiCLIExecutorRefactored(cfg)}
}

// Identifier returns the executor identifier.
func (e *GeminiCLIExecutor) Identifier() string {
	return e.ref.Identifier()
}

// PrepareRequest injects Gemini CLI credentials into the outgoing HTTP request.
func (e *GeminiCLIExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	return e.ref.PrepareRequest(req, auth)
}

// HttpRequest injects Gemini CLI credentials into the request and executes it.
func (e *GeminiCLIExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	return e.ref.HttpRequest(ctx, auth, req)
}

// Execute performs a non-streaming request to the Gemini CLI API.
func (e *GeminiCLIExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.ref.Execute(ctx, auth, req, opts)
}

// ExecuteStream performs a streaming request to the Gemini CLI API.
func (e *GeminiCLIExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return e.ref.ExecuteStream(ctx, auth, req, opts)
}

// CountTokens counts tokens for the given request using the Gemini CLI API.
func (e *GeminiCLIExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.ref.CountTokens(ctx, auth, req, opts)
}

// Refresh refreshes the authentication credentials (no-op for Gemini CLI).
func (e *GeminiCLIExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return e.ref.Refresh(ctx, auth)
}
