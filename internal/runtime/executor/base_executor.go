package executor

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	log "github.com/sirupsen/logrus"
)

// ProviderConfig defines provider-specific configuration and behavior
type ProviderConfig interface {
	// GetIdentifier returns the provider identifier (e.g., "kimi", "gemini")
	GetIdentifier() string

	// GetCredentials extracts credentials from auth
	GetCredentials(auth *cliproxyauth.Auth) (apiKey, baseURL string)

	// GetEndpoint returns the API endpoint for the given action
	GetEndpoint(baseURL, model, action string, stream bool) string

	// ApplyHeaders applies provider-specific headers to the request
	ApplyHeaders(req *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool)

	// GetTranslatorFormat returns the translator format string for this provider
	GetTranslatorFormat() string

	// TransformRequestBody applies provider-specific transformations to the request body
	TransformRequestBody(body []byte, model string, stream bool) ([]byte, error)

	// TransformResponseBody applies provider-specific transformations to the response body
	TransformResponseBody(body []byte) []byte

	// ParseUsage extracts usage information from the response
	ParseUsage(data []byte, stream bool) usageDetail
}

// BaseExecutor provides common execution logic for all providers
type BaseExecutor struct {
	cfg      *config.Config
	provider ProviderConfig
}

// NewBaseExecutor creates a new base executor with the given provider config
func NewBaseExecutor(cfg *config.Config, provider ProviderConfig) *BaseExecutor {
	return &BaseExecutor{
		cfg:      cfg,
		provider: provider,
	}
}

// Execute performs a non-streaming request using common logic
func (e *BaseExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	apiKey, baseURL := e.provider.GetCredentials(auth)

	reporter := newUsageReporter(ctx, e.provider.GetIdentifier(), baseModel, auth)
	defer reporter.trackFailure(ctx, &err)

	// Translate request
	from := opts.SourceFormat
	to := sdktranslator.FromString(e.provider.GetTranslatorFormat())
	originalPayloadSource := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayloadSource = opts.OriginalRequest
	}
	originalPayload := bytes.Clone(originalPayloadSource)
	originalTranslated := sdktranslator.TranslateRequest(from, to, baseModel, originalPayload, false)
	body := sdktranslator.TranslateRequest(from, to, baseModel, bytes.Clone(req.Payload), false)

	// Apply thinking
	body, err = thinking.ApplyThinking(body, req.Model, from.String(), e.provider.GetTranslatorFormat(), e.provider.GetIdentifier())
	if err != nil {
		return resp, err
	}

	// Apply payload config
	requestedModel := payloadRequestedModel(opts, req.Model)
	body = applyPayloadConfigWithRoot(e.cfg, baseModel, to.String(), "", body, originalTranslated, requestedModel)

	// Provider-specific transformations
	body, err = e.provider.TransformRequestBody(body, baseModel, false)
	if err != nil {
		return resp, err
	}

	// Build and execute HTTP request
	url := e.provider.GetEndpoint(baseURL, baseModel, "execute", false)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return resp, err
	}

	e.provider.ApplyHeaders(httpReq, auth, apiKey, false)

	// Record request
	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.provider.GetIdentifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	// Execute request
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("%s executor: close response body error: %v", e.provider.GetIdentifier(), errClose)
		}
	}()

	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	// Handle error responses
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		appendAPIResponseChunk(ctx, e.cfg, b)
		logWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), b))
		err = statusErr{code: httpResp.StatusCode, msg: string(b)}
		return resp, err
	}

	// Read response
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	appendAPIResponseChunk(ctx, e.cfg, data)

	// Parse usage and transform response
	reporter.publish(ctx, e.provider.ParseUsage(data, false))
	data = e.provider.TransformResponseBody(data)

	// Translate response
	var param any
	out := sdktranslator.TranslateNonStream(ctx, to, from, req.Model, opts.OriginalRequest, body, data, &param)
	resp = cliproxyexecutor.Response{Payload: []byte(out), Headers: httpResp.Header.Clone()}
	return resp, nil
}

// ExecuteStream performs a streaming request using common logic
func (e *BaseExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	apiKey, baseURL := e.provider.GetCredentials(auth)

	reporter := newUsageReporter(ctx, e.provider.GetIdentifier(), baseModel, auth)
	defer reporter.trackFailure(ctx, &err)

	// Translate request
	from := opts.SourceFormat
	to := sdktranslator.FromString(e.provider.GetTranslatorFormat())
	originalPayloadSource := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayloadSource = opts.OriginalRequest
	}
	originalPayload := bytes.Clone(originalPayloadSource)
	originalTranslated := sdktranslator.TranslateRequest(from, to, baseModel, originalPayload, true)
	body := sdktranslator.TranslateRequest(from, to, baseModel, bytes.Clone(req.Payload), true)

	// Apply thinking
	body, err = thinking.ApplyThinking(body, req.Model, from.String(), e.provider.GetTranslatorFormat(), e.provider.GetIdentifier())
	if err != nil {
		return nil, err
	}

	// Apply payload config
	requestedModel := payloadRequestedModel(opts, req.Model)
	body = applyPayloadConfigWithRoot(e.cfg, baseModel, to.String(), "", body, originalTranslated, requestedModel)

	// Provider-specific transformations
	body, err = e.provider.TransformRequestBody(body, baseModel, true)
	if err != nil {
		return nil, err
	}

	// Build and execute HTTP request
	url := e.provider.GetEndpoint(baseURL, baseModel, "stream", true)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	e.provider.ApplyHeaders(httpReq, auth, apiKey, true)

	// Record request
	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.provider.GetIdentifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	// Execute request
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return nil, err
	}

	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	// Handle error responses
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		appendAPIResponseChunk(ctx, e.cfg, b)
		logWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), b))
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("%s executor: close response body error: %v", e.provider.GetIdentifier(), errClose)
		}
		err = statusErr{code: httpResp.StatusCode, msg: string(b)}
		return nil, err
	}

	// Stream response
	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer func() {
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("%s executor: close response body error: %v", e.provider.GetIdentifier(), errClose)
			}
		}()

		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 1_048_576) // 1MB
		var param any

		for scanner.Scan() {
			line := scanner.Bytes()
			appendAPIResponseChunk(ctx, e.cfg, line)

			// Parse usage if available
			usage := e.provider.ParseUsage(line, true)
			if usage.InputTokens > 0 || usage.OutputTokens > 0 {
				reporter.publish(ctx, usage)
			}

			// Transform and translate
			transformedLine := e.provider.TransformResponseBody(line)
			chunks := sdktranslator.TranslateStream(ctx, to, from, req.Model, opts.OriginalRequest, body, bytes.Clone(transformedLine), &param)
			for i := range chunks {
				out <- cliproxyexecutor.StreamChunk{Payload: []byte(chunks[i])}
			}
		}

		// Send [DONE] marker
		doneChunks := sdktranslator.TranslateStream(ctx, to, from, req.Model, opts.OriginalRequest, body, []byte("[DONE]"), &param)
		for i := range doneChunks {
			out <- cliproxyexecutor.StreamChunk{Payload: []byte(doneChunks[i])}
		}

		if errScan := scanner.Err(); errScan != nil {
			recordAPIResponseError(ctx, e.cfg, errScan)
			reporter.publishFailure(ctx)
			out <- cliproxyexecutor.StreamChunk{Err: errScan}
		}
	}()

	return &cliproxyexecutor.StreamResult{Headers: httpResp.Header.Clone(), Chunks: out}, nil
}
