package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// GeminiCLIExecutorRefactored uses OAuth2 authentication and implements model fallback retry
type GeminiCLIExecutorRefactored struct {
	cfg      *config.Config
	provider *GeminiCLIProvider
}

// NewGeminiCLIExecutorRefactored creates a new refactored Gemini CLI executor
func NewGeminiCLIExecutorRefactored(cfg *config.Config) *GeminiCLIExecutorRefactored {
	return &GeminiCLIExecutorRefactored{
		cfg:      cfg,
		provider: &GeminiCLIProvider{cfg: cfg},
	}
}

// Identifier returns the executor identifier
func (e *GeminiCLIExecutorRefactored) Identifier() string {
	return e.provider.GetIdentifier()
}

// PrepareRequest injects Gemini CLI credentials into the outgoing HTTP request
func (e *GeminiCLIExecutorRefactored) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	tokenSource, _, err := e.provider.PrepareTokenSource(req.Context(), auth)
	if err != nil {
		return err
	}
	tok, errTok := tokenSource.Token()
	if errTok != nil {
		return errTok
	}
	if tok.AccessToken == "" {
		return statusErr{code: http.StatusUnauthorized, msg: "missing access token"}
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	e.provider.ApplyHeaders(req, auth, "", false)
	return nil
}

// HttpRequest injects Gemini CLI credentials into the request and executes it
func (e *GeminiCLIExecutorRefactored) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("gemini-cli executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

// Execute performs a non-streaming request with model fallback retry
func (e *GeminiCLIExecutorRefactored) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	if opts.Alt == "responses/compact" {
		return resp, statusErr{code: http.StatusNotImplemented, msg: "/responses/compact not supported"}
	}

	baseModel := thinking.ParseSuffix(req.Model).ModelName
	tokenSource, baseTokenData, err := e.provider.PrepareTokenSource(ctx, auth)
	if err != nil {
		return resp, err
	}

	reporter := newUsageReporter(ctx, e.Identifier(), baseModel, auth)
	defer reporter.trackFailure(ctx, &err)

	from := opts.SourceFormat
	to := sdktranslator.FromString(e.provider.GetTranslatorFormat())

	originalPayloadSource := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayloadSource = opts.OriginalRequest
	}
	originalTranslated := sdktranslator.TranslateRequest(from, to, baseModel, originalPayloadSource, false)
	basePayload := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, false)

	basePayload, err = thinking.ApplyThinking(basePayload, req.Model, from.String(), to.String(), e.Identifier())
	if err != nil {
		return resp, err
	}

	basePayload, err = e.provider.TransformRequestBody(basePayload, baseModel, false)
	if err != nil {
		return resp, err
	}

	requestedModel := payloadRequestedModel(opts, req.Model)
	basePayload = applyPayloadConfigWithRoot(e.cfg, baseModel, "gemini", "request", basePayload, originalTranslated, requestedModel)

	action := "generateContent"
	if req.Metadata != nil {
		if a, _ := req.Metadata["action"].(string); a == "countTokens" {
			action = "countTokens"
		}
	}

	projectID := e.provider.GetProjectID(auth)
	models := e.provider.GetFallbackModels(baseModel)

	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	respCtx := context.WithValue(ctx, "alt", opts.Alt)

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}

	var lastStatus int
	var lastBody []byte

	// Try each model in fallback order
	for idx, attemptModel := range models {
		payload := append([]byte(nil), basePayload...)
		if action == "countTokens" {
			payload, _ = sjson.DeleteBytes(payload, "project")
			payload, _ = sjson.DeleteBytes(payload, "model")
		} else {
			payload, _ = sjson.SetBytes(payload, "project", projectID)
			payload, _ = sjson.SetBytes(payload, "model", attemptModel)
		}

		tok, errTok := tokenSource.Token()
		if errTok != nil {
			return resp, errTok
		}
		e.provider.UpdateTokenMetadata(auth, baseTokenData, tok)

		_, baseURL := e.provider.GetCredentials(auth)
		url := e.provider.GetEndpoint(baseURL, attemptModel, action, false)
		if opts.Alt != "" && action != "countTokens" {
			url = url + fmt.Sprintf("?$alt=%s", opts.Alt)
		}

		httpReq, errReq := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if errReq != nil {
			return resp, errReq
		}

		httpReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		e.provider.ApplyHeaders(httpReq, auth, "", false)

		recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
			URL:       url,
			Method:    http.MethodPost,
			Headers:   httpReq.Header.Clone(),
			Body:      payload,
			Provider:  e.Identifier(),
			AuthID:    authID,
			AuthLabel: authLabel,
			AuthType:  authType,
			AuthValue: authValue,
		})

		httpResp, errDo := httpClient.Do(httpReq)
		if errDo != nil {
			recordAPIResponseError(ctx, e.cfg, errDo)
			return resp, errDo
		}

		data, errRead := io.ReadAll(httpResp.Body)
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("gemini cli executor: close response body error: %v", errClose)
		}
		recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
		if errRead != nil {
			recordAPIResponseError(ctx, e.cfg, errRead)
			return resp, errRead
		}
		appendAPIResponseChunk(ctx, e.cfg, data)

		if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
			reporter.publish(ctx, e.provider.ParseUsage(data, false))
			var param any
			out := sdktranslator.TranslateNonStream(respCtx, to, from, attemptModel, opts.OriginalRequest, payload, data, &param)
			resp = cliproxyexecutor.Response{Payload: []byte(out), Headers: httpResp.Header.Clone()}
			return resp, nil
		}

		lastStatus = httpResp.StatusCode
		lastBody = append([]byte(nil), data...)
		logWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), data))

		if httpResp.StatusCode == 429 {
			if idx+1 < len(models) {
				log.Debugf("gemini cli executor: rate limited, retrying with next model: %s", models[idx+1])
			} else {
				log.Debug("gemini cli executor: rate limited, no additional fallback model")
			}
			continue
		}

		return resp, newGeminiStatusErr(httpResp.StatusCode, data)
	}

	if len(lastBody) > 0 {
		appendAPIResponseChunk(ctx, e.cfg, lastBody)
	}
	if lastStatus == 0 {
		lastStatus = 429
	}
	return resp, newGeminiStatusErr(lastStatus, lastBody)
}

