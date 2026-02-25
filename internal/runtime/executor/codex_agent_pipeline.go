package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type codexAgentMode string

const (
	codexAgentModeNone            codexAgentMode = ""
	codexAgentModePlannerReviewer codexAgentMode = "planner-reviewer"
)

type codexAgentConfig struct {
	Mode        codexAgentMode
	Explicit    bool
	DisableAuto bool
}

func (c codexAgentConfig) Enabled() bool { return c.Mode != codexAgentModeNone }

type codexCompletedPassResult struct {
	Completed []byte
	Request   []byte
	Headers   http.Header
	Usage     usageDetail
	UsageOK   bool
}

func parseCodexAgentConfig(bodies ...[]byte) codexAgentConfig {
	for _, body := range bodies {
		if len(body) == 0 || !gjson.ValidBytes(body) {
			continue
		}
		for _, path := range []string{"_cliproxy.agent_mode", "agent_mode"} {
			raw := strings.TrimSpace(gjson.GetBytes(body, path).String())
			if raw == "" {
				continue
			}
			if codexAgentModeDisabled(raw) {
				return codexAgentConfig{Explicit: true, DisableAuto: true}
			}
			switch normalizeCodexAgentMode(raw) {
			case codexAgentModePlannerReviewer:
				return codexAgentConfig{Mode: codexAgentModePlannerReviewer, Explicit: true}
			}
		}
	}
	return codexAgentConfig{}
}

// resolveCodexAgentConfigForNonStream enables a default internal agent pipeline
// for normal Codex non-stream requests unless the caller explicitly disables it.
func resolveCodexAgentConfigForNonStream(bodies ...[]byte) codexAgentConfig {
	cfg := parseCodexAgentConfig(bodies...)
	if cfg.Enabled() || cfg.DisableAuto {
		return cfg
	}
	return codexAgentConfig{Mode: codexAgentModePlannerReviewer}
}

func normalizeCodexAgentMode(v string) codexAgentMode {
	s := strings.ToLower(strings.TrimSpace(v))
	s = strings.ReplaceAll(s, "_", "-")
	switch s {
	case "planner-reviewer", "plannerreviewer":
		return codexAgentModePlannerReviewer
	default:
		return codexAgentModeNone
	}
}

func codexAgentModeDisabled(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "off", "none", "disabled", "false", "0":
		return true
	default:
		return false
	}
}

func codexAgentModeSupportedForStreaming(body []byte) bool {
	cfg := parseCodexAgentConfig(body)
	return !cfg.Enabled()
}

func codexAgentCompatibilityIssue(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return "request body is not valid JSON"
	}
	if tools := gjson.GetBytes(body, "tools"); tools.Exists() {
		if !tools.IsArray() || len(tools.Array()) > 0 {
			return "tool-enabled requests are not supported"
		}
	}
	if gjson.GetBytes(body, "text.format").Exists() {
		return "structured text.format responses are not supported"
	}
	if gjson.GetBytes(body, "response_format").Exists() {
		return "structured response_format responses are not supported"
	}
	return ""
}

func codexExtractTaskText(body []byte) (string, bool) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return "", false
	}
	root := gjson.ParseBytes(body)
	var segments []string
	unsupported := false

	if inst := strings.TrimSpace(root.Get("instructions").String()); inst != "" {
		segments = append(segments, "Instructions:\n"+inst)
	}

	input := root.Get("input")
	if !input.Exists() {
		return strings.TrimSpace(strings.Join(segments, "\n\n")), !unsupported
	}

	if input.IsArray() {
		for _, item := range input.Array() {
			itemType := strings.TrimSpace(item.Get("type").String())
			switch itemType {
			case "message":
				role := strings.TrimSpace(item.Get("role").String())
				if role == "" {
					role = "user"
				}
				texts, okText := codexCollectMessageContentText(item.Get("content"))
				if !okText {
					unsupported = true
				}
				if len(texts) > 0 {
					segments = append(segments, strings.ToUpper(role)+":\n"+strings.Join(texts, "\n"))
				}
			case "function_call":
				name := strings.TrimSpace(item.Get("name").String())
				args := strings.TrimSpace(item.Get("arguments").String())
				if name != "" || args != "" {
					segments = append(segments, "FUNCTION_CALL:\nname="+name+"\nargs="+args)
				}
			case "function_call_output":
				out := strings.TrimSpace(item.Get("output").String())
				if out != "" {
					segments = append(segments, "FUNCTION_OUTPUT:\n"+out)
				}
			default:
				if txt := strings.TrimSpace(item.Get("text").String()); txt != "" {
					segments = append(segments, txt)
				} else if itemType != "" {
					unsupported = true
				}
			}
		}
	}

	return strings.TrimSpace(strings.Join(segments, "\n\n")), !unsupported
}

