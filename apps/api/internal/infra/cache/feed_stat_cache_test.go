package infracache

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
)

// Hold the first JSON publication after its payload has been computed. Both
// plain SET (the previous implementation) and transactional SET are intercepted,
// so the same schedule exposes the original stale-writer bug without sleeps.
type pauseStatPublicationHook struct {
	key      string
	ready    chan struct{}
	release  chan struct{}
	once     sync.Once
	attempts atomic.Int32
}

func (h *pauseStatPublicationHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (h *pauseStatPublicationHook) before(ctx context.Context, cmds []redis.Cmder) error {
	for _, cmd := range cmds {
		args := cmd.Args()
		if cmd.Name() != "set" || len(args) < 2 || args[1] != h.key {
			continue
		}
		h.attempts.Add(1)
		var err error
		h.once.Do(func() {
			close(h.ready)
			select {
			case <-h.release:
			case <-ctx.Done():
				err = ctx.Err()
			}
		})
		return err
	}
	return nil
}

func (h *pauseStatPublicationHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err := h.before(ctx, []redis.Cmder{cmd}); err != nil {
			return err
		}
		return next(ctx, cmd)
	}
}

func (h *pauseStatPublicationHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if err := h.before(ctx, cmds); err != nil {
			return err
		}
		return next(ctx, cmds)
	}
}

type failingStatWatchClient struct {
	redisStatCacheClient
	err   error
	calls *atomic.Int32
}

func (c failingStatWatchClient) Watch(context.Context, func(*redis.Tx) error, ...string) error {
	if c.calls != nil {
		c.calls.Add(1)
	}
	return c.err
}

type memoryFeedStatSource struct {
	mu    sync.Mutex
	stats map[int64]*domainfeed.FeedStat
	err   error
	calls int
}

func (s *memoryFeedStatSource) BatchGetFeedStats(_ context.Context, ids []int64) (map[int64]*domainfeed.FeedStat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	result := map[int64]*domainfeed.FeedStat{}
	for _, id := range ids {
		if stat := s.stats[id]; stat != nil {
			cloned := *stat
			result[id] = &cloned
		}
	}
	return result, nil
}
func (s *memoryFeedStatSource) put(stat *domainfeed.FeedStat) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stats == nil {
		s.stats = map[int64]*domainfeed.FeedStat{}
	}
	cloned := *stat
	s.stats[stat.VideoID] = &cloned
}

func TestFeedStatRefreshRejectsStalePublicationAcrossClients(t *testing.T) {
	testFeedStatPublicationScenarios(t, func(t *testing.T) *redis.Options {
		server, err := miniredis.Run()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(server.Close)
		return &redis.Options{Addr: server.Addr(), MaxRetries: -1}
	})
}

