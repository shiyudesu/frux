package infracache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	"sync"
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

func TestFollowingIndexMergesInboxAndOutboxWithoutDuplicatesInTimelineOrder(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	// Reverse the IDs relative to publication order and keep every item in the
	// same second. This catches score encodings that accidentally order a burst
	// by video ID instead of its sub-second publication timestamp.
	oldest := &domainfeed.FeedPageItem{VideoID: 303, AuthorID: 10, PublishedAt: base}
	middle := &domainfeed.FeedPageItem{VideoID: 202, AuthorID: 10, PublishedAt: base.Add(250 * time.Microsecond)}
	newest := &domainfeed.FeedPageItem{VideoID: 101, AuthorID: 99, PublishedAt: base.Add(500 * time.Microsecond)}

	if err := cache.AddInboxItems(ctx, oldest.AuthorID, []int64{42}, oldest, 100); err != nil {
		t.Fatalf("add oldest inbox item: %v", err)
	}
	if err := cache.AddInboxItems(ctx, middle.AuthorID, []int64{42}, middle, 100); err != nil {
		t.Fatalf("add middle inbox item: %v", err)
	}
	// Simulate a threshold transition leaving the same video in both indexes.
	if err := cache.AddInboxItems(ctx, newest.AuthorID, []int64{42}, newest, 100); err != nil {
		t.Fatalf("add duplicate inbox item: %v", err)
	}
	if err := cache.AddAuthorOutboxItem(ctx, newest.AuthorID, newest, 100); err != nil {
		t.Fatalf("add author outbox item: %v", err)
	}

	first, ok, err := cache.ListFollowingIndexPage(ctx, 42, []int64{10, 99}, []int64{99}, nil, 2)
	if err != nil || !ok {
		t.Fatalf("list first merged page: ok=%t err=%v", ok, err)
	}
	if len(first) != 2 || first[0].VideoID != 101 || first[1].VideoID != 202 {
		t.Fatalf("unexpected first merged page: %+v", first)
	}

	second, ok, err := cache.ListFollowingIndexPage(ctx, 42, []int64{10, 99}, []int64{99}, &domainfeed.TimelineCursor{
		PublishedAt: first[1].PublishedAt,
		VideoID:     first[1].VideoID,
	}, 2)
	if err != nil || !ok {
		t.Fatalf("list second merged page: ok=%t err=%v", ok, err)
	}
	if len(second) != 1 || second[0].VideoID != 303 {
		t.Fatalf("unexpected second merged page: %+v", second)
	}
}

func TestFollowingIndexWritesAreIdempotentForDuplicateDelivery(t *testing.T) {
	cache := newActionReceiptTestCache(t)
	ctx := context.Background()
	item := &domainfeed.FeedPageItem{
		VideoID: 201, AuthorID: 20,
		PublishedAt: time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC),
	}
	for range 2 {
		if err := cache.AddInboxItems(ctx, item.AuthorID, []int64{42}, item, 100); err != nil {
			t.Fatalf("repeat inbox delivery: %v", err)
		}
		if err := cache.AddAuthorOutboxItem(ctx, item.AuthorID, item, 100); err != nil {
			t.Fatalf("repeat outbox delivery: %v", err)
		}
	}

	inboxCount, err := cache.client.ZCard(ctx, followingInboxKey(42)).Result()
	if err != nil {
		t.Fatalf("read inbox cardinality: %v", err)
	}
	outboxCount, err := cache.client.ZCard(ctx, followingAuthorOutboxKey(item.AuthorID)).Result()
	if err != nil {
		t.Fatalf("read outbox cardinality: %v", err)
	}
	if inboxCount != 1 || outboxCount != 1 {
		t.Fatalf("duplicate delivery changed cardinality: inbox=%d outbox=%d", inboxCount, outboxCount)
	}
}

