package infracache

import (
	applicationfeed "github.com/shiyudesu/frux/internal/application/feed"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const hotWindowMinutes = 60
const hotMinuteBucketTTL = 2 * time.Hour
const hotWindowCacheTTL = 2 * time.Minute
const actionStateTTL = 30 * 24 * time.Hour
const actionStatTTL = 24 * time.Hour
const actionStatJSONTTL = 15 * time.Second
const actionStatCounterShardCount = 16
const followingIndexKeyTTL = 30 * 24 * time.Hour
const actionIdempotencyReceiptLimit = 32
const actionIdempotencyReceiptsField = "idempotency_receipts"
const actionIdempotencyReceiptsMaxBytes = actionIdempotencyReceiptLimit * (domaininteraction.MaxIdempotencyKeyLength*6 + 32)
const actionStateHandoffConfirmedField = "handoff_confirmed"
const actionStateHandoffDependencyField = "handoff_dependency"

type redisWatchCmdable interface {
	redis.Cmdable
	Pipeline() redis.Pipeliner
	Watch(ctx context.Context, fn func(*redis.Tx) error, keys ...string) error
}

type redisActionStatReader interface {
	HGetAll(ctx context.Context, key string) *redis.MapStringStringCmd
	Get(ctx context.Context, key string) *redis.StringCmd
}

type redisActionStatWriter interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
}

type redisActionStatReadWriter interface {
	redisActionStatReader
	redisActionStatWriter
}

type redisStatCacheClient interface {
	redisActionStatReadWriter
	MGet(ctx context.Context, keys ...string) *redis.SliceCmd
}

// FeedCache 使用 Redis 保存 Feed 查询结果。
type FeedCache struct {
	client redisWatchCmdable
}

// NewFeedCache 创建 Feed 结果缓存。
func NewFeedCache(client redisWatchCmdable) *FeedCache {
	return &FeedCache{client: client}
}

// GetPage 读取缓存中的轻量 Feed 页。
func (c *FeedCache) GetPage(ctx context.Context, key string) (*applicationfeed.FeedPage, bool, error) {
	content, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		inframetrics.ObserveCacheRead("page", 1, 0, nil)
		return nil, false, nil
	}
	if err != nil {
		inframetrics.ObserveCacheRead("page", 1, 0, err)
		return nil, false, err
	}

	var page applicationfeed.FeedPage
	if err := json.Unmarshal(content, &page); err != nil {
		inframetrics.ObserveCacheRead("page", 1, 0, err)
		return nil, false, err
	}
	inframetrics.ObserveCacheRead("page", 1, 1, nil)
	return &page, true, nil
}

// SetPage 写入轻量 Feed 页，并设置过期时间。
func (c *FeedCache) SetPage(ctx context.Context, key string, page *applicationfeed.FeedPage, ttl time.Duration) error {
	content, err := json.Marshal(page)
	if err != nil {
		inframetrics.ObserveCacheWrite("page", 1, err)
		return err
	}
	err = c.client.Set(ctx, key, content, ttl).Err()
	inframetrics.ObserveCacheWrite("page", 1, err)
	return err
}

// GetCards 批量读取视频卡片缓存。
func (c *FeedCache) GetCards(ctx context.Context, videoIDs []int64) (map[int64]*domainfeed.FeedCard, error) {
	cards := map[int64]*domainfeed.FeedCard{}
	if len(videoIDs) == 0 {
		return cards, nil
	}

	values, err := c.client.MGet(ctx, cacheKeys(videoIDs, feedCardKey)...).Result()
	if err != nil {
		inframetrics.ObserveCacheRead("card", len(videoIDs), 0, err)
		return nil, err
	}
	for index, value := range values {
		content, ok := cacheValueBytes(value)
		if !ok {
			continue
		}
		var card domainfeed.FeedCard
		if err := json.Unmarshal(content, &card); err != nil {
			continue
		}
		if card.VideoID <= 0 {
			card.VideoID = videoIDs[index]
		}
		cards[card.VideoID] = &card
	}
	inframetrics.ObserveCacheRead("card", len(videoIDs), len(cards), nil)
	return cards, nil
}

// SetCards 批量写入视频卡片缓存。
func (c *FeedCache) SetCards(ctx context.Context, cards map[int64]*domainfeed.FeedCard, ttl time.Duration) error {
	pipe := c.client.Pipeline()
	queued := false

	for _, card := range cards {
		if card == nil || card.VideoID <= 0 {
			continue
		}
		content, err := json.Marshal(card)
		if err != nil {
			return err
		}
		pipe.Set(ctx, feedCardKey(card.VideoID), content, ttl)
		queued = true
	}
	if !queued {
		return nil
	}
	_, err := pipe.Exec(ctx)
	inframetrics.ObserveCacheWrite("card", len(cards), err)
	return err
}

func (c *FeedCache) InvalidateVideo(ctx context.Context, videoID int64) error {
	if c == nil || videoID <= 0 {
		return nil
	}
	return c.client.Del(ctx, feedCardKey(videoID), feedStatKey(videoID)).Err()
}

// GetStats 批量读取视频计数缓存。
func (c *FeedCache) GetStats(ctx context.Context, videoIDs []int64) (map[int64]*domainfeed.FeedStat, error) {
	return getStats(ctx, c.client, videoIDs)
}