func testFeedStatPublicationScenarios(t *testing.T, startRedis func(*testing.T) *redis.Options) {
	for _, scenario := range []string{"like", "unlike", "comment", "json_eviction", "redis_flush", "fence_eviction", "cold_rebuild"} {
		t.Run(scenario, func(t *testing.T) {
			options := startRedis(t)
			optionsB := *options
			clientA, clientB := redis.NewClient(options), redis.NewClient(&optionsB)
			defer clientA.Close()
			defer clientB.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			const id int64 = 101
			source := &memoryFeedStatSource{}
			base := &domainfeed.FeedStat{VideoID: id, Revision: 10, LikeCount: 100, CommentCount: 3, FavoriteCount: 2}
			source.put(base)
			cacheA := NewFeedCache(clientA, WithFeedStatSource(source))
			cacheB := NewFeedCache(clientB, WithFeedStatSource(source))
			if scenario != "cold_rebuild" {
				if err := cacheB.SetStats(ctx, map[int64]*domainfeed.FeedStat{id: base}, actionStatJSONTTL); err != nil {
					t.Fatal(err)
				}
			}
			old := &domainfeed.FeedStat{VideoID: id, Revision: 11, LikeCount: 101, CommentCount: 4, FavoriteCount: 2}
			source.put(old)
			hook := &pauseStatPublicationHook{key: feedStatKey(id), ready: make(chan struct{}), release: make(chan struct{})}
			clientA.AddHook(hook)
			done := make(chan error, 1)
			go func() { done <- cacheA.SetStats(ctx, map[int64]*domainfeed.FeedStat{id: old}, actionStatJSONTTL) }()
			select {
			case <-hook.ready:
			case err := <-done:
				t.Fatalf("A did not reach publication: %v", err)
			case <-ctx.Done():
				t.Fatal("timed out waiting for A")
			}
			newer := &domainfeed.FeedStat{VideoID: id, Revision: 12, LikeCount: 102, CommentCount: 5, FavoriteCount: 2}
			if scenario == "unlike" {
				newer.LikeCount = 100
			}
			if scenario == "comment" {
				newer.LikeCount = 101
			}
			source.put(newer)
			err := cacheB.SetVideoStat(ctx, &domaininteraction.VideoStat{
				VideoID: id, Revision: newer.Revision, LikeCount: newer.LikeCount,
				CommentCount: newer.CommentCount, FavoriteCount: newer.FavoriteCount,
			})
			if err == nil {
				switch scenario {
				case "json_eviction":
					err = clientB.Del(ctx, feedStatKey(id)).Err()
				case "fence_eviction":
					err = clientB.Del(ctx, feedStatRevisionKey(id)).Err()
				case "redis_flush":
					err = clientB.FlushDB(ctx).Err()
				}
			}
			close(hook.release)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("timed out waiting for A to finish")
			}
			if scenario == "json_eviction" {
				if exists := clientB.Exists(ctx, feedStatKey(id)).Val(); exists != 0 {
					t.Fatal("old database snapshot repopulated JSON after eviction")
				}
				if err := cacheB.SetStats(ctx, map[int64]*domainfeed.FeedStat{id: newer}, actionStatJSONTTL); err != nil {
					t.Fatal(err)
				}
			}
			cached := readStatJSONForTest(t, ctx, clientB, id)
			if *cached != *newer {
				t.Fatalf("stale snapshot: got %+v, want %+v", cached, newer)
			}
			if revision := clientB.Get(ctx, feedStatRevisionKey(id)).Val(); revision != "12" {
				t.Fatalf("revision fence = %s", revision)
			}
			ttl := clientB.PTTL(ctx, feedStatKey(id)).Val()
			if ttl <= 10*time.Second || ttl > actionStatJSONTTL {
				t.Fatalf("unexpected JSON TTL: %s", ttl)
			}
		})
	}
}

func TestFeedStatRebuildIgnoresRetiredCounters(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	ctx := context.Background()
	const id int64 = 104
	current := &domainfeed.FeedStat{VideoID: id, Revision: 9, LikeCount: 7}
	source := &memoryFeedStatSource{}
	source.put(current)
	cache.statSource = source
	for _, key := range []string{"video:stat:counter:v1:104:base", "video:stat:counter:v1:104:shard:00"} {
		if err := cache.client.HSet(ctx, key, "like_count", 999).Err(); err != nil {
			t.Fatal(err)
		}
	}
	misses, err := cache.GetStats(ctx, []int64{id})
	if err != nil || len(misses) != 0 {
		t.Fatalf("legacy counters became facts: %+v, %v", misses, err)
	}
	old := &domainfeed.FeedStat{VideoID: id, Revision: 2, LikeCount: 3}
	if err := cache.SetStats(ctx, map[int64]*domainfeed.FeedStat{id: old}, actionStatJSONTTL); err != nil {
		t.Fatal(err)
	}
	if actual := readStatJSONForTest(t, ctx, cache.client, id); *actual != *current {
		t.Fatalf("rebuild trusted stale values: %+v", actual)
	}
}

