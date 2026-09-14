package infracache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

const feedStatRefreshAttempts = 3
const feedStatRevisionTTL = 24 * time.Hour

var errFeedStatSourceRequired = errors.New("authoritative feed stat source is required to rebuild a missing revision fence")

func feedStatRevisionKey(videoID int64) string {
	return fmt.Sprintf("video:stat:revision:v1:%d", videoID)
}

// publishFeedStatSnapshots caches complete database snapshots, never database
// baselines plus Redis deltas. Revision is advanced in the same PostgreSQL
// transaction as the counts. It is independent of whether counts rise or fall.
//
// The revision fence outlives JSON eviction. If that fence is also missing
// (expiry, eviction, FLUSHDB or restart), reload from PostgreSQL AFTER WATCH,
// rather than trusting a caller's snapshot fetched before the cache loss.
// Rebuild queries are batched, including for publication preheat/Feed backfill.
func publishFeedStatSnapshots(ctx context.Context, client redisStatCacheClient, source FeedStatSource, incoming map[int64]*domainfeed.FeedStat, ttl time.Duration) error {
	ids := make([]int64, 0, len(incoming))
	for id, stat := range incoming {
		if stat != nil && id > 0 && stat.VideoID == id && stat.Revision >= 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	keys := make([]string, 0, len(ids)*2)
	for _, id := range ids {
		keys = append(keys, feedStatRevisionKey(id))
	}
	for _, id := range ids {
		keys = append(keys, feedStatKey(id))
	}
	fenceTTL := feedStatRevisionTTL
	if ttl >= fenceTTL {
		fenceTTL = ttl + time.Second
	}
	for attempt := 0; attempt < feedStatRefreshAttempts; attempt++ {
		written := 0
		err := client.Watch(ctx, func(tx *redis.Tx) error {
			values, err := tx.MGet(ctx, keys...).Result()
			if err != nil {
				return err
			}
			revisions := make(map[int64]int64, len(ids))
			current := make(map[int64]*domainfeed.FeedStat, len(ids))
			missing := make([]int64, 0)
			for index, id := range ids {
				raw, present := cacheValueBytes(values[index])
				revision, parseErr := strconv.ParseInt(string(raw), 10, 64)
				if !present || parseErr != nil || revision < 0 {
					missing = append(missing, id)
				} else {
					revisions[id] = revision
				}
				if content, ok := cacheValueBytes(values[index+len(ids)]); ok {
					var stat domainfeed.FeedStat
					if json.Unmarshal(content, &stat) == nil && stat.VideoID == id && stat.Revision >= 0 {
						current[id] = &stat
					}
				}
			}
			var rebuilt map[int64]*domainfeed.FeedStat
			if len(missing) > 0 {
				if source == nil {
					return errFeedStatSourceRequired
				}
				rebuilt, err = source.BatchGetFeedStats(ctx, missing)
				if err != nil {
					return err
				}
			}
			pending := make(map[int64][]byte, len(ids))
			pendingRevisions := make(map[int64]int64, len(ids))
			for _, id := range ids {
				stat := incoming[id]
				revision, fenced := revisions[id]
				if !fenced {
					stat = rebuilt[id]
					if stat == nil || stat.VideoID != id || stat.Revision < 0 {
						continue
					}
				}
				if fenced && stat.Revision < revision {
					continue
				}
				if cached := current[id]; cached != nil {
					if stat.Revision < cached.Revision {
						continue
					}
					// Equal revision is immutable. Do not extend the TTL of an
					// already published value or mix fields from another query.
					if fenced && cached.Revision == stat.Revision {
						continue
					}
				}
				content, err := json.Marshal(stat)
				if err != nil {
					return err
				}
				pending[id] = content
				pendingRevisions[id] = stat.Revision
			}
			if len(pending) == 0 {
				return nil
			}
			written = len(pending)
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				for _, id := range ids {
					content, ok := pending[id]
					if !ok {
						continue
					}
					pipe.Set(ctx, feedStatKey(id), content, ttl)
					pipe.Set(ctx, feedStatRevisionKey(id), strconv.FormatInt(pendingRevisions[id], 10), fenceTTL)
				}
				return nil
			})
			return err
		}, keys...)
		if errors.Is(err, redis.TxFailedErr) {
			continue
		}
		inframetrics.ObserveCacheWrite("stat", written, err)
		return err
	}
	// Contention is an optional-cache failure. Never downgrade to a plain SET.
	inframetrics.ObserveCacheWrite("stat", len(ids), redis.TxFailedErr)
	return redis.TxFailedErr
}