func codexCollectMessageContentText(content gjson.Result) ([]string, bool) {
	var parts []string
	if !content.Exists() {
		return parts, true
	}
	if content.IsArray() {
		unsupported := false
		for _, part := range content.Array() {
			partType := strings.TrimSpace(part.Get("type").String())
			switch partType {
			case "input_text", "output_text", "text":
				if txt := strings.TrimSpace(part.Get("text").String()); txt != "" {
					parts = append(parts, txt)
				}
			case "":
				if txt := strings.TrimSpace(part.String()); txt != "" {
					parts = append(parts, txt)
				}
			default:
				unsupported = true
			}
		}
		return parts, !unsupported
	}
	if txt := strings.TrimSpace(content.String()); txt != "" {
		return []string{txt}, true
	}
	return parts, true
}

func codexExtractCompletedMessageAndReasoning(payload []byte) (message string, reasoning string) {
	root := gjson.ParseBytes(payload)
	if root.Get("type").String() != "response.completed" {
		return "", ""
	}
	output := root.Get("response.output")
	if !output.Exists() || !output.IsArray() {
		return "", ""
	}
	var msgParts []string
	var reasoningParts []string
	for _, item := range output.Array() {
		switch item.Get("type").String() {
		case "message":
			content := item.Get("content")
			if content.IsArray() {
				for _, part := range content.Array() {
					if part.Get("type").String() == "output_text" {
						if txt := strings.TrimSpace(part.Get("text").String()); txt != "" {
							msgParts = append(msgParts, txt)
						}
					}
				}
			}
		case "reasoning":
			if summary := item.Get("summary"); summary.IsArray() {
				for _, part := range summary.Array() {
					if txt := strings.TrimSpace(part.Get("text").String()); txt != "" {
						reasoningParts = append(reasoningParts, txt)
					}
				}
			}
			if len(reasoningParts) == 0 {
				if txt := strings.TrimSpace(item.Get("content").String()); txt != "" {
					reasoningParts = append(reasoningParts, txt)
				}
			}
		}
	}
	return strings.TrimSpace(strings.Join(msgParts, "\n")), strings.TrimSpace(strings.Join(reasoningParts, "\n"))
}

func codexAgentTruncate(s string, max int) string {
	if max <= 0 {
		max = 12000
	}
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	const suffix = "\n\n[truncated by server-side agent pipeline]"
	if max <= len(suffix)+16 {
		return s[:max]
	}
	return s[:max-len(suffix)] + suffix
}

func codexBuildAgentPassBody(baseBody []byte, phase string, originalTask string, plannerOutput string, reviewerOutput string) ([]byte, error) {
	if len(baseBody) == 0 || !gjson.ValidBytes(baseBody) {
		return nil, fmt.Errorf("codex agent pipeline: invalid base request body")
	}
	out := bytes.Clone(baseBody)

	// Remove client/local controls and features incompatible with internal review passes.
	for _, path := range []string{
		"_cliproxy",
		"agent_mode",
		"tools",
		"tool_choice",
		"parallel_tool_calls",
		"text.format",
		"response_format",
		"previous_response_id",
		"prompt_cache_key",
	} {
		out, _ = sjson.DeleteBytes(out, path)
	}
	out, _ = sjson.SetBytes(out, "stream", true)

	instructions := codexAgentPhaseInstructions(phase)
	out, _ = sjson.SetBytes(out, "instructions", instructions)

	prompt := codexAgentPhasePrompt(phase, originalTask, plannerOutput, reviewerOutput)
	inputJSON := fmt.Sprintf(`[{"type":"message","role":"user","content":[{"type":"input_text","text":%q}]}]`, prompt)
	out, _ = sjson.SetRawBytes(out, "input", []byte(inputJSON))

	return out, nil
}