// ExecuteStream performs a streaming request with model fallback retry
func (e *GeminiCLIExecutorRefactored) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	if opts.Alt == "responses/compact" {
		return nil, statusErr{code: http.StatusNotImplemented, msg: "/responses/compact not supported"}
	}

	baseModel := thinking.ParseSuffix(req.Model).ModelName
	tokenSource, baseTokenData, err := e.provider.PrepareTokenSource(ctx, auth)
	if err != nil {
		return nil, err
	}

	reporter := newUsageReporter(ctx, e.Identifier(), baseModel, auth)
	defer reporter.trackFailure(ctx, &err)

	from := opts.SourceFormat
	to := sdktranslator.FromString(e.provider.GetTranslatorFormat())

	originalPayloadSource := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayloadSource = opts.OriginalRequest
	}
	originalTranslated := sdktranslator.TranslateRequest(from, to, baseModel, originalPayloadSource, true)
	basePayload := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, true)

	basePayload, err = thinking.ApplyThinking(basePayload, req.Model, from.String(), to.String(), e.Identifier())
	if err != nil {
		return nil, err
	}

	basePayload, err = e.provider.TransformRequestBody(basePayload, baseModel, true)
	if err != nil {
		return nil, err
	}

	requestedModel := payloadRequestedModel(opts, req.Model)
	basePayload = applyPayloadConfigWithRoot(e.cfg, baseModel, "gemini", "request", basePayload, originalTranslated, requestedModel)

	projectID := e.provider.GetProjectID(auth)
	models := e.provider.GetFallbackModels(baseModel)

	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	respCtx := context.WithValue(ctx, "alt", opts.Alt)

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}

	var lastStatus int
	var lastBody []byte

	// Try each model in fallback order
	for idx, attemptModel := range models {
		payload := append([]byte(nil), basePayload...)
		payload, _ = sjson.SetBytes(payload, "project", projectID)
		payload, _ = sjson.SetBytes(payload, "model", attemptModel)

		tok, errTok := tokenSource.Token()
		if errTok != nil {
			return nil, errTok
		}
		e.provider.UpdateTokenMetadata(auth, baseTokenData, tok)

		_, baseURL := e.provider.GetCredentials(auth)
		url := e.provider.GetEndpoint(baseURL, attemptModel, "stream", true)
		if opts.Alt == "" {
			url = url + "?alt=sse"
		} else {
			url = url + fmt.Sprintf("?$alt=%s", opts.Alt)
		}

		httpReq, errReq := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if errReq != nil {
			return nil, errReq
		}

		httpReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		e.provider.ApplyHeaders(httpReq, auth, "", true)

		recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
			URL:       url,
			Method:    http.MethodPost,
			Headers:   httpReq.Header.Clone(),
			Body:      payload,
			Provider:  e.Identifier(),
			AuthID:    authID,
			AuthLabel: authLabel,
			AuthType:  authType,
			AuthValue: authValue,
		})

		httpResp, errDo := httpClient.Do(httpReq)
		if errDo != nil {
			recordAPIResponseError(ctx, e.cfg, errDo)
			return nil, errDo
		}
		recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

		if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
			data, errRead := io.ReadAll(httpResp.Body)
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("gemini cli executor: close response body error: %v", errClose)
			}
			if errRead != nil {
				recordAPIResponseError(ctx, e.cfg, errRead)
				return nil, errRead
			}
			appendAPIResponseChunk(ctx, e.cfg, data)
			lastStatus = httpResp.StatusCode
			lastBody = append([]byte(nil), data...)
			logWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), data))

			if httpResp.StatusCode == 429 {
				if idx+1 < len(models) {
					log.Debugf("gemini cli executor: rate limited, retrying with next model: %s", models[idx+1])
				} else {
					log.Debug("gemini cli executor: rate limited, no additional fallback model")
				}
				continue
			}
			return nil, newGeminiStatusErr(httpResp.StatusCode, data)
		}

		// Success - stream the response
		out := make(chan cliproxyexecutor.StreamChunk)
		go func(resp *http.Response, reqBody []byte, attemptModel string) {
			defer close(out)
			defer func() {
				if errClose := resp.Body.Close(); errClose != nil {
					log.Errorf("gemini cli executor: close response body error: %v", errClose)
				}
			}()

			if opts.Alt == "" {
				scanner := bufio.NewScanner(resp.Body)
				scanner.Buffer(nil, streamScannerBuffer)
				var param any
				for scanner.Scan() {
					line := scanner.Bytes()
					appendAPIResponseChunk(ctx, e.cfg, line)
					if detail, ok := parseGeminiCLIStreamUsage(line); ok {
						reporter.publish(ctx, detail)
					}
					if bytes.HasPrefix(line, dataTag) {
						segments := sdktranslator.TranslateStream(respCtx, to, from, attemptModel, opts.OriginalRequest, reqBody, bytes.Clone(line), &param)
						for i := range segments {
							out <- cliproxyexecutor.StreamChunk{Payload: []byte(segments[i])}
						}
					}
				}

				segments := sdktranslator.TranslateStream(respCtx, to, from, attemptModel, opts.OriginalRequest, reqBody, []byte("[DONE]"), &param)
				for i := range segments {
					out <- cliproxyexecutor.StreamChunk{Payload: []byte(segments[i])}
				}
				if errScan := scanner.Err(); errScan != nil {
					recordAPIResponseError(ctx, e.cfg, errScan)
					reporter.publishFailure(ctx)
					out <- cliproxyexecutor.StreamChunk{Err: errScan}
				}
				return
			}

			data, errRead := io.ReadAll(resp.Body)
			if errRead != nil {
				recordAPIResponseError(ctx, e.cfg, errRead)
				reporter.publishFailure(ctx)
				out <- cliproxyexecutor.StreamChunk{Err: errRead}
				return
			}
			appendAPIResponseChunk(ctx, e.cfg, data)
			reporter.publish(ctx, e.provider.ParseUsage(data, false))
			var param any
			segments := sdktranslator.TranslateStream(respCtx, to, from, attemptModel, opts.OriginalRequest, reqBody, data, &param)
			for i := range segments {
				out <- cliproxyexecutor.StreamChunk{Payload: []byte(segments[i])}
			}

			segments = sdktranslator.TranslateStream(respCtx, to, from, attemptModel, opts.OriginalRequest, reqBody, []byte("[DONE]"), &param)
			for i := range segments {
				out <- cliproxyexecutor.StreamChunk{Payload: []byte(segments[i])}
			}
		}(httpResp, append([]byte(nil), payload...), attemptModel)

		return &cliproxyexecutor.StreamResult{Headers: httpResp.Header.Clone(), Chunks: out}, nil
	}

	if len(lastBody) > 0 {
		appendAPIResponseChunk(ctx, e.cfg, lastBody)
	}
	if lastStatus == 0 {
		lastStatus = 429
	}
	return nil, newGeminiStatusErr(lastStatus, lastBody)
}

