package executor

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// applyCodexPromptCache centralizes Codex prompt cache behavior used by HTTP and WebSocket paths.
// It resolves cache IDs from source-format-specific fields, applies prompt_cache_key to payload,
// and returns the matching Conversation_id/Session_id headers.
//
// When skipIfBodyEmpty is true and rawJSON is empty, cache logic is bypassed. This preserves
// existing websocket behavior.
func applyCodexPromptCache(cfg *config.Config, from sdktranslator.Format, reqPayload []byte, model string, rawJSON []byte, skipIfBodyEmpty bool) ([]byte, string, http.Header) {
	headers := http.Header{}
	if skipIfBodyEmpty && len(rawJSON) == 0 {
		return rawJSON, "", headers
	}

	cacheID := resolveCodexPromptCacheID(cfg, from, reqPayload, model)
	if cacheID == "" {
		return rawJSON, "", headers
	}

	rawJSON, _ = sjson.SetBytes(rawJSON, "prompt_cache_key", cacheID)
	headers.Set("Conversation_id", cacheID)
	headers.Set("Session_id", cacheID)
	return rawJSON, cacheID, headers
}

func resolveCodexPromptCacheID(cfg *config.Config, from sdktranslator.Format, reqPayload []byte, model string) string {
	var cache codexCache
	if explicit := explicitCodexPromptCacheKey(reqPayload); explicit != "" {
		cache.ID = explicit
		return cache.ID
	}
	if cacheDisabledByRetention(reqPayload) {
		return ""
	}

	if from == sdktranslator.FromString("claude") {
		if userID := codexPromptCacheUserID(reqPayload); userID != "" {
			return getOrCreateCodexPromptCacheID(cfg, model, userID, codexPromptCacheEffectiveTTL(cfg, reqPayload))
		}
		return ""
	}

	// For non-Claude inputs, require an explicit retention hint before auto-deriving a cache key.
	if !cacheRetentionRequested(reqPayload) {
		return ""
	}
	if userID := codexPromptCacheUserID(reqPayload); userID != "" {
		return getOrCreateCodexPromptCacheID(cfg, model, userID, codexPromptCacheEffectiveTTL(cfg, reqPayload))
	}

	return cache.ID
}

func explicitCodexPromptCacheKey(reqPayload []byte) string {
	if len(reqPayload) == 0 {
		return ""
	}
	if key := strings.TrimSpace(gjson.GetBytes(reqPayload, "prompt_cache_key").String()); key != "" {
		return key
	}
	if key := strings.TrimSpace(gjson.GetBytes(reqPayload, "metadata.prompt_cache_key").String()); key != "" {
		return key
	}
	return ""
}

func codexPromptCacheUserID(reqPayload []byte) string {
	if len(reqPayload) == 0 {
		return ""
	}
	paths := []string{
		"metadata.user_id",
		"user",
		"safety_identifier",
		"metadata.safety_identifier",
	}
	for _, path := range paths {
		if v := strings.TrimSpace(gjson.GetBytes(reqPayload, path).String()); v != "" {
			return v
		}
	}
	return ""
}

func getOrCreateCodexPromptCacheID(cfg *config.Config, model, userID string, ttl time.Duration) string {
	model = strings.TrimSpace(model)
	userID = strings.TrimSpace(userID)
	if model == "" || userID == "" {
		return ""
	}
	key := fmt.Sprintf("%s-%s", model, userID)
	if distributedID, ok := getOrCreateCodexPromptCacheRedis(cfg, key, ttl); ok {
		return distributedID
	}
	cache, ok := getCodexCache(key)
	if ok {
		return cache.ID
	}
	cache = codexCache{
		ID:     uuid.New().String(),
		Expire: time.Now().Add(clampCodexPromptCacheTTL(ttl)),
	}
	setCodexCache(key, cache)
	return cache.ID
}

func cacheDisabledByRetention(reqPayload []byte) bool {
	if len(reqPayload) == 0 {
		return false
	}
	ret := gjson.GetBytes(reqPayload, "prompt_cache_retention")
	if !ret.Exists() {
		return false
	}
	switch ret.Type {
	case gjson.False:
		return true
	case gjson.Number:
		return ret.Float() <= 0
	case gjson.String:
		s := strings.ToLower(strings.TrimSpace(ret.String()))
		switch s {
		case "", "default", "auto":
			return false
		case "0", "off", "none", "false", "disabled", "disable", "no":
			return true
		}
	}
	return false
}

func cacheRetentionRequested(reqPayload []byte) bool {
	if len(reqPayload) == 0 {
		return false
	}
	ret := gjson.GetBytes(reqPayload, "prompt_cache_retention")
	return ret.Exists() && !cacheDisabledByRetention(reqPayload)
}

func codexPromptCacheRetentionTTL(reqPayload []byte) time.Duration {
	if len(reqPayload) == 0 {
		return time.Hour
	}
	ret := gjson.GetBytes(reqPayload, "prompt_cache_retention")
	if !ret.Exists() {
		return time.Hour
	}
	switch ret.Type {
	case gjson.True:
		return time.Hour
	case gjson.Number:
		if ret.Float() > 0 {
			return time.Duration(ret.Int()) * time.Second
		}
		return time.Hour
	case gjson.String:
		s := strings.TrimSpace(ret.String())
		if s == "" {
			return time.Hour
		}
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			return d
		}
		// Unknown string values fall back to the default retention.
		return time.Hour
	default:
		return time.Hour
	}
}

func codexPromptCacheEffectiveTTL(cfg *config.Config, reqPayload []byte) time.Duration {
	if len(reqPayload) > 0 {
		ret := gjson.GetBytes(reqPayload, "prompt_cache_retention")
		if ret.Exists() {
			if cacheDisabledByRetention(reqPayload) {
				return 0
			}
			ttl := codexPromptCacheRetentionTTL(reqPayload)
			if ttl > 0 {
				return clampCodexPromptCacheTTL(ttl)
			}
		}
	}
	if cfg != nil && cfg.CodexPromptCache.TTLSeconds > 0 {
		return clampCodexPromptCacheTTL(time.Duration(cfg.CodexPromptCache.TTLSeconds) * time.Second)
	}
	return clampCodexPromptCacheTTL(time.Duration(config.DefaultCodexPromptCacheTTLSeconds) * time.Second)
}

func clampCodexPromptCacheTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return time.Hour
	}
	const maxTTL = 7 * 24 * time.Hour
	if ttl > maxTTL {
		return maxTTL
	}
	return ttl
}