func codexAgentPhaseInstructions(phase string) string {
	var base string
	switch phase {
	case "planner":
		base = "You are an internal planning agent. Produce a strong plan and draft direction. Do not mention hidden reasoning. Be explicit and structured. Output planning artifacts only, not the final user answer."
	case "reviewer":
		base = "You are an internal reviewer agent. Critique the plan/draft rigorously, identify gaps and edge cases, and propose fixes. Do not produce the final user answer."
	case "final":
		base = "You are the final responder. Use the plan and review to produce the best final answer for the user. Do not mention the internal planner/reviewer workflow unless the user explicitly asks."
	default:
		base = "You are an internal agent pass."
	}

	sections := []string{base, codexDeepEngineeringStandardsPrompt()}
	if phase == "final" {
		sections = append(sections, codexNormalResponseFormatPrompt())
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func codexAgentPhasePrompt(phase string, originalTask string, plannerOutput string, reviewerOutput string) string {
	task := codexAgentTruncate(originalTask, 16000)
	planner := codexAgentTruncate(plannerOutput, 12000)
	reviewer := codexAgentTruncate(reviewerOutput, 12000)

	switch phase {
	case "planner":
		return strings.TrimSpace(`Original user request and context:
` + task + `

Produce:
1. A solution plan
2. Key technical risks and edge cases
3. A draft answer/implementation direction

Prefer depth over brevity, but keep it actionable.`)
	case "reviewer":
		return strings.TrimSpace(`Original user request and context:
` + task + `

Planner output:
` + planner + `

Review the planner output critically. Identify incorrect assumptions, missing edge cases, scalability/performance concerns, accessibility concerns (if UI), and maintainability risks.

Produce:
1. Findings (ordered by severity)
2. Suggested corrections
3. What the final answer must include`)
	case "final":
		return strings.TrimSpace(`Original user request and context:
` + task + `

Planner output:
` + planner + `

Reviewer output:
` + reviewer + `

Produce the final answer for the user. Incorporate valid reviewer feedback. Keep it technically rigorous and directly useful.`)
	default:
		return task
	}
}

func addUsageDetails(a usageDetail, b usageDetail) usageDetail {
	return usageDetail{
		InputTokens:     a.InputTokens + b.InputTokens,
		OutputTokens:    a.OutputTokens + b.OutputTokens,
		ReasoningTokens: a.ReasoningTokens + b.ReasoningTokens,
		CachedTokens:    a.CachedTokens + b.CachedTokens,
		TotalTokens:     a.TotalTokens + b.TotalTokens,
	}
}

func findCodexCompletedEventFromSSEData(data []byte) ([]byte, bool) {
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		if payload, ok := codexCompletedEventPayload(line); ok {
			return payload, true
		}
	}
	return nil, false
}

func (e *CodexExecutorRefactored) executeCodexCompletedHTTPPass(ctx context.Context, auth *cliproxyauth.Auth, apiKey string, url string, from sdktranslator.Format, req cliproxyexecutor.Request, body []byte, usePromptCache bool) (codexCompletedPassResult, error) {
	result := codexCompletedPassResult{Request: bytes.Clone(body)}

	var httpReq *http.Request
	var err error
	if usePromptCache {
		httpReq, err = e.cacheHelper(ctx, from, url, req, body)
	} else {
		httpReq, err = http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	}
	if err != nil {
		return result, err
	}

	provider := NewCodexProvider(e.cfg)
	provider.ApplyHeaders(httpReq, auth, apiKey, true)

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
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return result, err
	}
	defer httpResp.Body.Close()

	result.Headers = httpResp.Header.Clone()
	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, result.Headers.Clone())
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		appendAPIResponseChunk(ctx, e.cfg, b)
		logWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), b))
		return result, statusErr{code: httpResp.StatusCode, msg: string(b)}
	}

	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return result, err
	}
	appendAPIResponseChunk(ctx, e.cfg, data)

	completed, ok := findCodexCompletedEventFromSSEData(data)
	if !ok {
		return result, statusErr{code: 408, msg: "stream error: stream disconnected before completion: stream closed before response.completed"}
	}
	result.Completed = completed
	if detail, okUsage := parseCodexUsage(completed); okUsage {
		result.Usage = detail
		result.UsageOK = true
	}
	return result, nil
}