func getStats(ctx context.Context, client redisStatCacheClient, videoIDs []int64) (map[int64]*domainfeed.FeedStat, error) {
	stats := map[int64]*domainfeed.FeedStat{}
	if len(videoIDs) == 0 {
		return stats, nil
	}

	values, err := client.MGet(ctx, cacheKeys(videoIDs, feedStatKey)...).Result()
	if err != nil {
		inframetrics.ObserveCacheRead("stat", len(videoIDs), 0, err)
		return nil, err
	}
	for index, value := range values {
		content, ok := cacheValueBytes(value)
		if !ok {
			continue
		}
		var stat domainfeed.FeedStat
		if err := json.Unmarshal(content, &stat); err != nil {
			continue
		}
		if stat.VideoID <= 0 {
			stat.VideoID = videoIDs[index]
		}
		stats[stat.VideoID] = &stat
	}
	for _, videoID := range videoIDs {
		if stats[videoID] != nil {
			continue
		}
		stat, ok, err := actionStatFromCache(ctx, client, videoID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		stats[videoID] = stat
		_ = setActionStatJSON(ctx, client, feedStatKey(videoID), stat)
	}
	inframetrics.ObserveCacheRead("stat", len(videoIDs), len(stats), nil)
	return stats, nil
}

// SetStats 批量写入视频计数缓存。
func (c *FeedCache) SetStats(ctx context.Context, stats map[int64]*domainfeed.FeedStat, ttl time.Duration) error {
	pipe := c.client.Pipeline()
	queued := false

	for _, stat := range stats {
		if stat == nil || stat.VideoID <= 0 {
			continue
		}
		content, err := json.Marshal(stat)
		if err != nil {
			return err
		}
		pipe.Set(ctx, feedStatKey(stat.VideoID), content, ttl)
		queued = true
	}
	if !queued {
		return nil
	}
	_, err := pipe.Exec(ctx)
	inframetrics.ObserveCacheWrite("stat", len(stats), err)
	return err
}

// SetVideoStat 写入单个视频的计数缓存，用于评论写入后刷新 Feed 展示。
func (c *FeedCache) SetVideoStat(ctx context.Context, stat *domaininteraction.VideoStat) error {
	if stat == nil || stat.VideoID <= 0 {
		return nil
	}
	err := setActionStatJSON(ctx, c.client, feedStatKey(stat.VideoID), videoStatToFeedStat(stat))
	inframetrics.ObserveCacheWrite("stat", 1, err)
	return err
}

func videoStatToFeedStat(stat *domaininteraction.VideoStat) *domainfeed.FeedStat {
	if stat == nil {
		return nil
	}
	return &domainfeed.FeedStat{
		VideoID:       stat.VideoID,
		LikeCount:     stat.LikeCount,
		CommentCount:  stat.CommentCount,
		FavoriteCount: stat.FavoriteCount,
	}
}

func (c *FeedCache) AddInboxItems(ctx context.Context, authorID int64, userIDs []int64, item *domainfeed.FeedPageItem, maxLen int64) error {
	if authorID <= 0 || item == nil || item.VideoID <= 0 || item.PublishedAt.IsZero() || len(userIDs) == 0 {
		return nil
	}
	if maxLen <= 0 {
		maxLen = 1000
	}
	pipe := c.client.Pipeline()
	score := followingIndexScore(item.PublishedAt, item.VideoID)
	member := followingIndexMember(item.VideoID, authorID, item.PublishedAt)
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		key := followingInboxKey(userID)
		pipe.ZAdd(ctx, key, redis.Z{Score: score, Member: member})
		pipe.ZRemRangeByRank(ctx, key, 0, -maxLen-1)
		pipe.Expire(ctx, key, followingIndexKeyTTL)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (c *FeedCache) AddAuthorOutboxItem(ctx context.Context, authorID int64, item *domainfeed.FeedPageItem, maxLen int64) error {
	if authorID <= 0 || item == nil || item.VideoID <= 0 || item.PublishedAt.IsZero() {
		return nil
	}
	if maxLen <= 0 {
		maxLen = 500
	}
	key := followingAuthorOutboxKey(authorID)
	score := followingIndexScore(item.PublishedAt, item.VideoID)
	member := followingIndexMember(item.VideoID, authorID, item.PublishedAt)
	pipe := c.client.Pipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: score, Member: member})
	pipe.ZRemRangeByRank(ctx, key, 0, -maxLen-1)
	pipe.Expire(ctx, key, followingIndexKeyTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *FeedCache) ListFollowingIndexPage(ctx context.Context, viewerID int64, authorIDs []int64, cursor *domainfeed.TimelineCursor, limit int) ([]*domainfeed.FeedPageItem, bool, error) {
	if viewerID <= 0 || limit <= 0 {
		return []*domainfeed.FeedPageItem{}, false, nil
	}
	keys := []string{followingInboxKey(viewerID)}
	for _, authorID := range authorIDs {
		if authorID > 0 {
			keys = append(keys, followingAuthorOutboxKey(authorID))
		}
	}

	pipe := c.client.Pipeline()
	cardinalityCommands := make([]*redis.IntCmd, 0, len(keys))
	rangeCommands := make([]*redis.StringSliceCmd, 0, len(keys))
	minScore := "-inf"
	maxScore := "+inf"
	if cursor != nil {
		maxScore = fmt.Sprintf("(%f", followingIndexScore(cursor.PublishedAt, cursor.VideoID))
	}
	for _, key := range keys {
		cardinalityCommands = append(cardinalityCommands, pipe.ZCard(ctx, key))
		rangeCommands = append(rangeCommands, pipe.ZRevRangeByScore(ctx, key, &redis.ZRangeBy{
			Min:   minScore,
			Max:   maxScore,
			Count: int64(limit),
		}))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, false, err
	}

	hasIndex := false
	for _, cmd := range cardinalityCommands {
		count, err := cmd.Result()
		if err != nil && err != redis.Nil {
			return nil, false, err
		}
		if count > 0 {
			hasIndex = true
			break
		}
	}
	if !hasIndex {
		return nil, false, nil
	}

	seen := map[int64]struct{}{}
	items := make([]*domainfeed.FeedPageItem, 0, limit*len(rangeCommands))
	for commandIndex, cmd := range rangeCommands {
		members, err := cmd.Result()
		if err != nil && err != redis.Nil {
			return nil, false, err
		}
		for _, member := range members {
			item, ok := feedPageItemFromFollowingMember(member)
			if !ok {
				continue
			}
			if commandIndex > 0 && item.AuthorID > 0 {
				allowedAuthors := int64Set(authorIDs)
				if _, followed := allowedAuthors[item.AuthorID]; !followed {
					continue
				}
			}
			if _, exists := seen[item.VideoID]; exists {
				continue
			}
			seen[item.VideoID] = struct{}{}
			items = append(items, item)
		}
	}
	sortFeedPageItemsByTimeline(items)
	if len(items) > limit {
		items = items[:limit]
	}
	return items, true, nil
}

// AddHotScore 把一次互动热度写入 1 分钟粒度的热榜桶。
func (c *FeedCache) AddHotScore(ctx context.Context, videoID int64, scoreDelta int, at time.Time) error {
	if videoID <= 0 || scoreDelta == 0 {
		return nil
	}

	key := hotMinuteKey(at)
	if err := c.client.ZIncrBy(ctx, key, float64(scoreDelta), hotRankMember(videoID)).Err(); err != nil {
		return err
	}
	return c.client.Expire(ctx, key, hotMinuteBucketTTL).Err()
}

// ListHotWindowPage 合并最近 60 个分钟桶，返回一小时滑动窗口内的热榜页。
func (c *FeedCache) ListHotWindowPage(ctx context.Context, windowEnd time.Time, offset int, limit int) ([]*domainfeed.FeedPageItem, error) {
	items := []*domainfeed.FeedPageItem{}
	if limit <= 0 {
		return items, nil
	}
	if offset < 0 {
		offset = 0
	}

	windowEnd = windowEnd.UTC().Truncate(time.Minute)
	windowKey := hotWindowKey(windowEnd)
	exists, err := c.client.Exists(ctx, windowKey).Result()
	if err != nil {
		return nil, err
	}
	if exists == 0 {
		if err := c.rebuildHotWindow(ctx, windowKey, windowEnd); err != nil {
			return nil, err
		}
	}

	return c.listHotWindowPage(ctx, windowKey, offset, limit)
}

func (c *FeedCache) rebuildHotWindow(ctx context.Context, windowKey string, windowEnd time.Time) error {
	if _, err := c.client.ZUnionStore(ctx, windowKey, &redis.ZStore{
		Keys:      hotWindowMinuteKeys(windowEnd),
		Aggregate: "SUM",
	}).Result(); err != nil {
		return err
	}

	pipe := c.client.Pipeline()
	pipe.ZRemRangeByScore(ctx, windowKey, "-inf", "0")
	pipe.Expire(ctx, windowKey, hotWindowCacheTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *FeedCache) listHotWindowPage(ctx context.Context, windowKey string, offset int, limit int) ([]*domainfeed.FeedPageItem, error) {
	items := []*domainfeed.FeedPageItem{}
	values, err := c.client.ZRevRangeWithScores(ctx, windowKey, int64(offset), int64(offset+limit-1)).Result()
	if err != nil {
		return nil, err
	}
	for _, value := range values {
		member, ok := value.Member.(string)
		if !ok {
			continue
		}
		videoID, ok := hotRankVideoID(member)
		if !ok {
			continue
		}
		items = append(items, &domainfeed.FeedPageItem{
			VideoID:  videoID,
			HotScore: int(value.Score),
		})
	}
	return items, nil
}

// SetActionState 写入 Redis 行为状态和实时计数，供点赞收藏接口快速返回。
func (c *FeedCache) SetActionState(ctx context.Context, userID int64, videoID int64, actionType string, active bool, idempotencyKey string, initialStat *domaininteraction.VideoStat, initialState *domaininteraction.ActionStateSnapshot, mutation applicationinteraction.ActionMutation) (*applicationinteraction.ActionStateResult, error) {
	actionType, err := domaininteraction.NormalizeActionType(actionType)
	if err != nil {
		return nil, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if strings.TrimSpace(mutation.EventID) == "" || mutation.OccurredAt.IsZero() {
		return nil, domaininteraction.ErrInvalidActionEvent
	}

	actionKey := interactionActionKey(userID, videoID, actionType)
	counterBaseKey := interactionStatCounterBaseKey(videoID)
	counterShardKey := interactionStatCounterShardKey(videoID, interactionStatCounterShardIndex(userID))
	jsonKey := feedStatKey(videoID)
	targetStatus := domaininteraction.ActionStatusCanceled
	if active {
		targetStatus = domaininteraction.ActionStatusActive
	}

	var result *applicationinteraction.ActionStateResult
	err = c.client.Watch(ctx, func(tx *redis.Tx) error {
		values, err := tx.HGetAll(ctx, actionKey).Result()
		if err != nil {
			return err
		}

		previous, cached := actionStateSnapshotFromRedis(values)
		receipts := actionIdempotencyReceiptsFromRedis(values[actionIdempotencyReceiptsField])
		handoffConfirmed := actionStateHandoffConfirmed(values, receipts, previous)
		baselineConfirmsCurrent := false
		if initialState != nil && (!cached || initialState.Version > previous.Version) {
			previous = *initialState
			cached = false
			handoffConfirmed = true
		}
		if initialState != nil && cached && actionStateSnapshotsMatch(*initialState, previous) {
			handoffConfirmed = true
			baselineConfirmsCurrent = !actionStateHandoffStored(values[actionStateHandoffConfirmedField])
		}
		if receipt, found := actionIdempotencyReceiptForKey(receipts, idempotencyKey); found {
			if receipt.Active != active {
				return domaininteraction.ErrActionIdempotencyConflict
			}
			if baselineConfirmsCurrent {
				if _, err := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
					pipe.HSet(ctx, actionKey, actionStateHandoffConfirmedField, 1)
					pipe.Expire(ctx, actionKey, actionStateTTL)
					return nil
				}); err != nil {
					return err
				}
			}
			result = &applicationinteraction.ActionStateResult{
				UserID:                  userID,
				VideoID:                 videoID,
				ActionType:              actionType,
				Active:                  receipt.Active,
				IdempotencyKey:          receipt.Key,
				RecommendationRequestID: receipt.RecommendationRequestID,
				Version:                 receipt.Version,
				EventID:                 receipt.EventID,
				OccurredAt:              receipt.OccurredAt,
				ShouldPublish:           !receipt.NoEvent && !receipt.HandoffConfirmed && !(actionIdempotencyReceiptReferencesSnapshot(receipt, previous) && handoffConfirmed),
			}
			return nil
		}
		if previous.Exists && idempotencyKey != "" && previous.IdempotencyKey == idempotencyKey {
			if previous.Active != active {
				return domaininteraction.ErrActionIdempotencyConflict
			}
			receipt := actionIdempotencyReceiptFromSnapshot(previous, handoffConfirmed, false)
			if baselineConfirmsCurrent {
				if _, err := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
					pipe.HSet(ctx, actionKey, actionStateHandoffConfirmedField, 1)
					pipe.Expire(ctx, actionKey, actionStateTTL)
					return nil
				}); err != nil {
					return err
				}
			}
			if receipt.EventID != "" {
				receipts = appendActionIdempotencyReceipt(receipts, receipt)
				encodedReceipts, err := json.Marshal(receipts)
				if err != nil {
					return err
				}
				_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
					pipe.HSet(ctx, actionKey, actionIdempotencyReceiptsField, string(encodedReceipts))
					pipe.Expire(ctx, actionKey, actionStateTTL)
					return nil
				})
				if err != nil {
					return err
				}
			}
			result = &applicationinteraction.ActionStateResult{
				UserID:                  userID,
				VideoID:                 videoID,
				ActionType:              actionType,
				Active:                  previous.Active,
				IdempotencyKey:          previous.IdempotencyKey,
				RecommendationRequestID: previous.RecommendationRequestID,
				Version:                 previous.Version,
				EventID:                 previous.EventID,
				OccurredAt:              previous.OccurredAt,
				ShouldPublish:           receipt.EventID != "" && !handoffConfirmed,
			}
			return nil
		}

		delta := 0
		if !previous.Exists {
			if active {
				delta = 1
			}
		} else if previous.Active != active {
			if active {
				delta = 1
			} else {
				delta = -1
			}
		}
		if delta == 0 {
			needsHandoff := actionStateNeedsHandoff(previous) && !handoffConfirmed
			if idempotencyKey != "" {
				if needsHandoff {
					receipt := actionIdempotencyReceiptFromSnapshot(previous, false, true)
					receipt.Key = idempotencyKey
					receipts = appendActionIdempotencyReceipt(receipts, receipt)
				} else {
					receipts = appendActionIdempotencyReceipt(receipts, actionIdempotencyNoEventReceipt(idempotencyKey, active))
				}
				encodedReceipts, err := json.Marshal(receipts)
				if err != nil {
					return err
				}
				_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
					fields := map[string]any{actionIdempotencyReceiptsField: string(encodedReceipts)}
					if !cached {
						fields["status"] = actionStatusValue(previous.Active)
						fields["idempotency_key"] = previous.IdempotencyKey
						fields["recommendation_request_id"] = previous.RecommendationRequestID
						fields["version_counter"] = maxActionVersion(parseActionVersion(values["version_counter"]), previous.Version)
						fields["state_version"] = previous.Version
						fields["event_id"] = previous.EventID
						fields["occurred_at"] = formatOptionalActionTime(previous.OccurredAt)
						fields["updated_at"] = formatOptionalActionTime(previous.UpdatedAt)
						fields[actionStateHandoffConfirmedField] = actionStateHandoffFlag(handoffConfirmed)
						fields[actionStateHandoffDependencyField] = 0
					}
					if needsHandoff {
						fields[actionStateHandoffDependencyField] = 1
					}
					if baselineConfirmsCurrent {
						fields[actionStateHandoffConfirmedField] = 1
					}
					pipe.HSet(ctx, actionKey, fields)
					pipe.Expire(ctx, actionKey, actionStateTTL)
					return nil
				})
				if err != nil {
					return err
				}
			} else if needsHandoff || baselineConfirmsCurrent {
				_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
					fields := map[string]any{}
					if needsHandoff {
						fields[actionStateHandoffDependencyField] = 1
					}
					if baselineConfirmsCurrent {
						fields[actionStateHandoffConfirmedField] = 1
					}
					pipe.HSet(ctx, actionKey, fields)
					pipe.Expire(ctx, actionKey, actionStateTTL)
					return nil
				})
				if err != nil {
					return err
				}
			}
			result = &applicationinteraction.ActionStateResult{
				UserID:                  userID,
				VideoID:                 videoID,
				ActionType:              actionType,
				Active:                  previous.Active,
				IdempotencyKey:          idempotencyKey,
				RecommendationRequestID: previous.RecommendationRequestID,
				Version:                 previous.Version,
				EventID:                 previous.EventID,
				OccurredAt:              previous.OccurredAt,
				ShouldPublish:           needsHandoff,
			}
			return nil
		}

		baseStat := actionStatBaseInit(videoID, initialStat)
		versionCounter := maxActionVersion(parseActionVersion(values["version_counter"]), previous.Version)
		nextVersion := versionCounter + 1
		occurredAt := mutation.OccurredAt.UTC().Format(time.RFC3339Nano)
		if idempotencyKey != "" {
			receipts = appendActionIdempotencyReceipt(receipts, actionIdempotencyReceiptFromMutation(idempotencyKey, active, nextVersion, mutation))
		}
		encodedReceipts, err := json.Marshal(receipts)
		if err != nil {
			return err
		}

		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, actionKey, map[string]any{
				"status":                          targetStatus,
				"idempotency_key":                 idempotencyKey,
				actionIdempotencyReceiptsField:    string(encodedReceipts),
				"recommendation_request_id":       strings.TrimSpace(mutation.RecommendationRequestID),
				"version_counter":                 nextVersion,
				"state_version":                   nextVersion,
				"event_id":                        mutation.EventID,
				"occurred_at":                     occurredAt,
				"updated_at":                      occurredAt,
				actionStateHandoffConfirmedField:  0,
				actionStateHandoffDependencyField: 0,
			})
			pipe.Expire(ctx, actionKey, actionStateTTL)
			queueActionStatBaseInit(ctx, pipe, counterBaseKey, baseStat)
			pipe.Expire(ctx, counterBaseKey, actionStatTTL)
			if delta != 0 {
				pipe.HIncrBy(ctx, counterShardKey, interactionStatField(actionType), int64(delta))
			}
			pipe.Expire(ctx, counterShardKey, actionStatTTL)
			return nil
		})
		if err != nil {
			return err
		}

		result = &applicationinteraction.ActionStateResult{
			UserID:                   userID,
			VideoID:                  videoID,
			ActionType:               actionType,
			Active:                   active,
			Delta:                    delta,
			IdempotencyKey:           idempotencyKey,
			RecommendationRequestID:  strings.TrimSpace(mutation.RecommendationRequestID),
			Version:                  nextVersion,
			EventID:                  mutation.EventID,
			OccurredAt:               mutation.OccurredAt.UTC(),
			ShouldPublish:            delta != 0,
			CanRollback:              true,
			Previous:                 previous,
			PreviousHandoffConfirmed: handoffConfirmed,
			PreviousHasDependency:    actionStateHasHandoffDependency(values),
		}
		return nil
	}, actionKey)
	if err != nil {
		return nil, err
	}

	return completeActionStateResult(ctx, c.client, result, counterBaseKey, jsonKey, initialStat)
}

