package infracache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestFollowingIndexFallsBackForStaleUnfollowedInboxAuthor(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	ctx := context.Background()
	base := time.Date(2026, 8, 8, 7, 0, 0, 0, time.UTC)
	if err := cache.AddInboxItems(ctx, 200, []int64{42}, &domainfeed.FeedPageItem{
		VideoID:     1,
		AuthorID:    200,
		PublishedAt: base,
	}, 100); err != nil {
		t.Fatalf("add followed inbox item: %v", err)
	}
	if err := cache.AddInboxItems(ctx, 300, []int64{42}, &domainfeed.FeedPageItem{
		VideoID:     2,
		AuthorID:    300,
		PublishedAt: base.Add(time.Minute),
	}, 100); err != nil {
		t.Fatalf("add stale inbox item: %v", err)
	}

	items, ok, err := cache.ListFollowingIndexPage(ctx, 42, []int64{200}, nil, nil, 10)
	if err != nil {
		t.Fatalf("list following index: %v", err)
	}
	if ok || items != nil {
		t.Fatalf("stale inbox should force truth-source fallback: ok=%t items=%+v", ok, items)
	}
}

type actionStatFakeRedis struct {
	hashes     map[string]map[string]string
	hashErrors map[string]error
	values     map[string]string
}

type actionHandoffRepositoryStub struct{}

func (*actionHandoffRepositoryStub) PersistAcceptedActionEvent(context.Context, *domaininteraction.AcceptedActionEvent) error {
	return nil
}

func (*actionHandoffRepositoryStub) GetVideoStat(_ context.Context, videoID int64) (*domaininteraction.VideoStat, error) {
	return &domaininteraction.VideoStat{VideoID: videoID}, nil
}

func (*actionHandoffRepositoryStub) GetActionState(context.Context, int64, int64, string) (*domaininteraction.ActionStateSnapshot, error) {
	return &domaininteraction.ActionStateSnapshot{}, nil
}

func (*actionHandoffRepositoryStub) GetVideoAuthorID(context.Context, int64) (int64, error) {
	return 0, nil
}

func (*actionHandoffRepositoryStub) GetUserProfile(context.Context, int64) (*domaininteraction.UserProfile, error) {
	return nil, nil
}

func (*actionHandoffRepositoryStub) SetAction(context.Context, int64, int64, string, bool, string) (*domaininteraction.Action, int, int, error) {
	return nil, 0, 0, nil
}

func (*actionHandoffRepositoryStub) SetActionWithAcceptedEvent(context.Context, *domaininteraction.AcceptedActionEvent) (*domaininteraction.Action, int, int, error) {
	return nil, 0, 0, nil
}

func (*actionHandoffRepositoryStub) CreateComment(context.Context, *domaininteraction.Comment) (*domaininteraction.Comment, int, int, error) {
	return nil, 0, 0, nil
}

func (*actionHandoffRepositoryStub) FindCommentByUserAndIdempotencyKey(context.Context, int64, string) (*domaininteraction.Comment, int, error) {
	return nil, 0, nil
}

func (*actionHandoffRepositoryStub) ListComments(context.Context, int64, *domaininteraction.CommentCursor, int) ([]*domaininteraction.Comment, error) {
	return nil, nil
}

func (*actionHandoffRepositoryStub) DeleteComment(context.Context, int64, int64, string) (*domaininteraction.Comment, int, int, error) {
	return nil, 0, 0, nil
}

type actionEventPublisherStub struct {
	events []*applicationinteraction.ActionChangedEvent
}

func (p *actionEventPublisherStub) PublishActionChanged(_ context.Context, event *applicationinteraction.ActionChangedEvent) error {
	cloned := *event
	p.events = append(p.events, &cloned)
	return nil
}

func newActionStatFakeRedis() *actionStatFakeRedis {
	return &actionStatFakeRedis{
		hashes:     map[string]map[string]string{},
		hashErrors: map[string]error{},
		values:     map[string]string{},
	}
}

func (r *actionStatFakeRedis) HGetAll(ctx context.Context, key string) *redis.MapStringStringCmd {
	if err := r.hashErrors[key]; err != nil {
		return redis.NewMapStringStringResult(nil, err)
	}
	values := r.hashes[key]
	if values == nil {
		values = map[string]string{}
	}
	return redis.NewMapStringStringResult(values, nil)
}

func (r *actionStatFakeRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	value, ok := r.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(value, nil)
}

func (r *actionStatFakeRedis) Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd {
	switch typed := value.(type) {
	case string:
		r.values[key] = typed
	case []byte:
		r.values[key] = string(typed)
	default:
		content, _ := json.Marshal(typed)
		r.values[key] = string(content)
	}
	return redis.NewStatusResult("OK", nil)
}