func (e *CodexExecutorRefactored) executeCodexPlannerReviewerPipeline(ctx context.Context, auth *cliproxyauth.Auth, apiKey string, baseURL string, from sdktranslator.Format, req cliproxyexecutor.Request, normalizedBody []byte) (codexCompletedPassResult, error) {
	if issue := codexAgentCompatibilityIssue(normalizedBody); issue != "" {
		return codexCompletedPassResult{}, statusErr{code: http.StatusBadRequest, msg: "codex agent_mode planner-reviewer unsupported: " + issue}
	}

	taskText, taskSupported := codexExtractTaskText(normalizedBody)
	if !taskSupported {
		return codexCompletedPassResult{}, statusErr{code: http.StatusBadRequest, msg: "codex agent_mode planner-reviewer unsupported: request contains unsupported non-text input parts"}
	}
	if strings.TrimSpace(taskText) == "" {
		return codexCompletedPassResult{}, statusErr{code: http.StatusBadRequest, msg: "codex agent_mode planner-reviewer unsupported: unable to extract textual task content"}
	}

	url := strings.TrimSuffix(baseURL, "/") + "/responses"

	plannerBody, err := codexBuildAgentPassBody(normalizedBody, "planner", taskText, "", "")
	if err != nil {
		return codexCompletedPassResult{}, err
	}
	plannerRes, err := e.executeCodexCompletedHTTPPass(ctx, auth, apiKey, url, from, req, plannerBody, false)
	if err != nil {
		return codexCompletedPassResult{}, err
	}
	plannerText, plannerReasoning := codexExtractCompletedMessageAndReasoning(plannerRes.Completed)
	if plannerReasoning != "" && plannerText != "" {
		plannerText = plannerText + "\n\nReviewer note from planner reasoning summary:\n" + plannerReasoning
	} else if plannerText == "" {
		plannerText = plannerReasoning
	}
	if strings.TrimSpace(plannerText) == "" {
		return codexCompletedPassResult{}, statusErr{code: http.StatusBadGateway, msg: "codex agent_mode planner-reviewer failed: planner pass returned no text"}
	}

	reviewerBody, err := codexBuildAgentPassBody(normalizedBody, "reviewer", taskText, plannerText, "")
	if err != nil {
		return codexCompletedPassResult{}, err
	}
	reviewerRes, err := e.executeCodexCompletedHTTPPass(ctx, auth, apiKey, url, from, req, reviewerBody, false)
	if err != nil {
		return codexCompletedPassResult{}, err
	}
	reviewerText, reviewerReasoning := codexExtractCompletedMessageAndReasoning(reviewerRes.Completed)
	if reviewerReasoning != "" && reviewerText != "" {
		reviewerText = reviewerText + "\n\nReviewer reasoning summary:\n" + reviewerReasoning
	} else if reviewerText == "" {
		reviewerText = reviewerReasoning
	}
	if strings.TrimSpace(reviewerText) == "" {
		return codexCompletedPassResult{}, statusErr{code: http.StatusBadGateway, msg: "codex agent_mode planner-reviewer failed: reviewer pass returned no text"}
	}

	finalBody, err := codexBuildAgentPassBody(normalizedBody, "final", taskText, plannerText, reviewerText)
	if err != nil {
		return codexCompletedPassResult{}, err
	}
	finalRes, err := e.executeCodexCompletedHTTPPass(ctx, auth, apiKey, url, from, req, finalBody, false)
	if err != nil {
		return codexCompletedPassResult{}, err
	}
	finalRes.Request = finalBody
	finalRes.Usage = addUsageDetails(finalRes.Usage, plannerRes.Usage)
	finalRes.Usage = addUsageDetails(finalRes.Usage, reviewerRes.Usage)
	finalRes.UsageOK = finalRes.UsageOK || plannerRes.UsageOK || reviewerRes.UsageOK
	return finalRes, nil
}
