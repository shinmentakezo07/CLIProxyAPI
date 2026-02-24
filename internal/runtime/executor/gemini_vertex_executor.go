package executor

import (
	"context"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

// GeminiVertexExecutor is a compatibility wrapper over the refactored Vertex executor.
// Behavior is preserved while provider-specific logic lives in gemini_vertex_provider.go.
type GeminiVertexExecutor struct {
	ref *GeminiVertexExecutorRefactored
}

// NewGeminiVertexExecutor creates a new Vertex AI Gemini executor instance.
func NewGeminiVertexExecutor(cfg *config.Config) *GeminiVertexExecutor {
	return &GeminiVertexExecutor{ref: NewGeminiVertexExecutorRefactored(cfg)}
}

// Identifier returns the executor identifier.
func (e *GeminiVertexExecutor) Identifier() string { return e.ref.Identifier() }

// PrepareRequest injects Vertex credentials into the outgoing HTTP request.
func (e *GeminiVertexExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	return e.ref.PrepareRequest(req, auth)
}

// HttpRequest injects Vertex credentials into the request and executes it.
func (e *GeminiVertexExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	return e.ref.HttpRequest(ctx, auth, req)
}

// Execute performs a non-streaming request to the Vertex AI API.
func (e *GeminiVertexExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.ref.Execute(ctx, auth, req, opts)
}

// ExecuteStream performs a streaming request to the Vertex AI API.
func (e *GeminiVertexExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return e.ref.ExecuteStream(ctx, auth, req, opts)
}

// CountTokens counts tokens for the given request using the Vertex AI API.
func (e *GeminiVertexExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.ref.CountTokens(ctx, auth, req, opts)
}

// Refresh refreshes the authentication credentials (no-op for Vertex).
func (e *GeminiVertexExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return e.ref.Refresh(ctx, auth)
}
