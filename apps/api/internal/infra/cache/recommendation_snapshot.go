package infracache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	recommendationSnapshotMaxCandidates = 500
	recommendationSnapshotMaxBytes      = 512 * 1024
	recommendationSnapshotMaxSessions   = 20
)

const createRecommendationSnapshotScript = `
local existingID = redis.call('GET', KEYS[1])
if existingID then
  local existing = redis.call('GET', ARGV[3] .. existingID)
  if existing then
    return {0, existing}
  end
  redis.call('DEL', KEYS[1])
end

if redis.call('SET', KEYS[2], ARGV[1], 'NX', 'PX', ARGV[2]) then
  redis.call('SET', KEYS[1], ARGV[4], 'PX', ARGV[2])
  return {1, ARGV[1]}
end

local existing = redis.call('GET', KEYS[2])
if existing then
  redis.call('SET', KEYS[1], ARGV[4], 'PX', ARGV[2])
  return {0, existing}
end
return {2, ''}
`

// RecommendationSnapshotStore is a bounded Redis implementation of the
// recommendation session store. Snapshot keys use an opaque server-derived ID;
// the user index only enforces a short active-session bound.
type RecommendationSnapshotStore struct {
	client redis.Cmdable
}

func NewRecommendationSnapshotStore(client redis.Cmdable) *RecommendationSnapshotStore {
	if client == nil {
		return nil
	}
	return &RecommendationSnapshotStore{client: client}
}

func (s *RecommendationSnapshotStore) CreateSnapshot(ctx context.Context, snapshot *applicationrecommendation.Snapshot, ttl time.Duration) (*applicationrecommendation.Snapshot, bool, error) {
	if s == nil || s.client == nil || snapshot == nil || snapshot.ID == "" || snapshot.UserID <= 0 ||
		len(snapshot.Candidates) > recommendationSnapshotMaxCandidates || ttl <= 0 {
		return nil, false, applicationrecommendation.ErrSnapshotUnavailable
	}
	ttl, err := snapshotCreateTTL(snapshot, ttl, time.Now().UTC())
	if err != nil {
		return nil, false, err
	}
	content, err := json.Marshal(snapshot)
	if err != nil || len(content) > recommendationSnapshotMaxBytes {
		return nil, false, applicationrecommendation.ErrSnapshotUnavailable
	}
	result, err := s.client.Eval(
		ctx,
		createRecommendationSnapshotScript,
		[]string{recommendationSnapshotRequestKey(snapshot.UserID, snapshot.Scene, snapshot.RequestID), recommendationSnapshotKey(snapshot.ID)},
		string(content),
		ceilMilliseconds(ttl),
		recommendationSnapshotKeyPrefix(),
		snapshot.ID,
	).Slice()
	if err != nil || len(result) != 2 {
		if err != nil {
			return nil, false, err
		}
		return nil, false, applicationrecommendation.ErrSnapshotUnavailable
	}
	created, ok := snapshotCreateResult(result[0])
	if !ok {
		return nil, false, applicationrecommendation.ErrSnapshotUnavailable
	}
	stored, err := snapshotFromRedisContent(result[1], "")
	if err != nil {
		return nil, false, err
	}
	if stored.UserID != snapshot.UserID || stored.Scene != snapshot.Scene || stored.RequestID != snapshot.RequestID {
		return nil, false, applicationrecommendation.ErrSnapshotUnavailable
	}
	storedTTL, err := snapshotRemainingTTL(stored, time.Now().UTC())
	if err != nil {
		return nil, false, err
	}
	if err := s.client.PExpire(ctx, recommendationSnapshotRequestKey(stored.UserID, stored.Scene, stored.RequestID), storedTTL).Err(); err != nil {
		inframetrics.ObserveRecommendationSnapshot("maintenance_failure")
	}
	if err := s.maintainUserIndex(ctx, stored); err != nil {
		inframetrics.ObserveRecommendationSnapshot("maintenance_failure")
	}
	return stored, created, nil
}

func (s *RecommendationSnapshotStore) maintainUserIndex(ctx context.Context, snapshot *applicationrecommendation.Snapshot) error {
	indexKey := recommendationSnapshotUserKey(snapshot.UserID)
	now := time.Now().UTC()
	if _, err := snapshotRemainingTTL(snapshot, now); err != nil {
		return err
	}
	expiresAt := ceilUnixMilliseconds(snapshot.ExpiresAt.UTC())
	if err := s.client.ZAdd(ctx, indexKey, redis.Z{Score: float64(expiresAt), Member: snapshot.ID}).Err(); err != nil {
		return err
	}
	if err := s.client.ZRemRangeByScore(ctx, indexKey, "-inf", fmt.Sprintf("%d", now.UnixMilli()-1)).Err(); err != nil {
		return err
	}
	evicted, err := s.client.ZRange(ctx, indexKey, 0, -recommendationSnapshotMaxSessions-1).Result()
	if err != nil {
		return err
	}
	if err := s.client.ZRemRangeByRank(ctx, indexKey, 0, -recommendationSnapshotMaxSessions-1).Err(); err != nil {
		return err
	}
	if len(evicted) > 0 {
		keys := make([]string, 0, len(evicted))
		for _, id := range evicted {
			keys = append(keys, recommendationSnapshotKey(id))
		}
		if err := s.client.Del(ctx, keys...).Err(); err != nil {
			return err
		}
	}
	latest, err := s.client.ZRevRangeWithScores(ctx, indexKey, 0, 0).Result()
	if err != nil {
		return err
	}
	if len(latest) == 0 {
		return s.client.Del(ctx, indexKey).Err()
	}
	indexTTL := snapshotIndexTTL(int64(latest[0].Score), time.Now().UTC())
	if indexTTL <= 0 {
		return s.client.Del(ctx, indexKey).Err()
	}
	return s.client.PExpire(ctx, indexKey, indexTTL).Err()
}