func TestFeedCacheBatchReadsUseTwoCommandsInsteadOfPerItemGets(t *testing.T) {
	server, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})
	ctx := context.Background()
	batch := NewFeedCache(client)
	cards := map[int64]*domainfeed.FeedCard{}
	stats := map[int64]*domainfeed.FeedStat{}
	for _, videoID := range []int64{1, 2, 3} {
		cards[videoID] = &domainfeed.FeedCard{VideoID: videoID}
		stats[videoID] = &domainfeed.FeedStat{VideoID: videoID}
	}
	batch.statSource = &memoryFeedStatSource{stats: stats}
	if err := batch.SetCards(ctx, cards, time.Minute); err != nil {
		t.Fatalf("set cards: %v", err)
	}
	if err := batch.SetStats(ctx, stats, time.Minute); err != nil {
		t.Fatalf("set stats: %v", err)
	}

	beforeBatch := server.CommandCount()
	if _, err := batch.GetCards(ctx, []int64{1, 2, 3}); err != nil {
		t.Fatalf("batch cards: %v", err)
	}
	if _, err := batch.GetStats(ctx, []int64{1, 2, 3}); err != nil {
		t.Fatalf("batch stats: %v", err)
	}
	batchCommands := server.CommandCount() - beforeBatch

	sequential := NewFeedCache(client, WithSequentialFeedCacheReads(true))
	beforeSequential := server.CommandCount()
	if _, err := sequential.GetCards(ctx, []int64{1, 2, 3}); err != nil {
		t.Fatalf("sequential cards: %v", err)
	}
	if _, err := sequential.GetStats(ctx, []int64{1, 2, 3}); err != nil {
		t.Fatalf("sequential stats: %v", err)
	}
	sequentialCommands := server.CommandCount() - beforeSequential

	if batchCommands != 2 || sequentialCommands != 6 {
		t.Fatalf("commands: batch=%d sequential=%d", batchCommands, sequentialCommands)
	}
}

type actionHandoffRepositoryStub struct {
	mu        sync.Mutex
	stat      domaininteraction.VideoStat
	states    map[string]domaininteraction.ActionStateSnapshot
	processed map[string]bool
}

func (r *actionHandoffRepositoryStub) PersistAcceptedActionEvent(_ context.Context, event *domaininteraction.AcceptedActionEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.processed == nil {
		r.processed = map[string]bool{}
		r.states = map[string]domaininteraction.ActionStateSnapshot{}
	}
	if r.processed[event.EventID] {
		return nil
	}
	r.processed[event.EventID] = true
	key := fmt.Sprintf("%d:%d:%s", event.UserID, event.VideoID, event.ActionType)
	previous := r.states[key]
	delta := 0
	if previous.Active != event.Active {
		if event.Active {
			delta = 1
		} else {
			delta = -1
		}
	}
	r.stat.VideoID = event.VideoID
	if event.ActionType == domaininteraction.ActionTypeLike {
		r.stat.LikeCount += delta
	} else {
		r.stat.FavoriteCount += delta
	}
	if delta != 0 {
		r.stat.Revision++
	}
	r.states[key] = domaininteraction.ActionStateSnapshot{Exists: true, Active: event.Active, Version: event.Version, EventID: event.EventID, IdempotencyKey: event.IdempotencyKey, OccurredAt: event.OccurredAt, UpdatedAt: event.OccurredAt}
	return nil
}

func (r *actionHandoffRepositoryStub) GetVideoStat(_ context.Context, videoID int64) (*domaininteraction.VideoStat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stat := r.stat
	stat.VideoID = videoID
	return &stat, nil
}

func (r *actionHandoffRepositoryStub) GetActionState(_ context.Context, userID int64, videoID int64, actionType string) (*domaininteraction.ActionStateSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[fmt.Sprintf("%d:%d:%s", userID, videoID, actionType)]
	return &state, nil
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

func TestSetVideoStatWritesJSONCache(t *testing.T) {
	ctx := context.Background()
	videoID := int64(1005)
	cache := newActionReceiptTestCache(t)
	redisClient := cache.client
	cache.statSource = &memoryFeedStatSource{stats: map[int64]*domainfeed.FeedStat{
		videoID: {VideoID: videoID, LikeCount: 2, CommentCount: 3, FavoriteCount: 1},
	}}

	err := cache.SetVideoStat(ctx, &domaininteraction.VideoStat{
		VideoID:       videoID,
		LikeCount:     2,
		CommentCount:  3,
		FavoriteCount: 1,
	})
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
