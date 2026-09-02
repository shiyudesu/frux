package infraconfig

import (
	"path/filepath"
	"testing"
)

func TestBenchmarkConfigIsIsolatedAndValid(t *testing.T) {
	t.Setenv("FRUX_JWT_CONSUMER_SECRET", "benchmark-consumer-secret-value-123456")
	t.Setenv("FRUX_JWT_ADMIN_SECRET", "benchmark-admin-secret-value-123456789")
	t.Setenv("FRUX_HMAC_SECRET", "benchmark-hmac-secret-value-1234567890")
	t.Setenv("FRUX_INTERNAL_TOKEN", "benchmark-internal-token-value-123456")
	t.Setenv("FRUX_POSTGRES_USER", "frux_bench")
	t.Setenv("FRUX_POSTGRES_PASSWORD", "benchmark-postgres-password")
	t.Setenv("FRUX_POSTGRES_DATABASE", "frux_benchmark")
	t.Setenv("FRUX_REDIS_PASSWORD", "benchmark-redis-password")
	t.Setenv("FRUX_FEED_CACHE_MODE", "batch")
	t.Setenv("FRUX_MULTIMODAL_ENABLED", "false")
	t.Setenv("FRUX_MULTIMODAL_VIDEO_JOBS_ENABLED", "false")
	t.Setenv("FRUX_MULTIMODAL_QUERY_EMBEDDING_ENABLED", "false")
	t.Setenv("FRUX_MULTIMODAL_HYBRID_SEARCH_ENABLED", "false")
	t.Setenv("FRUX_MULTIMODAL_SIMILAR_VIDEOS_ENABLED", "false")
	t.Setenv("FRUX_MULTIMODAL_SESSION_RECOMMENDATION_ENABLED", "false")
	t.Setenv("FRUX_MULTIMODAL_SESSION_DEVELOPMENT_FULL_ROLLOUT_ENABLED", "false")
	t.Setenv("FRUX_MULTIMODAL_SESSION_PRODUCTION_FULL_ROLLOUT_ENABLED", "false")
	t.Setenv("FRUX_MULTIMODAL_ALLOW_INSECURE_PRIVATE_NETWORK", "false")

	path := filepath.Join("..", "..", "..", "configs", "config.benchmark.yaml")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load benchmark config: %v", err)
	}
	if cfg.Environment != "test" || cfg.Media.Backend != "local" ||
		cfg.Database.Name != "frux_benchmark" || cfg.Redis.Addr != "redis:6379" || cfg.Redis.FeedCacheMode != "batch" ||
		cfg.Kafka.TopicPrefix != "benchmark" {
		t.Fatalf("unexpected benchmark isolation config: %+v", cfg)
	}
	if cfg.Multimodal.Enabled || cfg.Multimodal.VideoJobsEnabled ||
		cfg.Multimodal.QueryEmbeddingEnabled || cfg.Multimodal.HybridSearchEnabled ||
		cfg.Multimodal.SimilarVideosEnabled || cfg.Multimodal.SessionRecommendationEnabled {
		t.Fatalf("benchmark config enables external multimodal work: %+v", cfg.Multimodal)
	}
}
