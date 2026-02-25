package executor

import (
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestGetOrCreateCodexPromptCacheRedis_StableKey(t *testing.T) {
	resetCodexPromptCacheRedisForTest()
	defer resetCodexPromptCacheRedisForTest()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer mr.Close()

	cfg := &config.Config{}
	cfg.CodexPromptCache.RedisURL = "redis://" + mr.Addr() + "/0"
	cfg.CodexPromptCache.KeyPrefix = "cliproxy:test:"
	cfg.CodexPromptCache.TTLSeconds = 3600
	cfg.CodexPromptCache.TimeoutMS = 500

	id1, ok1 := getOrCreateCodexPromptCacheRedis(cfg, "gpt-5-user-1", 0)
	if !ok1 || id1 == "" {
		t.Fatalf("first redis cache id not created: ok=%v id=%q", ok1, id1)
	}
	id2, ok2 := getOrCreateCodexPromptCacheRedis(cfg, "gpt-5-user-1", 0)
	if !ok2 || id2 == "" {
		t.Fatalf("second redis cache id not returned: ok=%v id=%q", ok2, id2)
	}
	if id1 != id2 {
		t.Fatalf("redis cache id mismatch: %q vs %q", id1, id2)
	}
}

func TestGetOrCreateCodexPromptCacheRedis_FallbackOnInvalidURL(t *testing.T) {
	resetCodexPromptCacheRedisForTest()
	defer resetCodexPromptCacheRedisForTest()

	cfg := &config.Config{}
	cfg.CodexPromptCache.RedisURL = "::://invalid"
	cfg.CodexPromptCache.TimeoutMS = 100

	id, ok := getOrCreateCodexPromptCacheRedis(cfg, "gpt-5-user-1", 0)
	if ok {
		t.Fatalf("expected redis disabled/fallback on invalid URL, got ok=true id=%q", id)
	}
}

func TestGetOrCreateCodexPromptCacheRedis_UsesConfiguredTTL(t *testing.T) {
	resetCodexPromptCacheRedisForTest()
	defer resetCodexPromptCacheRedisForTest()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer mr.Close()

	cfg := &config.Config{}
	cfg.CodexPromptCache.RedisURL = "redis://" + mr.Addr() + "/0"
	cfg.CodexPromptCache.KeyPrefix = "cliproxy:test:"
	cfg.CodexPromptCache.TTLSeconds = 7
	cfg.CodexPromptCache.TimeoutMS = 500

	_, ok := getOrCreateCodexPromptCacheRedis(cfg, "gpt-5-user-ttl", 0)
	if !ok {
		t.Fatalf("expected redis set to succeed")
	}

	ttl := mr.TTL("cliproxy:test:gpt-5-user-ttl")
	if ttl <= 0 {
		t.Fatalf("expected positive ttl, got %v", ttl)
	}
	if ttl > 8*time.Second || ttl < 6*time.Second {
		t.Fatalf("expected ttl around 7s, got %v", ttl)
	}
}

func TestGetOrCreateCodexPromptCacheRedis_RequestTTLOverridesConfiguredTTL(t *testing.T) {
	resetCodexPromptCacheRedisForTest()
	defer resetCodexPromptCacheRedisForTest()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer mr.Close()

	cfg := &config.Config{}
	cfg.CodexPromptCache.RedisURL = "redis://" + mr.Addr() + "/0"
	cfg.CodexPromptCache.KeyPrefix = "cliproxy:test:"
	cfg.CodexPromptCache.TTLSeconds = 3600
	cfg.CodexPromptCache.TimeoutMS = 500

	_, ok := getOrCreateCodexPromptCacheRedis(cfg, "gpt-5-user-ttl-override", 75*time.Second)
	if !ok {
		t.Fatalf("expected redis set to succeed")
	}

	ttl := mr.TTL("cliproxy:test:gpt-5-user-ttl-override")
	if ttl <= 0 {
		t.Fatalf("expected positive ttl, got %v", ttl)
	}
	if ttl > 76*time.Second || ttl < 74*time.Second {
		t.Fatalf("expected ttl around 75s, got %v", ttl)
	}
}
