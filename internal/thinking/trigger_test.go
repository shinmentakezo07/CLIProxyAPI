package thinking

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/tidwall/gjson"
)

type testApplier struct {
	last ThinkingConfig
}

func (a *testApplier) Apply(body []byte, config ThinkingConfig, _ *registry.ModelInfo) ([]byte, error) {
	a.last = config
	return body, nil
}

func TestHasThinkTrigger(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "prompt standalone", body: `{"prompt":"please think step by step"}`, want: true},
		{name: "messages content string", body: `{"messages":[{"role":"user","content":"Think this through"}]}`, want: true},
		{name: "messages content array", body: `{"messages":[{"role":"user","content":[{"type":"text","text":"can you think?"}]}]}`, want: true},
		{name: "gemini parts", body: `{"contents":[{"parts":[{"text":"think about options"}]}]}`, want: true},
		{name: "word boundary no match", body: `{"prompt":"rethink and thinking"}`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasThinkTrigger([]byte(tt.body)); got != tt.want {
				t.Fatalf("hasThinkTrigger() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPickAutoTriggerConfig(t *testing.T) {
	t.Run("prefers dynamic auto", func(t *testing.T) {
		cfg, ok := pickAutoTriggerConfig(&registry.ModelInfo{Thinking: &registry.ThinkingSupport{DynamicAllowed: true, Max: 32000}})
		if !ok {
			t.Fatal("expected config")
		}
		if cfg.Mode != ModeAuto || cfg.Budget != -1 {
			t.Fatalf("unexpected config: %+v", cfg)
		}
	})

	t.Run("falls back to highest level", func(t *testing.T) {
		cfg, ok := pickAutoTriggerConfig(&registry.ModelInfo{Thinking: &registry.ThinkingSupport{Levels: []string{"low", "high"}}})
		if !ok {
			t.Fatal("expected config")
		}
		if cfg.Mode != ModeLevel || cfg.Level != LevelHigh {
			t.Fatalf("unexpected config: %+v", cfg)
		}
	})

	t.Run("falls back to max budget", func(t *testing.T) {
		cfg, ok := pickAutoTriggerConfig(&registry.ModelInfo{Thinking: &registry.ThinkingSupport{Min: 128, Max: 4096}})
		if !ok {
			t.Fatal("expected config")
		}
		if cfg.Mode != ModeBudget || cfg.Budget != 4096 {
			t.Fatalf("unexpected config: %+v", cfg)
		}
	})
}

func TestApplyAutoTriggerConfig_DoesNotOverrideExistingConfig(t *testing.T) {
	body := []byte(`{"prompt":"please think carefully"}`)
	existing := ThinkingConfig{Mode: ModeLevel, Level: LevelLow}
	got := applyAutoTriggerConfig(body, existing, false, &registry.ModelInfo{ID: "gpt-5", Thinking: &registry.ThinkingSupport{DynamicAllowed: true}}, "openai")
	if got != existing {
		t.Fatalf("config overridden unexpectedly: got %+v want %+v", got, existing)
	}
}

func TestApplyThinking_TriggerAppliesDefaultWhenNoExplicitConfig(t *testing.T) {
	provider := "openai"
	old := GetProviderApplier(provider)
	mock := &testApplier{}
	RegisterProvider(provider, mock)
	t.Cleanup(func() { RegisterProvider(provider, old) })

	body := []byte(`{"messages":[{"role":"user","content":"please think carefully"}]}`)
	result, err := ApplyThinking(body, "gpt-5", "openai", provider, provider)
	if err != nil {
		t.Fatalf("ApplyThinking() error = %v", err)
	}
	if !gjson.ValidBytes(result) {
		t.Fatalf("invalid result json: %s", string(result))
	}
	if mock.last.Mode != ModeLevel || mock.last.Level != LevelHigh {
		t.Fatalf("trigger config = %+v, want ModeLevel high", mock.last)
	}
}

func TestApplyThinking_TriggerDoesNotOverrideSuffixConfig(t *testing.T) {
	provider := "openai"
	old := GetProviderApplier(provider)
	mock := &testApplier{}
	RegisterProvider(provider, mock)
	t.Cleanup(func() { RegisterProvider(provider, old) })

	body := []byte(`{"messages":[{"role":"user","content":"please think carefully"}],"reasoning_effort":"low"}`)
	_, err := ApplyThinking(body, "gpt-5(high)", "openai", provider, provider)
	if err != nil {
		t.Fatalf("ApplyThinking() error = %v", err)
	}
	if mock.last.Mode != ModeLevel || mock.last.Level != LevelHigh {
		t.Fatalf("suffix was not preserved, got %+v", mock.last)
	}
}
