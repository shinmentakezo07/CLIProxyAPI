package thinking

import "testing"

func TestExtractThinkingConfig_AllProviders(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		body     string
		want     ThinkingConfig
	}{
		{
			name:     "claude disabled",
			provider: "claude",
			body:     `{"thinking":{"type":"disabled"}}`,
			want:     ThinkingConfig{Mode: ModeNone, Budget: 0},
		},
		{
			name:     "gemini level",
			provider: "gemini",
			body:     `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"high"}}}`,
			want:     ThinkingConfig{Mode: ModeLevel, Level: LevelHigh},
		},
		{
			name:     "gemini-cli budget snake_case",
			provider: "gemini-cli",
			body:     `{"request":{"generationConfig":{"thinkingConfig":{"thinking_budget":2048}}}}`,
			want:     ThinkingConfig{Mode: ModeBudget, Budget: 2048},
		},
		{
			name:     "antigravity none",
			provider: "antigravity",
			body:     `{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":"none"}}}}`,
			want:     ThinkingConfig{Mode: ModeNone, Budget: 0},
		},
		{
			name:     "openai effort",
			provider: "openai",
			body:     `{"reasoning_effort":"medium"}`,
			want:     ThinkingConfig{Mode: ModeLevel, Level: LevelMedium},
		},
		{
			name:     "codex effort",
			provider: "codex",
			body:     `{"reasoning":{"effort":"high"}}`,
			want:     ThinkingConfig{Mode: ModeLevel, Level: LevelHigh},
		},
		{
			name:     "kimi uses openai format",
			provider: "kimi",
			body:     `{"reasoning_effort":"low"}`,
			want:     ThinkingConfig{Mode: ModeLevel, Level: LevelLow},
		},
		{
			name:     "iflow native enable_thinking",
			provider: "iflow",
			body:     `{"chat_template_kwargs":{"enable_thinking":true}}`,
			want:     ThinkingConfig{Mode: ModeBudget, Budget: 1},
		},
		{
			name:     "iflow fallback to openai",
			provider: "iflow",
			body:     `{"reasoning_effort":"high"}`,
			want:     ThinkingConfig{Mode: ModeLevel, Level: LevelHigh},
		},
		{
			name:     "unknown provider",
			provider: "unknown",
			body:     `{"reasoning_effort":"high"}`,
			want:     ThinkingConfig{},
		},
		{
			name:     "invalid json",
			provider: "openai",
			body:     `{invalid`,
			want:     ThinkingConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractThinkingConfig([]byte(tt.body), tt.provider)
			if got != tt.want {
				t.Fatalf("extractThinkingConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
