package toolcall

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
)

var ambiguousRepairCount uint64

func AmbiguousRepairCount() uint64 {
	return atomic.LoadUint64(&ambiguousRepairCount)
}

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

// NormalizeArgumentsObjectString ensures OpenAI function.arguments is always a valid JSON object string.
func NormalizeArgumentsObjectString(raw string) string {
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

func FallbackID(provider string, seq int) string {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		provider = "generic"
	}
	if seq < 1 {
		seq = 1
	}
	return fmt.Sprintf("tc_%s_%d", provider, seq)
}

func ShortenName(name string, max int) string {
	name = strings.TrimSpace(name)
	if max <= 0 || len(name) <= max {
		return name
	}
	return name[:max]
}

func NormalizeToolChoiceFromGeminiMode(mode string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "NONE":
		return "none", true
	case "AUTO":
		return "auto", true
	case "ANY":
		return "required", true
	default:
		return "", false
	}
}

func PickToolSchema(parametersRaw, parametersJSONSchemaRaw string) string {
	if strings.TrimSpace(parametersRaw) != "" {
		return parametersRaw
	}
	return parametersJSONSchemaRaw
}

type pendingCall struct {
	id   string
	name string
}

type Tracker struct {
	provider string
	seq      int
	pending  []pendingCall
}

func NewTracker(provider string) *Tracker {
	return &Tracker{provider: provider}
}

func (t *Tracker) RegisterCall(name, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		t.seq++
		id = FallbackID(t.provider, t.seq)
	}
	t.pending = append(t.pending, pendingCall{id: id, name: strings.TrimSpace(name)})
	return id
}

// ResolveResultID resolves a tool result id with repair behavior.
// Returns resolvedID, repaired, ambiguous, ok.
func (t *Tracker) ResolveResultID(name, providedID string) (string, bool, bool, bool) {
	providedID = strings.TrimSpace(providedID)
	name = strings.TrimSpace(name)
	if providedID != "" {
		for i, p := range t.pending {
			if p.id == providedID {
				t.pending = append(t.pending[:i], t.pending[i+1:]...)
				return providedID, false, false, true
			}
		}
		return "", false, false, false
	}
	if len(t.pending) == 1 {
		id := t.pending[0].id
		t.pending = t.pending[:0]
		return id, true, false, true
	}
	if name != "" {
		idx := -1
		for i, p := range t.pending {
			if p.name == name {
				if idx != -1 {
					atomic.AddUint64(&ambiguousRepairCount, 1)
					return "", false, true, false
				}
				idx = i
			}
		}
		if idx != -1 {
			id := t.pending[idx].id
			t.pending = append(t.pending[:idx], t.pending[idx+1:]...)
			return id, true, false, true
		}
	}
	if len(t.pending) > 1 {
		atomic.AddUint64(&ambiguousRepairCount, 1)
		return "", false, true, false
	}
	return "", false, false, false
}
