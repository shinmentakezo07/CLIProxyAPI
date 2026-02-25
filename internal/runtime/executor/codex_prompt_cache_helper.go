package executor

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
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
func applyCodexPromptCache(from sdktranslator.Format, reqPayload []byte, model string, rawJSON []byte, skipIfBodyEmpty bool) ([]byte, string, http.Header) {
	headers := http.Header{}
	if skipIfBodyEmpty && len(rawJSON) == 0 {
		return rawJSON, "", headers
	}

	cacheID := resolveCodexPromptCacheID(from, reqPayload, model)
	if cacheID == "" {
		return rawJSON, "", headers
	}

	rawJSON, _ = sjson.SetBytes(rawJSON, "prompt_cache_key", cacheID)
	headers.Set("Conversation_id", cacheID)
	headers.Set("Session_id", cacheID)
	return rawJSON, cacheID, headers
}

func resolveCodexPromptCacheID(from sdktranslator.Format, reqPayload []byte, model string) string {
	var cache codexCache

	if from == sdktranslator.FromString("claude") {
		userIDResult := gjson.GetBytes(reqPayload, "metadata.user_id")
		if userIDResult.Exists() {
			key := fmt.Sprintf("%s-%s", model, userIDResult.String())
			var ok bool
			if cache, ok = getCodexCache(key); !ok {
				cache = codexCache{
					ID:     uuid.New().String(),
					Expire: time.Now().Add(1 * time.Hour),
				}
				setCodexCache(key, cache)
			}
		}
	} else if from == sdktranslator.FromString("openai-response") {
		promptCacheKey := gjson.GetBytes(reqPayload, "prompt_cache_key")
		if promptCacheKey.Exists() {
			cache.ID = promptCacheKey.String()
		}
	}

	return cache.ID
}
