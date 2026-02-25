package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTempConfigYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadConfigOptional_CodexPromptCacheDefaults(t *testing.T) {
	cfgPath := writeTempConfigYAML(t, "port: 8317\n")
	cfg, err := LoadConfigOptional(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional error: %v", err)
	}
	if cfg.CodexPromptCache.KeyPrefix != DefaultCodexPromptCacheKeyPrefix {
		t.Fatalf("key-prefix = %q, want %q", cfg.CodexPromptCache.KeyPrefix, DefaultCodexPromptCacheKeyPrefix)
	}
	if cfg.CodexPromptCache.TTLSeconds != DefaultCodexPromptCacheTTLSeconds {
		t.Fatalf("ttl-seconds = %d, want %d", cfg.CodexPromptCache.TTLSeconds, DefaultCodexPromptCacheTTLSeconds)
	}
	if cfg.CodexPromptCache.TimeoutMS != DefaultCodexPromptCacheTimeoutMS {
		t.Fatalf("timeout-ms = %d, want %d", cfg.CodexPromptCache.TimeoutMS, DefaultCodexPromptCacheTimeoutMS)
	}
}

func TestLoadConfigOptional_CodexPromptCacheSanitizesInvalidValues(t *testing.T) {
	cfgPath := writeTempConfigYAML(t, `port: 8317
codex-prompt-cache:
  redis-url: " redis://localhost:6379/0 "
  key-prefix: ""
  ttl-seconds: 0
  timeout-ms: -1
`)
	cfg, err := LoadConfigOptional(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional error: %v", err)
	}
	if cfg.CodexPromptCache.RedisURL != "redis://localhost:6379/0" {
		t.Fatalf("redis-url = %q, want trimmed value", cfg.CodexPromptCache.RedisURL)
	}
	if cfg.CodexPromptCache.KeyPrefix != DefaultCodexPromptCacheKeyPrefix {
		t.Fatalf("key-prefix = %q, want default %q", cfg.CodexPromptCache.KeyPrefix, DefaultCodexPromptCacheKeyPrefix)
	}
	if cfg.CodexPromptCache.TTLSeconds != DefaultCodexPromptCacheTTLSeconds {
		t.Fatalf("ttl-seconds = %d, want default %d", cfg.CodexPromptCache.TTLSeconds, DefaultCodexPromptCacheTTLSeconds)
	}
	if cfg.CodexPromptCache.TimeoutMS != DefaultCodexPromptCacheTimeoutMS {
		t.Fatalf("timeout-ms = %d, want default %d", cfg.CodexPromptCache.TimeoutMS, DefaultCodexPromptCacheTimeoutMS)
	}
}

func TestLoadConfigOptional_CodexPromptCacheKeepsConfiguredTTL(t *testing.T) {
	cfgPath := writeTempConfigYAML(t, `port: 8317
codex-prompt-cache:
  ttl-seconds: 7
  timeout-ms: 200
`)
	cfg, err := LoadConfigOptional(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional error: %v", err)
	}
	if cfg.CodexPromptCache.TTLSeconds != 7 {
		t.Fatalf("ttl-seconds = %d, want 7", cfg.CodexPromptCache.TTLSeconds)
	}
	if cfg.CodexPromptCache.TimeoutMS != 200 {
		t.Fatalf("timeout-ms = %d, want 200", cfg.CodexPromptCache.TimeoutMS)
	}

	gotTTL := time.Duration(cfg.CodexPromptCache.TTLSeconds) * time.Second
	if gotTTL != 7*time.Second {
		t.Fatalf("effective config ttl = %v, want %v", gotTTL, 7*time.Second)
	}
}