func completeActionStateResult(ctx context.Context, client redisActionStatReadWriter, result *applicationinteraction.ActionStateResult, counterBaseKey string, jsonKey string, initialStat *domaininteraction.VideoStat) (*applicationinteraction.ActionStateResult, error) {
	stat, err := actionStat(ctx, client, counterBaseKey, interactionStatCounterShardKeys(result.VideoID), jsonKey, result.VideoID, initialStat)
	if err != nil {
		return result, fmt.Errorf("read interaction counts after Redis mutation: %w", err)
	}
	result.LikeCount = stat.LikeCount
	result.FavoriteCount = stat.FavoriteCount
	_ = setActionStatJSON(ctx, client, jsonKey, stat)
	return result, nil
}

// ConfirmActionStateHandoff records that the current state version reached a
// durable handoff and confirms every receipt that depends on that version.
func (c *FeedCache) ConfirmActionStateHandoff(ctx context.Context, state *applicationinteraction.ActionStateResult) error {
	if state == nil || state.UserID <= 0 || state.VideoID <= 0 || strings.TrimSpace(state.EventID) == "" {
		return nil
	}
	actionKey := interactionActionKey(state.UserID, state.VideoID, state.ActionType)
	return c.client.Watch(ctx, func(tx *redis.Tx) error {
		values, err := tx.HGetAll(ctx, actionKey).Result()
		if err != nil {
			return err
		}
		receipts := actionIdempotencyReceiptsFromRedis(values[actionIdempotencyReceiptsField])
		receiptsChanged := false
		for index := range receipts {
			if !actionIdempotencyReceiptReferencesState(receipts[index], state) {
				continue
			}
			if receipts[index].HandoffConfirmed {
				continue
			}
			receipts[index].HandoffConfirmed = true
			receiptsChanged = true
		}
		currentMatches := actionStateMatchesResult(values, state)
		if !receiptsChanged && (!currentMatches || actionStateHandoffStored(values[actionStateHandoffConfirmedField])) {
			return nil
		}
		encodedReceipts, err := json.Marshal(receipts)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			if receiptsChanged {
				pipe.HSet(ctx, actionKey, actionIdempotencyReceiptsField, string(encodedReceipts))
			}
			if currentMatches {
				pipe.HSet(ctx, actionKey, actionStateHandoffConfirmedField, 1)
			}
			pipe.Expire(ctx, actionKey, actionStateTTL)
			return nil
		})
		return err
	}, actionKey)
}

