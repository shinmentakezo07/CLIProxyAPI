package executor

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/misc"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	codexClientVersion = "0.101.0"
	codexUserAgent     = "codex_cli_rs/0.101.0 (Mac OS 26.0.1; arm64) Apple_Terminal/464"
	codexDefaultURL    = "https://chatgpt.com/backend-api/codex"
)

var dataTag = []byte("data:")

// CodexProvider implements ProviderConfig for Codex API
type CodexProvider struct {
	cfg *config.Config
}

func NewCodexProvider(cfg *config.Config) *CodexProvider {
	return &CodexProvider{cfg: cfg}
}

func (p *CodexProvider) GetIdentifier() string {
	return "codex"
}

func (p *CodexProvider) GetCredentials(auth *cliproxyauth.Auth) (apiKey, baseURL string) {
	if auth == nil {
		return "", codexDefaultURL
	}

	if auth.Attributes != nil {
		apiKey = auth.Attributes["api_key"]
		baseURL = auth.Attributes["base_url"]
	}

	if apiKey == "" && auth.Metadata != nil {
		if v, ok := auth.Metadata["access_token"].(string); ok {
			apiKey = v
		}
	}

	if baseURL == "" {
		baseURL = codexDefaultURL
	}

	return apiKey, baseURL
}

func (p *CodexProvider) GetEndpoint(baseURL, model, action string, stream bool) string {
	return strings.TrimSuffix(baseURL, "/") + "/responses"
}

func (p *CodexProvider) ApplyHeaders(req *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	var ginHeaders http.Header
	if ginCtx, ok := req.Context().Value("gin").(*gin.Context); ok && ginCtx != nil && ginCtx.Request != nil {
		ginHeaders = ginCtx.Request.Header
	}

	misc.EnsureHeader(req.Header, ginHeaders, "Version", codexClientVersion)
	misc.EnsureHeader(req.Header, ginHeaders, "Session_id", uuid.NewString())
	misc.EnsureHeader(req.Header, ginHeaders, "User-Agent", codexUserAgent)
	misc.EnsureHeader(req.Header, ginHeaders, "x-codex-beta-features", "")
	misc.EnsureHeader(req.Header, ginHeaders, "x-codex-turn-state", "")
	misc.EnsureHeader(req.Header, ginHeaders, "x-codex-turn-metadata", "")
	misc.EnsureHeader(req.Header, ginHeaders, "x-responsesapi-include-timing-metrics", "")

	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("Connection", "Keep-Alive")

	// Check if using API key or OAuth
	isAPIKey := false
	if auth != nil && auth.Attributes != nil {
		if v := strings.TrimSpace(auth.Attributes["api_key"]); v != "" {
			isAPIKey = true
		}
	}

	if !isAPIKey {
		req.Header.Set("Originator", "codex_cli_rs")
		if auth != nil && auth.Metadata != nil {
			if accountID, ok := auth.Metadata["account_id"].(string); ok {
				req.Header.Set("Chatgpt-Account-Id", accountID)
			}
		}
	}

	// Apply custom headers from auth attributes
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
}

func (p *CodexProvider) GetTranslatorFormat() string {
	return "codex"
}

func (p *CodexProvider) TransformRequestBody(body []byte, model string, stream bool) ([]byte, error) {
	return normalizeCodexRequestBody(body, model, stream)
}

func normalizeCodexRequestBody(body []byte, model string, stream bool) ([]byte, error) {
	var err error

	// Set model in payload
	body, err = sjson.SetBytes(body, "model", model)
	if err != nil {
		return body, fmt.Errorf("codex executor: failed to set model in payload: %w", err)
	}

	// Set stream flag
	body, _ = sjson.SetBytes(body, "stream", stream)

	// Accept OpenAI-style alias and normalize it to Codex/Responses format.
	if !gjson.GetBytes(body, "reasoning.effort").Exists() {
		if effort := strings.TrimSpace(gjson.GetBytes(body, "reasoning_effort").String()); effort != "" {
			body, _ = sjson.SetBytes(body, "reasoning.effort", effort)
		}
	}
	body, _ = sjson.DeleteBytes(body, "reasoning_effort")
	body = applyCodexReasoningProfile(body)
	body, _ = sjson.DeleteBytes(body, "_cliproxy")
	body, _ = sjson.DeleteBytes(body, "agent_mode")

	// Delete Codex-specific fields that shouldn't be sent
	body, _ = sjson.DeleteBytes(body, "previous_response_id")
	body, _ = sjson.DeleteBytes(body, "prompt_cache_retention")
	body, _ = sjson.DeleteBytes(body, "safety_identifier")

	// Ensure instructions field exists
	if !gjson.GetBytes(body, "instructions").Exists() {
		body, _ = sjson.SetBytes(body, "instructions", "")
	}

	return body, nil
}

func (p *CodexProvider) TransformResponseBody(body []byte) []byte {
	// No transformation needed for Codex responses
	return body
}

func (p *CodexProvider) ParseUsage(data []byte, stream bool) usageDetail {
	// For Codex, usage is in response.completed events
	if payload, ok := codexCompletedEventPayload(data); ok {
		if detail, ok := parseCodexUsage(payload); ok {
			return detail
		}
	}
	return usageDetail{}
}

func codexCompletedEventPayload(data []byte) ([]byte, bool) {
	if !bytes.HasPrefix(data, dataTag) {
		return nil, false
	}
	payload := bytes.TrimSpace(data[len(dataTag):])
	if gjson.GetBytes(payload, "type").String() != "response.completed" {
		return nil, false
	}
	return payload, true
}

