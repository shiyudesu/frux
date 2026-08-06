package infracache

import (
	"testing"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

func TestRateLimitRedisClientHonorsContextsAndDisablesCommandRetries(t *testing.T) {
	client := NewRateLimitRedisClient(infraconfig.RedisConfig{
		Addr: "127.0.0.1:6379",
		DB:   2,
	})
	t.Cleanup(func() { _ = client.Close() })

	options := client.Options()
	if !options.ContextTimeoutEnabled {
		t.Fatal("rate-limit client must honor policy context deadlines")
	}
	if options.MaxRetries != 0 {
		t.Fatalf("rate-limit client command retries=%d, want 0", options.MaxRetries)
	}
	if options.DB != 2 {
		t.Fatalf("rate-limit client DB=%d, want 2", options.DB)
	}
}