// RollbackActionState reverses only an unconfirmed state version with no later
// request that depends on that version.
func (c *FeedCache) RollbackActionState(ctx context.Context, state *applicationinteraction.ActionStateResult) (bool, error) {
	if state == nil || !state.CanRollback || state.UserID <= 0 || state.VideoID <= 0 || state.Version <= 0 {
		return false, nil
	}
	actionKey := interactionActionKey(state.UserID, state.VideoID, state.ActionType)
	counterShardKey := interactionStatCounterShardKey(state.VideoID, interactionStatCounterShardIndex(state.UserID))
	jsonKey := feedStatKey(state.VideoID)
	rolledBack := false
	err := c.client.Watch(ctx, func(tx *redis.Tx) error {
		values, err := tx.HGetAll(ctx, actionKey).Result()
		if err != nil {
			return err
		}
		if parseActionVersion(values["state_version"]) != state.Version || values["event_id"] != state.EventID {
			return nil
		}
		receipts := actionIdempotencyReceiptsFromRedis(values[actionIdempotencyReceiptsField])
		if actionStateHandoffConfirmed(values, receipts, actionStateSnapshotForResult(state)) ||
			actionStateHasHandoffDependency(values) ||
			actionIdempotencyReceiptsHaveDependency(receipts, state) {
			return nil
		}
		receipts = removeActionIdempotencyReceiptsForState(receipts, state)
		encodedReceipts, err := json.Marshal(receipts)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			if state.Previous.Exists {
				pipe.HSet(ctx, actionKey, map[string]any{
					"status":                          actionStatusValue(state.Previous.Active),
					"idempotency_key":                 state.Previous.IdempotencyKey,
					"recommendation_request_id":       state.Previous.RecommendationRequestID,
					"state_version":                   state.Previous.Version,
					"event_id":                        state.Previous.EventID,
					"occurred_at":                     formatOptionalActionTime(state.Previous.OccurredAt),
					"updated_at":                      formatOptionalActionTime(state.Previous.UpdatedAt),
					actionStateHandoffConfirmedField:  actionStateHandoffFlag(state.PreviousHandoffConfirmed),
					actionStateHandoffDependencyField: actionStateHandoffFlag(state.PreviousHasDependency),
				})
			} else {
				pipe.HDel(ctx, actionKey, "status", "idempotency_key", "recommendation_request_id", "state_version", "event_id", "occurred_at", "updated_at", actionStateHandoffConfirmedField, actionStateHandoffDependencyField)
			}
			if len(receipts) == 0 {
				pipe.HDel(ctx, actionKey, actionIdempotencyReceiptsField)
			} else {
				pipe.HSet(ctx, actionKey, actionIdempotencyReceiptsField, string(encodedReceipts))
			}
			pipe.Expire(ctx, actionKey, actionStateTTL)
			if state.Delta != 0 {
				pipe.HIncrBy(ctx, counterShardKey, interactionStatField(state.ActionType), int64(-state.Delta))
				pipe.Expire(ctx, counterShardKey, actionStatTTL)
			}
			pipe.Del(ctx, jsonKey)
			return nil
		})
		if err == nil {
			rolledBack = true
		}
		return err
	}, actionKey)
	return rolledBack, err
}

