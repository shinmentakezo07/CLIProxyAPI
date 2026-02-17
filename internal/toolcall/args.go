package toolcall

import (
	"encoding/json"
	"fmt"
	"strings"
)

func normalizeJSON(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// NormalizeArgsJSON ensures OpenAI function.arguments is always a valid JSON object string.
func NormalizeArgsJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}"
	}
	if normalized, ok := normalizeJSON(raw); ok {
		if strings.HasPrefix(normalized, "{") {
			return normalized
		}
		return fmt.Sprintf(`{"value":%s}`, normalized)
	}
	enc, _ := json.Marshal(raw)
	return fmt.Sprintf(`{"value":%s}`, string(enc))
}

// NormalizeArgumentsObjectString is kept as backward-compatible alias.
func NormalizeArgumentsObjectString(raw string) string {
	return NormalizeArgsJSON(raw)
}