func snapshotRemainingTTL(snapshot *applicationrecommendation.Snapshot, now time.Time) (time.Duration, error) {
	if snapshot == nil || snapshot.ExpiresAt.IsZero() {
		return 0, applicationrecommendation.ErrSnapshotUnavailable
	}
	remaining := snapshot.ExpiresAt.UTC().Sub(now.UTC())
	if remaining <= 0 {
		return 0, applicationrecommendation.ErrSnapshotUnavailable
	}
	return remaining, nil
}

func snapshotCreateTTL(snapshot *applicationrecommendation.Snapshot, requestedTTL time.Duration, now time.Time) (time.Duration, error) {
	if requestedTTL <= 0 {
		return 0, applicationrecommendation.ErrSnapshotUnavailable
	}
	return snapshotRemainingTTL(snapshot, now)
}

func snapshotIndexTTL(latestExpiryMillis int64, now time.Time) time.Duration {
	return time.UnixMilli(latestExpiryMillis).Sub(now.UTC())
}

func ceilUnixMilliseconds(value time.Time) int64 {
	nanos := value.UTC().UnixNano()
	milliseconds := nanos / int64(time.Millisecond)
	if nanos%int64(time.Millisecond) != 0 {
		milliseconds++
	}
	return milliseconds
}

func ceilMilliseconds(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	milliseconds := int64(value / time.Millisecond)
	if value%time.Millisecond != 0 {
		milliseconds++
	}
	return milliseconds
}

func (s *RecommendationSnapshotStore) LoadSnapshot(ctx context.Context, snapshotID string) (*applicationrecommendation.Snapshot, bool, error) {
	if s == nil || s.client == nil || snapshotID == "" {
		return nil, false, applicationrecommendation.ErrSnapshotUnavailable
	}
	content, err := s.client.Get(ctx, recommendationSnapshotKey(snapshotID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(content) == 0 || len(content) > recommendationSnapshotMaxBytes {
		return nil, false, applicationrecommendation.ErrSnapshotUnavailable
	}
	snapshot, err := snapshotFromRedisContent(content, snapshotID)
	if err != nil {
		return nil, false, err
	}
	return snapshot, true, nil
}

func (s *RecommendationSnapshotStore) LoadSnapshotForRequest(ctx context.Context, userID int64, scene string, requestID string) (*applicationrecommendation.Snapshot, bool, error) {
	if s == nil || s.client == nil || userID <= 0 || scene == "" || requestID == "" {
		return nil, false, applicationrecommendation.ErrSnapshotUnavailable
	}
	snapshotID, err := s.client.Get(ctx, recommendationSnapshotRequestKey(userID, scene, requestID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	snapshot, found, err := s.LoadSnapshot(ctx, snapshotID)
	if err != nil || !found {
		if !found {
			_ = s.client.Del(ctx, recommendationSnapshotRequestKey(userID, scene, requestID)).Err()
		}
		return snapshot, found, err
	}
	if snapshot.UserID != userID || snapshot.Scene != scene || snapshot.RequestID != requestID {
		return nil, false, applicationrecommendation.ErrSnapshotUnavailable
	}
	return snapshot, true, nil
}

func snapshotCreateResult(value any) (bool, bool) {
	switch value := value.(type) {
	case int64:
		return value == 1, value == 0 || value == 1
	case string:
		return value == "1", value == "0" || value == "1"
	case []byte:
		return string(value) == "1", string(value) == "0" || string(value) == "1"
	default:
		return false, false
	}
}

func snapshotFromRedisContent(content any, snapshotID string) (*applicationrecommendation.Snapshot, error) {
	var encoded []byte
	switch value := content.(type) {
	case string:
		encoded = []byte(value)
	case []byte:
		encoded = value
	default:
		return nil, applicationrecommendation.ErrSnapshotUnavailable
	}
	if len(encoded) == 0 || len(encoded) > recommendationSnapshotMaxBytes {
		return nil, applicationrecommendation.ErrSnapshotUnavailable
	}
	var snapshot applicationrecommendation.Snapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil || snapshot.ID == "" || (snapshotID != "" && snapshot.ID != snapshotID) ||
		len(snapshot.Candidates) > recommendationSnapshotMaxCandidates {
		return nil, applicationrecommendation.ErrSnapshotUnavailable
	}
	return snapshot.Clone(), nil
}

func recommendationSnapshotKey(snapshotID string) string {
	return recommendationSnapshotKeyPrefix() + snapshotID
}

func recommendationSnapshotKeyPrefix() string {
	return "recommendation:snapshot:v1:"
}

func recommendationSnapshotUserKey(userID int64) string {
	return fmt.Sprintf("recommendation:snapshot:v1:user:%d", userID)
}

func recommendationSnapshotRequestKey(userID int64, scene string, requestID string) string {
	return fmt.Sprintf("recommendation:snapshot:v1:request:%d:%s:%s", userID, scene, requestID)
}