func actionStateSnapshotFromRedis(values map[string]string) (domaininteraction.ActionStateSnapshot, bool) {
	status, err := strconv.Atoi(values["status"])
	if err != nil || (status != domaininteraction.ActionStatusActive && status != domaininteraction.ActionStatusCanceled) {
		return domaininteraction.ActionStateSnapshot{}, false
	}
	snapshot := domaininteraction.ActionStateSnapshot{
		Exists:                  true,
		Active:                  status == domaininteraction.ActionStatusActive,
		IdempotencyKey:          strings.TrimSpace(values["idempotency_key"]),
		RecommendationRequestID: strings.TrimSpace(values["recommendation_request_id"]),
		Version:                 parseActionVersion(values["state_version"]),
		EventID:                 strings.TrimSpace(values["event_id"]),
		OccurredAt:              parseOptionalActionTime(values["occurred_at"]),
		UpdatedAt:               parseOptionalActionTime(values["updated_at"]),
	}
	return snapshot, true
}

type actionIdempotencyReceipt struct {
	Key                     string    `json:"key"`
	Active                  bool      `json:"active"`
	EventID                 string    `json:"event_id,omitempty"`
	RecommendationRequestID string    `json:"recommendation_request_id,omitempty"`
	Version                 int64     `json:"version,omitempty"`
	OccurredAt              time.Time `json:"occurred_at,omitempty"`
	NoEvent                 bool      `json:"no_event,omitempty"`
	HandoffConfirmed        bool      `json:"handoff_confirmed,omitempty"`
	Dependent               bool      `json:"dependent,omitempty"`
}