func (r *actionStatFakeRedis) MGet(ctx context.Context, keys ...string) *redis.SliceCmd {
	values := make([]any, 0, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values = append(values, value)
			continue
		}
		values = append(values, nil)
	}
	return redis.NewSliceResult(values, nil)
}

func TestActionStatAggregatesCounterShards(t *testing.T) {
	ctx := context.Background()
	videoID := int64(1001)
	redisClient := newActionStatFakeRedis()
	redisClient.hashes[interactionStatCounterBaseKey(videoID)] = map[string]string{
		"like_count":     "10",
		"comment_count":  "3",
		"favorite_count": "4",
	}
	redisClient.hashes[interactionStatCounterShardKey(videoID, interactionStatCounterShardIndex(42))] = map[string]string{
		"like_count":     "1",
		"favorite_count": "1",
	}
	redisClient.hashes[interactionStatCounterShardKey(videoID, interactionStatCounterShardIndex(43))] = map[string]string{
		"like_count": "-1",
	}
	redisClient.hashes[interactionStatCounterShardKey(videoID, interactionStatCounterShardIndex(44))] = map[string]string{
		"like_count": "1",
	}

	stat, err := actionStat(ctx, redisClient, interactionStatCounterBaseKey(videoID), interactionStatCounterShardKeys(videoID), feedStatKey(videoID), videoID, nil)
	if err != nil {
		t.Fatalf("actionStat: %v", err)
	}
	if stat.LikeCount != 11 || stat.FavoriteCount != 5 || stat.CommentCount != 3 {
		t.Fatalf("unexpected stat: %+v", stat)
	}
}

func TestActionStatFallsBackToInitialStat(t *testing.T) {
	ctx := context.Background()
	videoID := int64(1002)
	redisClient := newActionStatFakeRedis()
	initial := &domaininteraction.VideoStat{
		VideoID:       videoID,
		LikeCount:     7,
		CommentCount:  2,
		FavoriteCount: 1,
	}
	redisClient.hashes[interactionStatCounterShardKey(videoID, interactionStatCounterShardIndex(42))] = map[string]string{
		"like_count":     "1",
		"favorite_count": "-1",
	}

	stat, err := actionStat(ctx, redisClient, interactionStatCounterBaseKey(videoID), interactionStatCounterShardKeys(videoID), feedStatKey(videoID), videoID, initial)
	if err != nil {
		t.Fatalf("actionStat: %v", err)
	}
	if stat.LikeCount != 8 || stat.FavoriteCount != 0 || stat.CommentCount != 2 {
		t.Fatalf("unexpected stat: %+v", stat)
	}
}

func TestGetStatsReadsShardedCountersOnJSONMiss(t *testing.T) {
	ctx := context.Background()
	videoID := int64(1003)
	redisClient := newActionStatFakeRedis()
	redisClient.hashes[interactionStatCounterBaseKey(videoID)] = map[string]string{
		"like_count":     "2",
		"comment_count":  "1",
		"favorite_count": "0",
	}
	redisClient.hashes[interactionStatCounterShardKey(videoID, interactionStatCounterShardIndex(42))] = map[string]string{
		"like_count":     "1",
		"favorite_count": "1",
	}
	stats, err := getStats(ctx, redisClient, []int64{videoID})
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	stat := stats[videoID]
	if stat == nil || stat.LikeCount != 3 || stat.FavoriteCount != 1 || stat.CommentCount != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if _, ok := redisClient.values[feedStatKey(videoID)]; !ok {
		t.Fatalf("expected sharded stat to be written back to JSON cache")
	}
}

func TestSetVideoStatWritesJSONCache(t *testing.T) {
	ctx := context.Background()
	videoID := int64(1005)
	redisClient := newActionStatFakeRedis()

	err := setActionStatJSON(ctx, redisClient, feedStatKey(videoID), videoStatToFeedStat(&domaininteraction.VideoStat{
		VideoID:       videoID,
		LikeCount:     2,
		CommentCount:  3,
		FavoriteCount: 1,
	}))
	if err != nil {
		t.Fatalf("SetVideoStat: %v", err)
	}

	stats, err := getStats(ctx, redisClient, []int64{videoID})
	if err != nil {
		t.Fatalf("getStats: %v", err)
	}
	stat := stats[videoID]
	if stat == nil || stat.LikeCount != 2 || stat.CommentCount != 3 || stat.FavoriteCount != 1 {
		t.Fatalf("unexpected stat: %+v", stat)
	}
}