func TestFeedStatFenceExpiryRevalidatesDatabase(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	ctx := context.Background()
	source := &memoryFeedStatSource{}
	first := &domainfeed.FeedStat{VideoID: 105, Revision: 1, LikeCount: 100}
	source.put(first)
	cache := NewFeedCache(client, WithFeedStatSource(source))
	if err := cache.SetStats(ctx, map[int64]*domainfeed.FeedStat{105: first}, actionStatJSONTTL); err != nil {
		t.Fatal(err)
	}
	server.FastForward(feedStatRevisionTTL + time.Second)
	next := &domainfeed.FeedStat{VideoID: 105, Revision: 2, LikeCount: 99}
	source.put(next)
	if err := cache.SetStats(ctx, map[int64]*domainfeed.FeedStat{105: first}, actionStatJSONTTL); err != nil {
		t.Fatal(err)
	}
	if actual := readStatJSONForTest(t, ctx, client, 105); *actual != *next {
		t.Fatalf("expiry regressed counts: %+v", actual)
	}
}

func TestFeedStatColdRebuildBatchesAndFailsClosed(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	ctx := context.Background()
	stats := map[int64]*domainfeed.FeedStat{}
	for id := int64(1); id <= 20; id++ {
		stats[id] = &domainfeed.FeedStat{VideoID: id, Revision: 1, LikeCount: 1}
	}
	source := &memoryFeedStatSource{stats: stats}
	cache.statSource = source
	if err := cache.SetStats(ctx, stats, actionStatJSONTTL); err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 {
		t.Fatalf("cold rebuild introduced N+1: %d database calls", source.calls)
	}
	if err := cache.client.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	source.err = errors.New("database unavailable")
	if err := cache.SetStats(ctx, stats, actionStatJSONTTL); !errors.Is(err, source.err) {
		t.Fatalf("lost source error: %v", err)
	}
	if exists := cache.client.Exists(ctx, feedStatKey(1)).Val(); exists != 0 {
		t.Fatal("failed rebuild published an unverified snapshot")
	}
}

func TestFeedStatEqualRevisionDoesNotMixFieldsOrExtendTTL(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	ctx := context.Background()
	snapshot := &domainfeed.FeedStat{VideoID: 106, Revision: 5, LikeCount: 10, CommentCount: 7}
	source := &memoryFeedStatSource{}
	source.put(snapshot)
	cache := NewFeedCache(client, WithFeedStatSource(source))
	if err := cache.SetStats(ctx, map[int64]*domainfeed.FeedStat{106: snapshot}, actionStatJSONTTL); err != nil {
		t.Fatal(err)
	}
	server.FastForward(2 * time.Second)
	invalid := *snapshot
	invalid.CommentCount = 3
	if err := cache.SetStats(ctx, map[int64]*domainfeed.FeedStat{106: &invalid}, actionStatJSONTTL); err != nil {
		t.Fatal(err)
	}
	if actual := readStatJSONForTest(t, ctx, client, 106); *actual != *snapshot {
		t.Fatalf("equal revision mixed snapshots: %+v", actual)
	}
	if ttl := server.TTL(feedStatKey(106)); ttl != actionStatJSONTTL-2*time.Second {
		t.Fatalf("replay extended TTL: %s", ttl)
	}
}

func TestFeedStatRefreshContentionDoesNotPublishUncheckedSnapshot(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	ctx := context.Background()
	var calls atomic.Int32
	client := failingStatWatchClient{redisStatCacheClient: cache.client, err: redis.TxFailedErr, calls: &calls}
	stat := &domainfeed.FeedStat{VideoID: 103, Revision: 1, LikeCount: 100}
	err := publishFeedStatSnapshots(ctx, client, nil, map[int64]*domainfeed.FeedStat{103: stat}, actionStatJSONTTL)
	if !errors.Is(err, redis.TxFailedErr) || calls.Load() != feedStatRefreshAttempts {
		t.Fatalf("contention = %v, calls=%d", err, calls.Load())
	}
	if exists := cache.client.Exists(ctx, feedStatKey(103)).Val(); exists != 0 {
		t.Fatal("contention published unchecked JSON")
	}
}

func readStatJSONForTest(t *testing.T, ctx context.Context, client redis.Cmdable, videoID int64) *domainfeed.FeedStat {
	t.Helper()
	content, err := client.Get(ctx, feedStatKey(videoID)).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var stat domainfeed.FeedStat
	if err := json.Unmarshal(content, &stat); err != nil {
		t.Fatal(err)
	}
	return &stat
}