func actionIdempotencyReceiptsFromRedis(raw string) []actionIdempotencyReceipt {
	if len(raw) == 0 || len(raw) > actionIdempotencyReceiptsMaxBytes {
		return nil
	}
	var parsed []actionIdempotencyReceipt
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed) > actionIdempotencyReceiptLimit {
		return nil
	}
	receipts := make([]actionIdempotencyReceipt, 0, len(parsed))
	seen := make(map[string]struct{}, len(parsed))
	for _, receipt := range parsed {
		receipt.Key = strings.TrimSpace(receipt.Key)
		if receipt.Key == "" || len(receipt.Key) > domaininteraction.MaxIdempotencyKeyLength {
			continue
		}
		receipt.EventID = strings.TrimSpace(receipt.EventID)
		receipt.RecommendationRequestID = strings.TrimSpace(receipt.RecommendationRequestID)
		if receipt.NoEvent {
			if receipt.EventID != "" || receipt.RecommendationRequestID != "" || receipt.Version != 0 || !receipt.OccurredAt.IsZero() || receipt.Dependent {
				continue
			}
			receipt.HandoffConfirmed = true
		} else if receipt.EventID == "" ||
			len(receipt.EventID) > domaininteraction.MaxActionEventIDLength ||
			len(receipt.RecommendationRequestID) > domaininteraction.MaxRecommendationRequestIDLength ||
			receipt.Version < 0 ||
			receipt.OccurredAt.IsZero() {
			continue
		} else {
			receipt.OccurredAt = receipt.OccurredAt.UTC()
		}
		if _, exists := seen[receipt.Key]; exists {
			continue
		}
		seen[receipt.Key] = struct{}{}
		receipts = append(receipts, receipt)
	}
	return receipts
}

func actionIdempotencyReceiptForKey(receipts []actionIdempotencyReceipt, key string) (actionIdempotencyReceipt, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return actionIdempotencyReceipt{}, false
	}
	for index := len(receipts) - 1; index >= 0; index-- {
		if receipts[index].Key == key {
			return receipts[index], true
		}
	}
	return actionIdempotencyReceipt{}, false
}

func appendActionIdempotencyReceipt(receipts []actionIdempotencyReceipt, receipt actionIdempotencyReceipt) []actionIdempotencyReceipt {
	receipt.Key = strings.TrimSpace(receipt.Key)
	if receipt.Key == "" || len(receipt.Key) > domaininteraction.MaxIdempotencyKeyLength {
		return receipts
	}
	if _, found := actionIdempotencyReceiptForKey(receipts, receipt.Key); found {
		return receipts
	}
	receipts = append(receipts, receipt)
	if len(receipts) > actionIdempotencyReceiptLimit {
		receipts = append([]actionIdempotencyReceipt(nil), receipts[len(receipts)-actionIdempotencyReceiptLimit:]...)
	}
	return receipts
}

func actionIdempotencyReceiptFromMutation(key string, active bool, version int64, mutation applicationinteraction.ActionMutation) actionIdempotencyReceipt {
	return actionIdempotencyReceipt{
		Key:                     strings.TrimSpace(key),
		Active:                  active,
		EventID:                 strings.TrimSpace(mutation.EventID),
		RecommendationRequestID: strings.TrimSpace(mutation.RecommendationRequestID),
		Version:                 version,
		OccurredAt:              mutation.OccurredAt.UTC(),
	}
}

func actionIdempotencyReceiptFromSnapshot(snapshot domaininteraction.ActionStateSnapshot, handoffConfirmed bool, dependent bool) actionIdempotencyReceipt {
	return actionIdempotencyReceipt{
		Key:                     strings.TrimSpace(snapshot.IdempotencyKey),
		Active:                  snapshot.Active,
		EventID:                 strings.TrimSpace(snapshot.EventID),
		RecommendationRequestID: strings.TrimSpace(snapshot.RecommendationRequestID),
		Version:                 snapshot.Version,
		OccurredAt:              snapshot.OccurredAt.UTC(),
		HandoffConfirmed:        handoffConfirmed,
		Dependent:               dependent,
	}
}

func actionIdempotencyNoEventReceipt(key string, active bool) actionIdempotencyReceipt {
	return actionIdempotencyReceipt{
		Key:              strings.TrimSpace(key),
		Active:           active,
		NoEvent:          true,
		HandoffConfirmed: true,
	}
}

func actionIdempotencyReceiptMatchesState(receipt actionIdempotencyReceipt, state *applicationinteraction.ActionStateResult) bool {
	return state != nil &&
		receipt.Key == strings.TrimSpace(state.IdempotencyKey) &&
		actionIdempotencyReceiptReferencesState(receipt, state)
}

func actionIdempotencyReceiptReferencesState(receipt actionIdempotencyReceipt, state *applicationinteraction.ActionStateResult) bool {
	return state != nil &&
		!receipt.NoEvent &&
		receipt.Active == state.Active &&
		receipt.EventID == strings.TrimSpace(state.EventID) &&
		receipt.Version == state.Version &&
		actionEventTimesEqual(receipt.OccurredAt, state.OccurredAt)
}

func actionIdempotencyReceiptReferencesSnapshot(receipt actionIdempotencyReceipt, snapshot domaininteraction.ActionStateSnapshot) bool {
	return !receipt.NoEvent &&
		receipt.Active == snapshot.Active &&
		receipt.EventID == strings.TrimSpace(snapshot.EventID) &&
		receipt.Version == snapshot.Version &&
		actionEventTimesEqual(receipt.OccurredAt, snapshot.OccurredAt)
}

func removeActionIdempotencyReceiptsForState(receipts []actionIdempotencyReceipt, state *applicationinteraction.ActionStateResult) []actionIdempotencyReceipt {
	if state == nil {
		return receipts
	}
	remaining := make([]actionIdempotencyReceipt, 0, len(receipts))
	for _, receipt := range receipts {
		if actionIdempotencyReceiptReferencesState(receipt, state) {
			continue
		}
		remaining = append(remaining, receipt)
	}
	return remaining
}

func actionStateNeedsHandoff(snapshot domaininteraction.ActionStateSnapshot) bool {
	return snapshot.Exists && strings.TrimSpace(snapshot.EventID) != ""
}

func actionStateHandoffConfirmed(values map[string]string, receipts []actionIdempotencyReceipt, snapshot domaininteraction.ActionStateSnapshot) bool {
	if !actionStateNeedsHandoff(snapshot) {
		return true
	}
	if actionStateHandoffStored(values[actionStateHandoffConfirmedField]) {
		return true
	}
	for _, receipt := range receipts {
		if actionIdempotencyReceiptReferencesSnapshot(receipt, snapshot) && receipt.HandoffConfirmed {
			return true
		}
	}
	return false
}

