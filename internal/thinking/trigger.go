package thinking

import (
	"regexp"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

var thinkTriggerRegex = regexp.MustCompile(`(?i)(^|[^a-z0-9_])think([^a-z0-9_]|$)`)

var thinkTriggerPromptPaths = []string{
	"prompt",
	"input",
	"instructions",
	"system",
	"text",
	"messages.#.content",
	"messages.#.content.#.text",
	"contents.#.parts.#.text",
	"request.contents.#.parts.#.text",
}

// hasThinkTrigger reports whether prompt text contains a standalone "think" token.
func hasThinkTrigger(body []byte) bool {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}

	for _, path := range thinkTriggerPromptPaths {
		results := gjson.GetBytes(body, path)
		if resultContainsThink(results) {
			return true
		}
	}
	return false
}

func resultContainsThink(result gjson.Result) bool {
	if !result.Exists() {
		return false
	}

	switch {
	case result.Type == gjson.String:
		return containsStandaloneThink(result.String())
	case result.IsArray():
		found := false
		result.ForEach(func(_, value gjson.Result) bool {
			if resultContainsThink(value) {
				found = true
				return false
			}
			return true
		})
		return found
	case result.IsObject():
		if text := result.Get("text"); text.Exists() && text.Type == gjson.String {
			return containsStandaloneThink(text.String())
		}
	}

	return false
}

func containsStandaloneThink(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	return thinkTriggerRegex.MatchString(value)
}

// pickAutoTriggerConfig selects a provider-safe default thinking config.
// Priority: dynamic auto first, otherwise highest supported level, otherwise max budget.
func pickAutoTriggerConfig(modelInfo *registry.ModelInfo) (ThinkingConfig, bool) {
	if modelInfo == nil || modelInfo.Thinking == nil {
		return ThinkingConfig{}, false
	}

	support := modelInfo.Thinking
	if support.DynamicAllowed {
		return ThinkingConfig{Mode: ModeAuto, Budget: -1}, true
	}

	if level, ok := highestSupportedLevel(support.Levels); ok {
		return ThinkingConfig{Mode: ModeLevel, Level: level}, true
	}

	if support.Max > 0 {
		return ThinkingConfig{Mode: ModeBudget, Budget: support.Max}, true
	}

	return ThinkingConfig{}, false
}

func highestSupportedLevel(levels []string) (ThinkingLevel, bool) {
	for i := len(standardLevelOrder) - 1; i >= 0; i-- {
		candidate := string(standardLevelOrder[i])
		for _, level := range levels {
			if strings.EqualFold(strings.TrimSpace(level), candidate) {
				return ThinkingLevel(candidate), true
			}
		}
	}
	return "", false
}

// applyAutoTriggerConfig applies a prompt-triggered default thinking config when
// no explicit config is present and there is no suffix.
func applyAutoTriggerConfig(body []byte, config ThinkingConfig, hasSuffix bool, modelInfo *registry.ModelInfo, provider string) ThinkingConfig {
	if hasSuffix || hasThinkingConfig(config) || !hasThinkTrigger(body) {
		return config
	}

	autoConfig, ok := pickAutoTriggerConfig(modelInfo)
	if !ok {
		return config
	}

	modelID := ""
	if modelInfo != nil {
		modelID = modelInfo.ID
	}

	log.WithFields(log.Fields{
		"provider": provider,
		"model":    modelID,
		"mode":     autoConfig.Mode,
		"budget":   autoConfig.Budget,
		"level":    autoConfig.Level,
	}).Debug("thinking: auto-triggered config from prompt |")

	return autoConfig
}