// applyCodexReasoningProfile injects a structured analysis scaffold into `instructions`
// when callers request a local reasoning profile and reasoning is enabled.
//
// Local control fields (removed before upstream request):
//   - `_cliproxy.reasoning_profile`: "deep", "deep_engineering"
//   - `_cliproxy.reasoning_prompt`: custom instruction text to append
//   - `_cliproxy.force_reasoning_profile`: bool (inject even if reasoning is disabled)
//
// This intentionally requests a visible rationale/analysis structure rather than hidden chain-of-thought.
func applyCodexReasoningProfile(body []byte) []byte {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return body
	}

	profile := strings.TrimSpace(gjson.GetBytes(body, "_cliproxy.reasoning_profile").String())
	custom := strings.TrimSpace(gjson.GetBytes(body, "_cliproxy.reasoning_prompt").String())
	force := gjson.GetBytes(body, "_cliproxy.force_reasoning_profile").Bool()

	if profile == "" && custom == "" {
		return body
	}
	if !force && !codexReasoningEnabled(body) {
		body, _ = sjson.DeleteBytes(body, "_cliproxy")
		return body
	}

	snippet := buildCodexReasoningProfilePrompt(profile, custom)
	if strings.TrimSpace(snippet) == "" {
		body, _ = sjson.DeleteBytes(body, "_cliproxy")
		return body
	}

	currentInstructions := strings.TrimSpace(gjson.GetBytes(body, "instructions").String())
	if currentInstructions == "" {
		body, _ = sjson.SetBytes(body, "instructions", snippet)
	} else {
		body, _ = sjson.SetBytes(body, "instructions", currentInstructions+"\n\n"+snippet)
	}

	body, _ = sjson.DeleteBytes(body, "_cliproxy")
	return body
}

func codexReasoningEnabled(body []byte) bool {
	for _, path := range []string{"reasoning.effort", "reasoning_effort"} {
		if effort := gjson.GetBytes(body, path); effort.Exists() {
			v := strings.ToLower(strings.TrimSpace(effort.String()))
			if v != "" && v != "none" {
				return true
			}
		}
	}
	return false
}

func buildCodexReasoningProfilePrompt(profile, custom string) string {
	var sections []string
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", "none", "off", "disabled":
		// no preset
	case "deep", "deep_engineering", "engineering":
		sections = append(sections, `Response profile:
- Prefer thoroughness over brevity.
- Use multi-lens analysis (user intent/cognitive load, technical tradeoffs/performance, accessibility, scalability/maintenance).
- Avoid shallow conclusions; justify decisions concretely.
- Provide a visible rationale summary instead of hidden internal reasoning.
- Structure the answer into:
  1. Architectural/Design Rationale
  2. Edge Cases and Failure Prevention
  3. Production-Ready Implementation`)
		sections = append(sections, codexDeepEngineeringStandardsPrompt())
		sections = append(sections, codexNormalResponseFormatPrompt())
	default:
		// Unknown preset names are ignored; custom text can still be applied.
	}
	if strings.TrimSpace(custom) != "" {
		sections = append(sections, custom)
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func codexDeepEngineeringStandardsPrompt() string {
	return strings.TrimSpace(`Engineering depth policy:
- Override Brevity: Immediately suspend the "Zero Fluff" rule when rigor is required.
- Maximum Depth: Engage in exhaustive, deep-level reasoning before writing a single line when implementation is requested.
- Prohibition: Never use surface-level logic. If the reasoning feels easy, dig deeper until the logic is irrefutable.

Multi-Dimensional Analysis (apply when relevant):
- Architectural: Separation of concerns, modularity, dependency direction, coupling.
- Performance: Time/space complexity, memory layout, I/O costs, concurrency pitfalls, hot-path optimization.
- Reliability: Error handling strategy, edge cases, failure modes, defensive programming.
- Scalability: Will the design hold at 10x load/scope, and what is the long-term maintenance burden.
- Security: Input validation, injection vectors, privilege boundaries, secrets management.
- Ecosystem Fit: Prefer solutions native to the language/framework/community.

Coding standards (all languages):
- Library & Framework Discipline (critical): If a library/framework/engine is active in the project, use it. Do not rebuild utilities the ecosystem already provides, and do not introduce redundant overlapping dependencies. Exception: wrappers/extensions are fine when the underlying primitive stays project-native.
- Language-specific awareness:
  - Python: Type hints, pathlib over os.path, f-strings, dataclasses/Pydantic when appropriate, async for I/O-bound work.
  - Lua: Respect table-driven design, metatables over class emulation unless already used, 1-based indexing, and target runtime differences.
  - JavaScript/TypeScript: Strict TypeScript where possible, framework conventions (React/Vue/Svelte), ESM over CJS.
  - Systems (Rust/Go/C/C++): Ownership/lifetime clarity, avoid unnecessary hot-path allocations, respect the language concurrency model.
  - Shell/Bash: Prefer POSIX when portability matters, use set -euo pipefail, quote variables.
  - SQL: Always parameterize queries unless a documented exception exists.
- Universal standards:
  - Error Handling: Never swallow errors silently; use the idiomatic error model.
  - Naming: Descriptive and consistent, following language/project conventions.
  - Structure: Logical module organization; no god files; avoid oversized functions.
  - Comments: Explain why, not what.`)
}

func codexNormalResponseFormatPrompt() string {
	return strings.TrimSpace(`Response format (normal mode):
1. Rationale: 1-2 sentences on the approach and why.
2. The Code: production-ready code using project-native libraries/frameworks.
3. Edge Cases: what can fail and how the implementation defends against it.

When describing reasoning, provide a visible rationale summary. Do not expose hidden chain-of-thought.`)
}