func actionStateHasHandoffDependency(values map[string]string) bool {
	return actionStateHandoffStored(values[actionStateHandoffDependencyField])
}

func actionIdempotencyReceiptsHaveDependency(receipts []actionIdempotencyReceipt, state *applicationinteraction.ActionStateResult) bool {
	for _, receipt := range receipts {
		if receipt.Dependent && actionIdempotencyReceiptReferencesState(receipt, state) {
			return true
		}
	}
	return false
}

func actionStateSnapshotsMatch(left, right domaininteraction.ActionStateSnapshot) bool {
	return left.Exists == right.Exists &&
		left.Active == right.Active &&
		left.Version == right.Version &&
		strings.TrimSpace(left.EventID) == strings.TrimSpace(right.EventID) &&
		actionEventTimesEqual(left.OccurredAt, right.OccurredAt)
}

func actionStateSnapshotForResult(state *applicationinteraction.ActionStateResult) domaininteraction.ActionStateSnapshot {
	if state == nil {
		return domaininteraction.ActionStateSnapshot{}
	}
	return domaininteraction.ActionStateSnapshot{
		Exists:     true,
		Active:     state.Active,
		Version:    state.Version,
		EventID:    state.EventID,
		OccurredAt: state.OccurredAt,
	}
}

func actionStateMatchesResult(values map[string]string, state *applicationinteraction.ActionStateResult) bool {
	current, found := actionStateSnapshotFromRedis(values)
	return found && actionStateSnapshotsMatch(current, actionStateSnapshotForResult(state))
}

func actionStateHandoffStored(value string) bool {
	value = strings.TrimSpace(value)
	return value == "1" || strings.EqualFold(value, "true")
}

func actionStateHandoffFlag(value bool) int {
	if value {
		return 1
	}
	return 0
}

func actionEventTimesEqual(left, right time.Time) bool {
	return left.UTC().Truncate(time.Microsecond).Equal(right.UTC().Truncate(time.Microsecond))
}

func parseActionVersion(value string) int64 {
	version, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || version < 0 {
		return 0
	}
	return version
}

func maxActionVersion(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func actionStatusValue(active bool) int {
	if active {
		return domaininteraction.ActionStatusActive
	}
	return domaininteraction.ActionStatusCanceled
}

func parseOptionalActionTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return parsed
}

func formatOptionalActionTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func actionStat(ctx context.Context, client redisActionStatReader, counterBaseKey string, counterShardKeys []string, jsonKey string, videoID int64, initialStat *domaininteraction.VideoStat) (*domainfeed.FeedStat, error) {
	stat, _, err := actionStatWithPresence(ctx, client, counterBaseKey, counterShardKeys, jsonKey, videoID, initialStat)
	if err != nil {
		return nil, err
	}
	if stat == nil {
		return &domainfeed.FeedStat{VideoID: videoID}, nil
	}
	return stat, nil
}

func actionStatFromCache(ctx context.Context, client redisActionStatReader, videoID int64) (*domainfeed.FeedStat, bool, error) {
	return actionStatWithPresence(ctx, client, interactionStatCounterBaseKey(videoID), interactionStatCounterShardKeys(videoID), feedStatKey(videoID), videoID, nil)
}

func actionStatWithPresence(ctx context.Context, client redisActionStatReader, counterBaseKey string, counterShardKeys []string, jsonKey string, videoID int64, initialStat *domaininteraction.VideoStat) (*domainfeed.FeedStat, bool, error) {
	stat := &domainfeed.FeedStat{VideoID: videoID}
	found := false
	values, err := client.HGetAll(ctx, counterBaseKey).Result()
	if err != nil {
		return nil, false, err
	}
	if len(values) > 0 {
		applyActionStatFields(stat, values)
		found = true
	} else {
		fallbackStat, ok, err := actionStatFallback(ctx, client, jsonKey, videoID, initialStat)
		if err != nil {
			return nil, false, err
		}
		if ok {
			stat = fallbackStat
			found = true
		}
	}
	shardFound, err := applyActionStatShardDeltas(ctx, client, stat, counterShardKeys)
	if err != nil {
		return nil, false, err
	}
	found = found || shardFound
	if !found {
		return nil, false, nil
	}
	return stat, true, nil
}