// CountTokens counts tokens for the given request
func (e *GeminiCLIExecutorRefactored) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	tokenSource, baseTokenData, err := e.provider.PrepareTokenSource(ctx, auth)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}

	from := opts.SourceFormat
	to := sdktranslator.FromString(e.provider.GetTranslatorFormat())

	models := e.provider.GetFallbackModels(baseModel)
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	respCtx := context.WithValue(ctx, "alt", opts.Alt)

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}

	var lastStatus int
	var lastBody []byte

	for range models {
		payload := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, false)

		payload, err = thinking.ApplyThinking(payload, req.Model, from.String(), to.String(), e.Identifier())
		if err != nil {
			return cliproxyexecutor.Response{}, err
		}

		payload, _ = sjson.DeleteBytes(payload, "project")
		payload, _ = sjson.DeleteBytes(payload, "model")
		payload, _ = sjson.DeleteBytes(payload, "request.safetySettings")
		payload, err = e.provider.TransformRequestBody(payload, baseModel, false)
		if err != nil {
			return cliproxyexecutor.Response{}, err
		}

		tok, errTok := tokenSource.Token()
		if errTok != nil {
			return cliproxyexecutor.Response{}, errTok
		}
		e.provider.UpdateTokenMetadata(auth, baseTokenData, tok)

		_, baseURL := e.provider.GetCredentials(auth)
		url := e.provider.GetEndpoint(baseURL, baseModel, "countTokens", false)
		if opts.Alt != "" {
			url = url + fmt.Sprintf("?$alt=%s", opts.Alt)
		}

		httpReq, errReq := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if errReq != nil {
			return cliproxyexecutor.Response{}, errReq
		}

		httpReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		e.provider.ApplyHeaders(httpReq, auth, "", false)

		recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
			URL:       url,
			Method:    http.MethodPost,
			Headers:   httpReq.Header.Clone(),
			Body:      payload,
			Provider:  e.Identifier(),
			AuthID:    authID,
			AuthLabel: authLabel,
			AuthType:  authType,
			AuthValue: authValue,
		})

		resp, errDo := httpClient.Do(httpReq)
		if errDo != nil {
			recordAPIResponseError(ctx, e.cfg, errDo)
			return cliproxyexecutor.Response{}, errDo
		}
		data, errRead := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		recordAPIResponseMetadata(ctx, e.cfg, resp.StatusCode, resp.Header.Clone())
		if errRead != nil {
			recordAPIResponseError(ctx, e.cfg, errRead)
			return cliproxyexecutor.Response{}, errRead
		}
		appendAPIResponseChunk(ctx, e.cfg, data)

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			count := gjson.GetBytes(data, "totalTokens").Int()
			translated := sdktranslator.TranslateTokenCount(respCtx, to, from, count, data)
			return cliproxyexecutor.Response{Payload: []byte(translated), Headers: resp.Header.Clone()}, nil
		}

		lastStatus = resp.StatusCode
		lastBody = append([]byte(nil), data...)
		if resp.StatusCode == 429 {
			log.Debugf("gemini cli executor: rate limited, retrying with next model")
			continue
		}
		break
	}

	if lastStatus == 0 {
		lastStatus = 429
	}
	return cliproxyexecutor.Response{}, newGeminiStatusErr(lastStatus, lastBody)
}

// Refresh refreshes the authentication credentials (no-op for Gemini CLI)
func (e *GeminiCLIExecutorRefactored) Refresh(_ context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return auth, nil
}