func TestActionStatBaseInitUsesInitialStat(t *testing.T) {
	videoID := int64(1004)
	initial := &domaininteraction.VideoStat{
		VideoID:       videoID,
		LikeCount:     1,
		CommentCount:  1,
		FavoriteCount: 1,
	}

	stat := actionStatBaseInit(videoID, initial)
	if stat != initial {
		t.Fatalf("unexpected stat: %+v", stat)
	}
}

func TestCompleteActionStateResultReturnsCommittedMutationOnCountReadFailure(t *testing.T) {
	videoID := int64(1006)
	countReadErr := errors.New("count read failed")
	redisClient := newActionStatFakeRedis()
	counterBaseKey := interactionStatCounterBaseKey(videoID)
	redisClient.hashErrors[counterBaseKey] = countReadErr
	committed := &applicationinteraction.ActionStateResult{
		UserID:        42,
		VideoID:       videoID,
		ActionType:    domaininteraction.ActionTypeLike,
		Active:        true,
		Delta:         1,
		Version:       3,
		EventID:       "event-3",
		ShouldPublish: true,
		CanRollback:   true,
	}

	result, err := completeActionStateResult(
		context.Background(),
		redisClient,
		committed,
		counterBaseKey,
		feedStatKey(videoID),
		&domaininteraction.VideoStat{VideoID: videoID},
	)
	if !errors.Is(err, countReadErr) {
		t.Fatalf("expected count read failure, got %v", err)
	}
	if result != committed || result.Version != 3 || !result.CanRollback {
		t.Fatalf("committed mutation metadata was lost: %+v", result)
	}
}

func TestActionIdempotencyReceiptsAreBoundedAndPayloadBound(t *testing.T) {
	receipts := []actionIdempotencyReceipt{}
	for index := 0; index < actionIdempotencyReceiptLimit+1; index++ {
		receipts = appendActionIdempotencyReceipt(receipts, actionIdempotencyNoEventReceipt(fmt.Sprintf("receipt-%d", index), index%2 == 0))
	}
	if len(receipts) != actionIdempotencyReceiptLimit {
		t.Fatalf("receipt count = %d, want %d", len(receipts), actionIdempotencyReceiptLimit)
	}
	if _, found := actionIdempotencyReceiptForKey(receipts, "receipt-0"); found {
		t.Fatalf("oldest receipt was not evicted: %#v", receipts)
	}
	receipt, found := actionIdempotencyReceiptForKey(receipts, "receipt-32")
	if !found || !receipt.Active {
		t.Fatalf("newest receipt lost its payload: %#v", receipts)
	}

	encoded, err := json.Marshal(receipts)
	if err != nil {
		t.Fatal(err)
	}
	decoded := actionIdempotencyReceiptsFromRedis(string(encoded))
	if len(decoded) != actionIdempotencyReceiptLimit {
		t.Fatalf("stored receipt count = %d, want %d", len(decoded), actionIdempotencyReceiptLimit)
	}
	if replay, found := actionIdempotencyReceiptForKey(decoded, "receipt-32"); !found || !replay.Active {
		t.Fatalf("same idempotency key did not retain its active payload: %#v", decoded)
	}
}

func TestActionReceiptCrashBeforePublishReplay(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	mutation := actionReceiptTestMutation("event-crash")

	accepted := setActionReceiptTestState(t, cache, "crash-key", true, mutation)
	replayed := setActionReceiptTestState(t, cache, "crash-key", true, actionReceiptTestMutation("event-retry"))

	if !replayed.ShouldPublish || replayed.CanRollback {
		t.Fatalf("pending receipt did not request recoverable publication: %#v", replayed)
	}
	if replayed.EventID != accepted.EventID ||
		replayed.Version != accepted.Version ||
		!replayed.OccurredAt.Equal(accepted.OccurredAt) {
		t.Fatalf("replay did not retain the accepted event: got=%#v want=%#v", replayed, accepted)
	}
}

func TestActionReceiptPendingHandoffCanRollbackAndRetry(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	accepted := setActionReceiptTestState(t, cache, "rollback-key", true, actionReceiptTestMutation("event-rollback"))

	rolledBack, err := cache.RollbackActionState(context.Background(), accepted)
	if err != nil || !rolledBack {
		t.Fatalf("rollback accepted action: rolledBack=%t err=%v", rolledBack, err)
	}

	retried := setActionReceiptTestState(t, cache, "rollback-key", true, actionReceiptTestMutation("event-retry"))
	if !retried.ShouldPublish || retried.EventID != "event-retry" {
		t.Fatalf("rollback left an unrecoverable receipt: %#v", retried)
	}
}