func actionStatFallback(ctx context.Context, client redisActionStatReader, jsonKey string, videoID int64, initialStat *domaininteraction.VideoStat) (*domainfeed.FeedStat, bool, error) {
	stat := &domainfeed.FeedStat{VideoID: videoID}
	content, err := client.Get(ctx, jsonKey).Bytes()
	if err == redis.Nil {
		if initialStat != nil {
			stat.LikeCount = initialStat.LikeCount
			stat.CommentCount = initialStat.CommentCount
			stat.FavoriteCount = initialStat.FavoriteCount
			return stat, true, nil
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(content, stat); err != nil {
		return nil, false, nil
	}
	if stat.VideoID <= 0 {
		stat.VideoID = videoID
	}
	return stat, true, nil
}

func actionStatBaseInit(videoID int64, initialStat *domaininteraction.VideoStat) *domaininteraction.VideoStat {
	if initialStat != nil {
		return initialStat
	}
	return &domaininteraction.VideoStat{VideoID: videoID}
}

func queueActionStatBaseInit(ctx context.Context, pipe redis.Pipeliner, counterBaseKey string, initialStat *domaininteraction.VideoStat) {
	stat := &domaininteraction.VideoStat{}
	if initialStat != nil {
		stat = initialStat
	}
	pipe.HSetNX(ctx, counterBaseKey, "like_count", stat.LikeCount)
	pipe.HSetNX(ctx, counterBaseKey, "comment_count", stat.CommentCount)
	pipe.HSetNX(ctx, counterBaseKey, "favorite_count", stat.FavoriteCount)
}

func setActionStatJSON(ctx context.Context, client redisActionStatWriter, jsonKey string, stat *domainfeed.FeedStat) error {
	content, err := json.Marshal(stat)
	if err != nil {
		return err
	}
	return client.Set(ctx, jsonKey, content, actionStatJSONTTL).Err()
}

func applyActionStatShardDeltas(ctx context.Context, client redisActionStatReader, stat *domainfeed.FeedStat, shardKeys []string) (bool, error) {
	if stat == nil || len(shardKeys) == 0 {
		return false, nil
	}

	shardValues, err := loadActionStatShardValues(ctx, client, shardKeys)
	if err != nil {
		return false, err
	}
	found := false
	likeDelta := 0
	favoriteDelta := 0
	for _, values := range shardValues {
		if len(values) > 0 {
			found = true
		}
		likeDelta += actionStatFieldInt(values, "like_count")
		favoriteDelta += actionStatFieldInt(values, "favorite_count")
	}
	stat.LikeCount = clampRedisCount(stat.LikeCount + likeDelta)
	stat.FavoriteCount = clampRedisCount(stat.FavoriteCount + favoriteDelta)
	return found, nil
}

func loadActionStatShardValues(ctx context.Context, client redisActionStatReader, shardKeys []string) ([]map[string]string, error) {
	type pipelineProvider interface {
		Pipeline() redis.Pipeliner
	}

	if provider, ok := client.(pipelineProvider); ok {
		pipe := provider.Pipeline()
		cmds := make([]*redis.MapStringStringCmd, 0, len(shardKeys))
		for _, key := range shardKeys {
			cmds = append(cmds, pipe.HGetAll(ctx, key))
		}
		if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
			return nil, err
		}
		values := make([]map[string]string, 0, len(cmds))
		for _, cmd := range cmds {
			value, err := cmd.Result()
			if err != nil && err != redis.Nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	}

	values := make([]map[string]string, 0, len(shardKeys))
	for _, key := range shardKeys {
		value, err := client.HGetAll(ctx, key).Result()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func applyActionStatFields(stat *domainfeed.FeedStat, values map[string]string) {
	if stat == nil {
		return
	}
	stat.LikeCount = actionStatFieldInt(values, "like_count")
	stat.CommentCount = actionStatFieldInt(values, "comment_count")
	stat.FavoriteCount = actionStatFieldInt(values, "favorite_count")
}

func actionStatFieldInt(values map[string]string, field string) int {
	value, _ := strconv.Atoi(values[field])
	return value
}

func cacheKeys(videoIDs []int64, build func(int64) string) []string {
	keys := make([]string, 0, len(videoIDs))
	for _, videoID := range videoIDs {
		keys = append(keys, build(videoID))
	}
	return keys
}

func feedCardKey(videoID int64) string {
	return fmt.Sprintf("video:card:v2:%d", videoID)
}

func feedStatKey(videoID int64) string {
	return fmt.Sprintf("video:stat:v1:%d", videoID)
}

func followingInboxKey(userID int64) string {
	return fmt.Sprintf("feed:following:inbox:v1:%d", userID)
}

func followingAuthorOutboxKey(authorID int64) string {
	return fmt.Sprintf("feed:following:author:v1:%d", authorID)
}

func followingIndexScore(publishedAt time.Time, videoID int64) float64 {
	return float64(publishedAt.UTC().Unix()*1000000 + videoID%1000000)
}

func followingIndexMember(videoID int64, authorID int64, publishedAt time.Time) string {
	return fmt.Sprintf("%d:%d:%s", videoID, authorID, publishedAt.UTC().Format(time.RFC3339Nano))
}

func feedPageItemFromFollowingMember(member string) (*domainfeed.FeedPageItem, bool) {
	parts := strings.SplitN(member, ":", 3)
	if len(parts) != 2 && len(parts) != 3 {
		return nil, false
	}
	videoID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || videoID <= 0 {
		return nil, false
	}
	authorID := int64(0)
	publishedAtIndex := 1
	if len(parts) == 3 {
		authorID, _ = strconv.ParseInt(parts[1], 10, 64)
		publishedAtIndex = 2
	}
	publishedAt, err := time.Parse(time.RFC3339Nano, parts[publishedAtIndex])
	if err != nil || publishedAt.IsZero() {
		return nil, false
	}
	return &domainfeed.FeedPageItem{
		VideoID:     videoID,
		AuthorID:    authorID,
		PublishedAt: publishedAt,
	}, true
}

func int64Set(values []int64) map[int64]struct{} {
	set := map[int64]struct{}{}
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func sortFeedPageItemsByTimeline(items []*domainfeed.FeedPageItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].PublishedAt.Equal(items[j].PublishedAt) {
			return items[i].VideoID > items[j].VideoID
		}
		return items[i].PublishedAt.After(items[j].PublishedAt)
	})
}

func interactionActionKey(userID int64, videoID int64, actionType string) string {
	return fmt.Sprintf("interaction:action:v1:%d:%d:%s", userID, videoID, strings.ToLower(actionType))
}

func interactionStatCounterKey(videoID int64) string {
	return fmt.Sprintf("video:stat:counter:v1:%d", videoID)
}

func interactionStatCounterBaseKey(videoID int64) string {
	return fmt.Sprintf("%s:base", interactionStatCounterKey(videoID))
}

func interactionStatCounterShardKey(videoID int64, shard int) string {
	return fmt.Sprintf("%s:shard:%02d", interactionStatCounterKey(videoID), shard)
}

func interactionStatCounterShardKeys(videoID int64) []string {
	keys := make([]string, 0, actionStatCounterShardCount)
	for shard := 0; shard < actionStatCounterShardCount; shard++ {
		keys = append(keys, interactionStatCounterShardKey(videoID, shard))
	}
	return keys
}

func interactionStatCounterShardIndex(userID int64) int {
	if userID <= 0 {
		return 0
	}
	return int(userID % actionStatCounterShardCount)
}

func interactionStatField(actionType string) string {
	if actionType == domaininteraction.ActionTypeLike {
		return "like_count"
	}
	return "favorite_count"
}

func clampRedisCount(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func hotMinuteKey(at time.Time) string {
	return fmt.Sprintf("feed:hot:minute:v1:%s", at.UTC().Truncate(time.Minute).Format("200601021504"))
}

func hotWindowKey(windowEnd time.Time) string {
	return fmt.Sprintf("feed:hot:window:v1:%d", windowEnd.UTC().Truncate(time.Minute).Unix())
}

func hotWindowMinuteKeys(windowEnd time.Time) []string {
	keys := make([]string, 0, hotWindowMinutes)
	for index := hotWindowMinutes - 1; index >= 0; index-- {
		keys = append(keys, hotMinuteKey(windowEnd.Add(-time.Duration(index)*time.Minute)))
	}
	return keys
}

func hotRankMember(videoID int64) string {
	return fmt.Sprintf("%020d", videoID)
}

func hotRankVideoID(member string) (int64, bool) {
	value := strings.TrimLeft(member, "0")
	if value == "" {
		return 0, false
	}
	videoID, err := strconv.ParseInt(value, 10, 64)
	return videoID, err == nil && videoID > 0
}

func cacheValueBytes(value any) ([]byte, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case string:
		return []byte(typed), true
	case []byte:
		return typed, true
	default:
		return nil, false
	}
}
