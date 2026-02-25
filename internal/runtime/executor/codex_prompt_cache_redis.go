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

type codexPromptCacheRedisValueState uint8

const (
	codexPromptCacheRedisValueMissing codexPromptCacheRedisValueState = iota
	codexPromptCacheRedisValueValid
	codexPromptCacheRedisValueInvalid
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

	// Retry once if we encounter a malformed value (blank/whitespace) and repair it.
	for attempt := 0; attempt < 2; attempt++ {
		existing, state, errGet := codexPromptCacheRedisGetValue(client, redisKey, timeout)
		if errGet != nil {
			warnCodexPromptCacheRedis("redis_get", errGet)
			return "", false
		}
		switch state {
		case codexPromptCacheRedisValueValid:
			return existing, true
		case codexPromptCacheRedisValueInvalid:
			warnCodexPromptCacheRedisInvalidValue("redis_get_invalid")
			if errDel := codexPromptCacheRedisDeleteKey(client, redisKey, timeout); errDel != nil {
				warnCodexPromptCacheRedis("redis_del_invalid", errDel)
				return "", false
			}
			continue
		}

		candidate := uuid.NewString()
		setOK, errSet := codexPromptCacheRedisSetNX(client, redisKey, candidate, ttl, timeout)
		if errSet != nil {
			warnCodexPromptCacheRedis("redis_setnx", errSet)
			return "", false
		}
		if setOK {
			return candidate, true
		}

		winner, winnerState, errWinner := codexPromptCacheRedisGetValue(client, redisKey, timeout)
		if errWinner != nil {
			warnCodexPromptCacheRedis("redis_get_after_setnx", errWinner)
			return "", false
		}
		switch winnerState {
		case codexPromptCacheRedisValueValid:
			return winner, true
		case codexPromptCacheRedisValueInvalid:
			warnCodexPromptCacheRedisInvalidValue("redis_get_after_setnx_invalid")
			if attempt == 0 {
				if errDel := codexPromptCacheRedisDeleteKey(client, redisKey, timeout); errDel != nil {
					warnCodexPromptCacheRedis("redis_del_invalid_after_setnx", errDel)
					return "", false
				}
				continue
			}
		case codexPromptCacheRedisValueMissing:
			// Another actor may have deleted the key between SetNX(false) and Get; retry once.
			if attempt == 0 {
				continue
			}
		}
		return "", false
	}

	return "", false
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

func codexPromptCacheRedisGetValue(client *redis.Client, key string, timeout time.Duration) (string, codexPromptCacheRedisValueState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	value, err := client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", codexPromptCacheRedisValueMissing, nil
	}
	if err != nil {
		return "", codexPromptCacheRedisValueMissing, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", codexPromptCacheRedisValueInvalid, nil
	}
	return value, codexPromptCacheRedisValueValid, nil
}

func codexPromptCacheRedisSetNX(client *redis.Client, key, value string, ttl, timeout time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return client.SetNX(ctx, key, value, ttl).Result()
}

func codexPromptCacheRedisDeleteKey(client *redis.Client, key string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return client.Del(ctx, key).Err()
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

func warnCodexPromptCacheRedisInvalidValue(kind string) {
	if kind == "" {
		kind = "redis_invalid_value"
	}
	if _, loaded := codexPromptCacheRedisWarns.LoadOrStore("invalid:"+kind, struct{}{}); loaded {
		return
	}
	log.Warn("codex prompt cache redis contains blank cache id, attempting key repair")
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