func TestActionReceiptUnkeyedNoOpRecoversPendingHandoff(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	accepted := setActionReceiptTestState(t, cache, "crash-key", true, actionReceiptTestMutation("event-crash"))

	recovered := setActionReceiptTestState(t, cache, "", true, actionReceiptTestMutation("event-unkeyed-retry"))
	if !recovered.ShouldPublish || recovered.EventID != accepted.EventID || recovered.Version != accepted.Version {
		t.Fatalf("unkeyed no-op did not recover pending event: got=%#v want=%#v", recovered, accepted)
	}
	if err := cache.ConfirmActionStateHandoff(context.Background(), recovered); err != nil {
		t.Fatalf("confirm recovered handoff: %v", err)
	}

	replayed := setActionReceiptTestState(t, cache, "", true, actionReceiptTestMutation("event-after-confirm"))
	if replayed.ShouldPublish || replayed.EventID != accepted.EventID || replayed.Version != accepted.Version {
		t.Fatalf("confirmed unkeyed no-op requested another handoff: %#v", replayed)
	}
}

func TestAsyncActionServiceRecoversCrashWithUnkeyedNoOp(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	accepted := setActionReceiptTestState(t, cache, "crash-key", true, actionReceiptTestMutation("event-crash"))
	publisher := &actionEventPublisherStub{}
	service := applicationinteraction.New(
		&actionHandoffRepositoryStub{},
		applicationinteraction.WithAsyncActionPipeline(cache, publisher),
	)

	result, err := service.Like(context.Background(), 42, 1007, "")
	if err != nil {
		t.Fatalf("recover unkeyed no-op: %v", err)
	}
	if !result.Active || result.LikeCount != 1 || len(publisher.events) != 1 {
		t.Fatalf("unkeyed no-op returned without publishing the pending event: result=%+v events=%+v", result, publisher.events)
	}
	if event := publisher.events[0]; event.EventID != accepted.EventID || event.Version != accepted.Version {
		t.Fatalf("recovery published a new or mismatched event: got=%+v want=%+v", event, accepted)
	}

	replayed := setActionReceiptTestState(t, cache, "", true, actionReceiptTestMutation("event-after-confirm"))
	if replayed.ShouldPublish {
		t.Fatalf("durably handed-off state requested another publication: %#v", replayed)
	}
}

func TestActionReceiptRecognizesDurableMicrosecondBaseline(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	accepted := setActionReceiptTestState(t, cache, "pending-key", true, applicationinteraction.ActionMutation{
		EventID:    "event-with-nanoseconds",
		OccurredAt: time.Date(2026, time.July, 27, 9, 0, 0, 123456789, time.UTC),
	})
	baseline := &domaininteraction.ActionStateSnapshot{
		Exists:     true,
		Active:     true,
		Version:    accepted.Version,
		EventID:    accepted.EventID,
		OccurredAt: accepted.OccurredAt.Truncate(time.Microsecond),
	}

	state, err := cache.SetActionState(
		context.Background(),
		42,
		1007,
		domaininteraction.ActionTypeLike,
		true,
		"",
		&domaininteraction.VideoStat{VideoID: 1007},
		baseline,
		actionReceiptTestMutation("event-unused"),
	)
	if err != nil {
		t.Fatalf("recognize durable baseline: %v", err)
	}
	if state.ShouldPublish {
		t.Fatalf("durable microsecond baseline requested duplicate handoff: %#v", state)
	}
	confirmed, err := cache.client.HGet(
		context.Background(),
		interactionActionKey(42, 1007, domaininteraction.ActionTypeLike),
		actionStateHandoffConfirmedField,
	).Result()
	if err != nil || confirmed != "1" {
		t.Fatalf("durable baseline was not recorded in Redis: value=%q err=%v", confirmed, err)
	}
}

