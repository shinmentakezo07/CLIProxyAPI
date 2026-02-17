package executor

import (
	"context"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestAuthLogFields(t *testing.T) {
	id, label, typ, val := authLogFields(nil)
	if id != "" || label != "" || typ != "" || val != "" {
		t.Fatalf("expected empty fields for nil auth, got %q %q %q %q", id, label, typ, val)
	}

	auth := &cliproxyauth.Auth{ID: "id1", Label: "label1", Attributes: map[string]string{"api_key": "secret"}}
	id, label, typ, val = authLogFields(auth)
	if id != "id1" || label != "label1" || typ == "" || val == "" {
		t.Fatalf("unexpected fields: %q %q %q %q", id, label, typ, val)
	}
}

func TestHandleNon2xxResponse(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"bad"}`))}
	if err := handleNon2xxResponse(context.Background(), nil, resp); err == nil {
		t.Fatal("expected status error")
	}
}

func TestBuildTranslatedPayload_BeforeAfterHooks(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	out, original, err := buildTranslatedPayload(translatedPayloadOptions{
		cfg:             nil,
		baseModel:       "gpt-5",
		reqModel:        "gpt-5",
		providerKey:     "openai",
		from:            "openai",
		to:              "openai",
		root:            "",
		payload:         payload,
		originalPayload: payload,
		requestedModel:  "gpt-5",
		stream:          false,
		applyThinking:   false,
		thinkingBefore:  false,
		beforePayload: func(body []byte) []byte {
			updated, _ := sjson.SetBytes(body, "metadata.before", true)
			return updated
		},
		afterPayload: func(body []byte) []byte {
			updated, _ := sjson.SetBytes(body, "metadata.after", true)
			return updated
		},
	})
	if err != nil {
		t.Fatalf("buildTranslatedPayload error: %v", err)
	}
	if len(original) == 0 {
		t.Fatal("expected original translated payload")
	}
	if !gjson.GetBytes(out, "metadata.before").Bool() || !gjson.GetBytes(out, "metadata.after").Bool() {
		t.Fatalf("expected before/after hooks to run: %s", string(out))
	}
}

func TestExecuteWithPreparedRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "ok" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("missing header"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := executeWithPreparedRequest(context.Background(), nil, nil, req, func(req *http.Request, _ *cliproxyauth.Auth) error {
		req.Header.Set("X-Test", "ok")
		return nil
	})
	if err != nil {
		t.Fatalf("executeWithPreparedRequest error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}
