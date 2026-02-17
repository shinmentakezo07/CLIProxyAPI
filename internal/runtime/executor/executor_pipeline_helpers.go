package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type requestPrepareFunc func(req *http.Request, auth *cliproxyauth.Auth) error

func executeWithPreparedRequest(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, req *http.Request, prepareFn requestPrepareFunc) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if prepareFn != nil {
		if err := prepareFn(httpReq, auth); err != nil {
			return nil, err
		}
	}
	httpClient := newProxyAwareHTTPClient(ctx, cfg, auth, 0)
	return httpClient.Do(httpReq)
}

func authLogFields(auth *cliproxyauth.Auth) (id, label, authType, authValue string) {
	if auth == nil {
		return "", "", "", ""
	}
	authType, authValue = auth.AccountInfo()
	return auth.ID, auth.Label, authType, authValue
}

func handleNon2xxResponse(ctx context.Context, cfg *config.Config, resp *http.Response) error {
	if resp == nil {
		return fmt.Errorf("executor: response is nil")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	appendAPIResponseChunk(ctx, cfg, body)
	logWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", resp.StatusCode, summarizeErrorBody(resp.Header.Get("Content-Type"), body))
	return statusErr{code: resp.StatusCode, msg: string(body)}
}
