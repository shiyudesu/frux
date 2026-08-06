package infracache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	applicationratelimit "github.com/shiyudesu/frux/internal/application/ratelimit"

	"github.com/redis/go-redis/v9"
)

var ErrInvalidRateLimitResponse = errors.New("invalid Redis rate-limit response")

const redisTokenBucketScript = `
local now = redis.call("TIME")
local now_ms = now[1] * 1000 + math.floor(now[2] / 1000)
local capacity = tonumber(ARGV[1])
local refill_per_ms = tonumber(ARGV[2])
local ttl_ms = tonumber(ARGV[3])
local values = redis.call("HMGET", KEYS[1], "tokens", "refilled_at")
local tokens = tonumber(values[1])
local refilled_at = tonumber(values[2])
if tokens == nil or refilled_at == nil then
  tokens = capacity
  refilled_at = now_ms
else
  local elapsed = math.max(0, now_ms - refilled_at)
  tokens = math.min(capacity, tokens + elapsed * refill_per_ms)
  refilled_at = now_ms
end
local allowed = 0
local retry_ms = 0
if tokens >= 1 then
  allowed = 1
  tokens = tokens - 1
else
  retry_ms = math.ceil((1 - tokens) / refill_per_ms)
end
redis.call("HSET", KEYS[1], "tokens", tokens, "refilled_at", refilled_at)
redis.call("PEXPIRE", KEYS[1], ttl_ms)
return {allowed, retry_ms, math.floor(tokens)}
`

type RedisEvaler interface {
	Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd
}

type RedisRateLimiter struct {
	client RedisEvaler
	prefix string
}

func NewRedisRateLimiter(client RedisEvaler) *RedisRateLimiter {
	return &RedisRateLimiter{client: client, prefix: "frux:rate-limit:"}
}

func (r *RedisRateLimiter) Allow(
	ctx context.Context,
	policy applicationratelimit.PolicyName,
	identity string,
	quota applicationratelimit.Quota,
	idleTTL time.Duration,
) (applicationratelimit.DistributedDecision, error) {
	if r == nil || r.client == nil {
		return applicationratelimit.DistributedDecision{}, applicationratelimit.ErrDistributedUnavailable
	}
	digest := sha256.Sum256([]byte(identity))
	key := r.prefix + string(policy) + ":" + hex.EncodeToString(digest[:])
	result, err := r.client.Eval(
		ctx,
		redisTokenBucketScript,
		[]string{key},
		quota.Capacity,
		strconv.FormatFloat(quota.RefillPerSecond/1000, 'g', -1, 64),
		idleTTL.Milliseconds(),
	).Result()
	if err != nil {
		return applicationratelimit.DistributedDecision{}, err
	}
	values, ok := result.([]any)
	if !ok || len(values) != 3 {
		return applicationratelimit.DistributedDecision{}, ErrInvalidRateLimitResponse
	}
	allowed, err := redisInt64(values[0])
	if err != nil {
		return applicationratelimit.DistributedDecision{}, err
	}
	retryMS, err := redisInt64(values[1])
	if err != nil {
		return applicationratelimit.DistributedDecision{}, err
	}
	remaining, err := redisInt64(values[2])
	if err != nil {
		return applicationratelimit.DistributedDecision{}, err
	}
	return applicationratelimit.DistributedDecision{
		Allowed:    allowed == 1,
		RetryAfter: time.Duration(maxInt64(0, retryMS)) * time.Millisecond,
		Remaining:  int(maxInt64(0, remaining)),
	}, nil
}

func redisInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0, ErrInvalidRateLimitResponse
		}
		return int64(math.Floor(parsed)), nil
	case []byte:
		return redisInt64(string(typed))
	default:
		return 0, fmt.Errorf("%w: %T", ErrInvalidRateLimitResponse, value)
	}
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
