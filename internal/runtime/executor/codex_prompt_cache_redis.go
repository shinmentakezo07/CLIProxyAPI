package executor

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	log "github.com/sirupsen/logrus"
)

type codexPromptCacheRedisState struct {
	client  *redis.Client
	url     string
	timeout time.Duration
}

var (
	codexPromptCacheRedisMu    sync.Mutex
	codexPromptCacheRedis      codexPromptCacheRedisState
	codexPromptCacheRedisWarns sync.Map
)

func redisConfigFrom(cfg *config.Config) (url string, keyPrefix string, ttl time.Duration, timeout time.Duration) {
	url = ""
	keyPrefix = config.DefaultCodexPromptCacheKeyPrefix
	ttl = time.Duration(config.DefaultCodexPromptCacheTTLSeconds) * time.Second
	timeout = time.Duration(config.DefaultCodexPromptCacheTimeoutMS) * time.Millisecond
	if cfg == nil {
		return
	}
	url = strings.TrimSpace(cfg.CodexPromptCache.RedisURL)
	if v := strings.TrimSpace(cfg.CodexPromptCache.KeyPrefix); v != "" {
		keyPrefix = v
	}
	if cfg.CodexPromptCache.TTLSeconds > 0 {
		ttl = time.Duration(cfg.CodexPromptCache.TTLSeconds) * time.Second
	}
	if cfg.CodexPromptCache.TimeoutMS > 0 {
		timeout = time.Duration(cfg.CodexPromptCache.TimeoutMS) * time.Millisecond
	}
	return
}

func getOrCreateCodexPromptCacheRedis(cfg *config.Config, key string, requestTTL time.Duration) (string, bool) {
	redisURL, keyPrefix, ttl, timeout := redisConfigFrom(cfg)
	if requestTTL > 0 {
		ttl = clampCodexPromptCacheTTL(requestTTL)
	}
	if ttl <= 0 {
		ttl = clampCodexPromptCacheTTL(time.Duration(config.DefaultCodexPromptCacheTTLSeconds) * time.Second)
	}
	if redisURL == "" {
		return "", false
	}
	if timeout <= 0 {
		timeout = time.Duration(config.DefaultCodexPromptCacheTimeoutMS) * time.Millisecond
	}
	client, err := codexPromptCacheRedisClient(redisURL, timeout)
	if err != nil {
		warnCodexPromptCacheRedis("redis_init", err)
		return "", false
	}

	redisKey := keyPrefix + key
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if existing, errGet := client.Get(ctx, redisKey).Result(); errGet == nil {
		trimmed := strings.TrimSpace(existing)
		if trimmed != "" {
			return trimmed, true
		}
	} else if errGet != redis.Nil {
		warnCodexPromptCacheRedis("redis_get", errGet)
		return "", false
	}

	candidate := uuid.NewString()
	setOK, errSet := client.SetNX(ctx, redisKey, candidate, ttl).Result()
	if errSet != nil {
		warnCodexPromptCacheRedis("redis_setnx", errSet)
		return "", false
	}
	if setOK {
		return candidate, true
	}

	winner, errWinner := client.Get(ctx, redisKey).Result()
	if errWinner != nil {
		warnCodexPromptCacheRedis("redis_get_after_setnx", errWinner)
		return "", false
	}
	winner = strings.TrimSpace(winner)
	if winner == "" {
		return "", false
	}
	return winner, true
}

func codexPromptCacheRedisClient(redisURL string, timeout time.Duration) (*redis.Client, error) {
	codexPromptCacheRedisMu.Lock()
	defer codexPromptCacheRedisMu.Unlock()

	if codexPromptCacheRedis.client != nil && codexPromptCacheRedis.url == redisURL && codexPromptCacheRedis.timeout == timeout {
		return codexPromptCacheRedis.client, nil
	}
	if codexPromptCacheRedis.client != nil {
		_ = codexPromptCacheRedis.client.Close()
		codexPromptCacheRedis.client = nil
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	opts.DialTimeout = timeout
	opts.ReadTimeout = timeout
	opts.WriteTimeout = timeout

	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if errPing := client.Ping(ctx).Err(); errPing != nil {
		_ = client.Close()
		return nil, errPing
	}

	codexPromptCacheRedis.client = client
	codexPromptCacheRedis.url = redisURL
	codexPromptCacheRedis.timeout = timeout
	codexPromptCacheRedisWarns = sync.Map{}
	return client, nil
}

func warnCodexPromptCacheRedis(kind string, err error) {
	if err == nil {
		return
	}
	if _, loaded := codexPromptCacheRedisWarns.LoadOrStore(kind+":"+err.Error(), struct{}{}); loaded {
		return
	}
	log.WithError(err).Warn("codex prompt cache redis unavailable, falling back to local cache")
}

func resetCodexPromptCacheRedisForTest() {
	codexPromptCacheRedisMu.Lock()
	defer codexPromptCacheRedisMu.Unlock()
	if codexPromptCacheRedis.client != nil {
		_ = codexPromptCacheRedis.client.Close()
	}
	codexPromptCacheRedis = codexPromptCacheRedisState{}
	codexPromptCacheRedisWarns = sync.Map{}
}