func TestActionReceiptDifferentKeyNoOpWaitsForHandoff(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	accepted := setActionReceiptTestState(t, cache, "original-key", true, actionReceiptTestMutation("event-original"))

	dependent := setActionReceiptTestState(t, cache, "dependent-key", true, actionReceiptTestMutation("event-dependent"))
	if !dependent.ShouldPublish || dependent.EventID != accepted.EventID ||
		dependent.Version != accepted.Version ||
		dependent.IdempotencyKey != accepted.IdempotencyKey {
		t.Fatalf("different-key no-op did not recover the stable pending event: got=%#v want=%#v", dependent, accepted)
	}
	rawReceipts, err := cache.client.HGet(
		context.Background(),
		interactionActionKey(42, 1007, domaininteraction.ActionTypeLike),
		actionIdempotencyReceiptsField,
	).Result()
	if err != nil {
		t.Fatalf("read dependent receipt: %v", err)
	}
	receipt, found := actionIdempotencyReceiptForKey(actionIdempotencyReceiptsFromRedis(rawReceipts), "dependent-key")
	if !found || receipt.NoEvent || !receipt.Dependent || receipt.HandoffConfirmed {
		t.Fatalf("different-key no-op did not retain an unconfirmed dependency: %#v", receipt)
	}

	if err := cache.ConfirmActionStateHandoff(context.Background(), dependent); err != nil {
		t.Fatalf("confirm dependent handoff: %v", err)
	}
	replayed := setActionReceiptTestState(t, cache, "dependent-key", true, actionReceiptTestMutation("event-after-confirm"))
	if replayed.ShouldPublish || replayed.EventID != accepted.EventID {
		t.Fatalf("confirmed dependency replay requested duplicate handoff: %#v", replayed)
	}

}

func TestDependentReceiptRetryRetainsOriginalEventIdempotencyKey(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	accepted := setActionReceiptTestState(
		t,
		cache,
		"original-key",
		true,
		actionReceiptTestMutation("event-original"),
	)
	_ = setActionReceiptTestState(
		t,
		cache,
		"dependent-key",
		true,
		actionReceiptTestMutation("event-dependent"),
	)
	retried := setActionReceiptTestState(
		t,
		cache,
		"dependent-key",
		true,
		actionReceiptTestMutation("event-retry"),
	)
	if retried.EventID != accepted.EventID ||
		retried.IdempotencyKey != accepted.IdempotencyKey {
		t.Fatalf("retry mutated stable payload: got=%#v want=%#v", retried, accepted)
	}
}

func TestActionReceiptRollbackRefusesConfirmedOrDependentState(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	accepted := setActionReceiptTestState(t, cache, "original-key", true, actionReceiptTestMutation("event-original"))
	dependent := setActionReceiptTestState(t, cache, "dependent-key", true, actionReceiptTestMutation("event-dependent"))

	rolledBack, err := cache.RollbackActionState(context.Background(), accepted)
	if err != nil || rolledBack {
		t.Fatalf("rollback invalidated a pending dependent state: rolledBack=%t err=%v", rolledBack, err)
	}
	if err := cache.ConfirmActionStateHandoff(context.Background(), dependent); err != nil {
		t.Fatalf("confirm dependent handoff: %v", err)
	}
	rolledBack, err = cache.RollbackActionState(context.Background(), accepted)
	if err != nil || rolledBack {
		t.Fatalf("rollback invalidated a confirmed state: rolledBack=%t err=%v", rolledBack, err)
	}
}

func TestActionReceiptSuccessfulHandoffReplay(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	accepted := setActionReceiptTestState(t, cache, "handoff-key", true, actionReceiptTestMutation("event-handoff"))
	if err := cache.ConfirmActionStateHandoff(context.Background(), accepted); err != nil {
		t.Fatalf("confirm handoff: %v", err)
	}

	replayed := setActionReceiptTestState(t, cache, "handoff-key", true, actionReceiptTestMutation("event-retry"))
	if replayed.ShouldPublish || replayed.EventID != accepted.EventID || replayed.Version != accepted.Version {
		t.Fatalf("confirmed receipt requested duplicate publication: %#v", replayed)
	}
	if _, err := cache.SetActionState(
		context.Background(),
		42,
		1007,
		domaininteraction.ActionTypeLike,
		false,
		"handoff-key",
		&domaininteraction.VideoStat{VideoID: 1007},
		nil,
		actionReceiptTestMutation("event-opposite"),
	); !errors.Is(err, domaininteraction.ErrActionIdempotencyConflict) {
		t.Fatalf("opposite payload should conflict, got %v", err)
	}
}

func newActionReceiptTestCache(t *testing.T) *FeedCache {
	t.Helper()
	server, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})
	return NewFeedCache(client)
}

func setActionReceiptTestState(t *testing.T, cache *FeedCache, idempotencyKey string, active bool, mutation applicationinteraction.ActionMutation) *applicationinteraction.ActionStateResult {
	t.Helper()
	state, err := cache.SetActionState(
		context.Background(),
		42,
		1007,
		domaininteraction.ActionTypeLike,
		active,
		idempotencyKey,
		&domaininteraction.VideoStat{VideoID: 1007},
		nil,
		mutation,
	)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func actionReceiptTestMutation(eventID string) applicationinteraction.ActionMutation {
	return applicationinteraction.ActionMutation{
		EventID:    eventID,
		OccurredAt: time.Date(2026, time.July, 27, 9, 0, 0, 0, time.UTC),
	}
}
