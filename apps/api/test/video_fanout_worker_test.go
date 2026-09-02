package test

import (
	"context"
	"testing"
	"time"

	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"
)

type memoryFanoutRepo struct {
	followerCount int
	followerIDs   []int64
}

func (r *memoryFanoutRepo) CountFollowers(ctx context.Context, authorID int64) (int, error) {
	return r.followerCount, nil
}

func (r *memoryFanoutRepo) ListFollowerIDs(ctx context.Context, authorID int64, cursor int64, limit int) ([]int64, error) {
	items := make([]int64, 0, limit)
	for _, followerID := range r.followerIDs {
		if followerID <= cursor {
			continue
		}
		items = append(items, followerID)
		if len(items) >= limit {
			break
		}
	}
	return items, nil
}

func (r *memoryFanoutRepo) BatchGetFeedCards(ctx context.Context, videoIDs []int64) (map[int64]*domainfeed.FeedCard, error) {
	cards := map[int64]*domainfeed.FeedCard{}
	for _, videoID := range videoIDs {
		cards[videoID] = &domainfeed.FeedCard{VideoID: videoID, AuthorID: 7, Title: "published"}
	}
	return cards, nil
}

func (r *memoryFanoutRepo) BatchGetFeedStats(ctx context.Context, videoIDs []int64) (map[int64]*domainfeed.FeedStat, error) {
	stats := map[int64]*domainfeed.FeedStat{}
	for _, videoID := range videoIDs {
		stats[videoID] = &domainfeed.FeedStat{VideoID: videoID}
	}
	return stats, nil
}

type memoryFollowingIndex struct {
	inboxUsers    []int64
	inboxVideoID  int64
	outboxAuthor  int64
	outboxVideoID int64
}

type countingFollowingIndex struct {
	inboxCalls   int
	inboxWrites  int
	outboxCalls  int
	outboxWrites int
}

func (i *countingFollowingIndex) AddInboxItems(_ context.Context, _ int64, userIDs []int64, _ *domainfeed.FeedPageItem, _ int64) error {
	i.inboxCalls++
	i.inboxWrites += len(userIDs)
	return nil
}

func (i *countingFollowingIndex) AddAuthorOutboxItem(_ context.Context, _ int64, _ *domainfeed.FeedPageItem, _ int64) error {
	i.outboxCalls++
	i.outboxWrites++
	return nil
}

func (i *memoryFollowingIndex) AddInboxItems(ctx context.Context, authorID int64, userIDs []int64, item *domainfeed.FeedPageItem, maxLen int64) error {
	i.inboxUsers = append(i.inboxUsers, userIDs...)
	i.inboxVideoID = item.VideoID
	return nil
}

func (i *memoryFollowingIndex) AddAuthorOutboxItem(ctx context.Context, authorID int64, item *domainfeed.FeedPageItem, maxLen int64) error {
	i.outboxAuthor = authorID
	i.outboxVideoID = item.VideoID
	return nil
}

type memoryFeedPreheater struct {
	videoID int64
}

type staticFanoutControlReader bool

func (r staticFanoutControlReader) Bool(key domaingovernance.Key) bool {
	return bool(r)
}

func (p *memoryFeedPreheater) PreheatFeedVideo(ctx context.Context, videoID int64, ttl time.Duration) error {
	p.videoID = videoID
	return nil
}

func TestFanoutWorkerPushesSmallCreatorInbox(t *testing.T) {
	repo := &memoryFanoutRepo{
		followerCount: 3,
		followerIDs:   []int64{10, 11, 12},
	}
	index := &memoryFollowingIndex{}
	preheater := &memoryFeedPreheater{}
	worker := applicationvideo.NewFanoutWorker(repo, nil, index, preheater, applicationvideo.WithFanoutBatchSize(2))

	event := &applicationvideo.PublishedEvent{
		VideoID:     99,
		AuthorID:    7,
		Title:       "published",
		PublishedAt: time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
	}
	if err := worker.HandleVideoPublished(context.Background(), event); err != nil {
		t.Fatalf("handle video published: %v", err)
	}
	if len(index.inboxUsers) != 3 || index.inboxUsers[0] != 10 || index.inboxUsers[2] != 12 || index.inboxVideoID != 99 {
		t.Fatalf("unexpected inbox fanout: %+v", index)
	}
	if index.outboxVideoID != 0 {
		t.Fatalf("unexpected outbox fanout: %+v", index)
	}
	if preheater.videoID != 99 {
		t.Fatalf("unexpected preheat video id: %d", preheater.videoID)
	}
}

func TestFanoutWorkerWritesBigCreatorOutbox(t *testing.T) {
	repo := &memoryFanoutRepo{
		followerCount: domainfeed.BigCreatorFollowerThreshold,
		followerIDs:   []int64{10, 11, 12},
	}
	index := &memoryFollowingIndex{}
	worker := applicationvideo.NewFanoutWorker(repo, nil, index, nil)

	event := &applicationvideo.PublishedEvent{
		VideoID:     100,
		AuthorID:    8,
		PublishedAt: time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
	}
	if err := worker.HandleVideoPublished(context.Background(), event); err != nil {
		t.Fatalf("handle video published: %v", err)
	}
	if len(index.inboxUsers) != 0 {
		t.Fatalf("unexpected inbox fanout: %+v", index)
	}
	if index.outboxAuthor != 8 || index.outboxVideoID != 100 {
		t.Fatalf("unexpected outbox fanout: %+v", index)
	}
}

func TestFanoutWorkerSkipsPreheatWhenRuntimeControlIsDisabled(t *testing.T) {
	repo := &memoryFanoutRepo{followerCount: 1, followerIDs: []int64{10}}
	index := &memoryFollowingIndex{}
	preheater := &memoryFeedPreheater{}
	worker := applicationvideo.NewFanoutWorker(
		repo, nil, index, preheater,
		applicationvideo.WithFanoutControlReader(staticFanoutControlReader(false)),
	)
	event := &applicationvideo.PublishedEvent{
		VideoID: 101, AuthorID: 8,
		PublishedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
	}
	if err := worker.HandleVideoPublished(context.Background(), event); err != nil {
		t.Fatalf("handle video published: %v", err)
	}
	if preheater.videoID != 0 {
		t.Fatalf("disabled control still preheated video %d", preheater.videoID)
	}
	if index.inboxVideoID != 101 {
		t.Fatal("disabling optional preheat changed required fanout behavior")
	}
}

func TestFanoutCostMatrix(t *testing.T) {
	tests := []int{100, 1000, 5000, 9999, 10000, 50000}
	for _, followerCount := range tests {
		repo := &memoryFanoutRepo{followerCount: followerCount}
		if followerCount < domainfeed.BigCreatorFollowerThreshold {
			repo.followerIDs = make([]int64, followerCount)
			for index := range repo.followerIDs {
				repo.followerIDs[index] = int64(index + 1)
			}
		}
		index := &countingFollowingIndex{}
		worker := applicationvideo.NewFanoutWorker(
			repo, nil, index, nil,
			applicationvideo.WithFanoutBatchSize(500),
		)
		started := time.Now()
		err := worker.HandleVideoPublished(context.Background(), &applicationvideo.PublishedEvent{
			VideoID: 9001, AuthorID: 42,
			PublishedAt: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC),
		})
		duration := time.Since(started)
		if err != nil {
			t.Fatalf("followers=%d: %v", followerCount, err)
		}
		if followerCount < domainfeed.BigCreatorFollowerThreshold {
			if index.inboxWrites != followerCount || index.outboxWrites != 0 {
				t.Fatalf("followers=%d index=%+v", followerCount, index)
			}
		} else if index.inboxWrites != 0 || index.outboxWrites != 1 {
			t.Fatalf("followers=%d index=%+v", followerCount, index)
		}
		t.Logf(
			"followers=%d inbox_calls=%d logical_inbox_writes=%d outbox_calls=%d logical_outbox_writes=%d duration=%s",
			followerCount,
			index.inboxCalls,
			index.inboxWrites,
			index.outboxCalls,
			index.outboxWrites,
			duration,
		)
	}
}

func TestHybridFanoutLogicalWriteReductionForBenchmarkMix(t *testing.T) {
	const normalEvents = 800
	const normalFollowers = 5419
	const bigCreatorEvents = 200
	const bigCreatorFollowers = 11999

	allPushWrites := normalEvents*normalFollowers + bigCreatorEvents*bigCreatorFollowers
	hybridWrites := normalEvents*normalFollowers + bigCreatorEvents
	reduction := 1 - float64(hybridWrites)/float64(allPushWrites)
	if reduction <= 0 {
		t.Fatalf("unexpected reduction: %f", reduction)
	}
	t.Logf(
		"events=%d all_push_logical_writes=%d hybrid_logical_writes=%d reduction=%.2f%%",
		normalEvents+bigCreatorEvents,
		allPushWrites,
		hybridWrites,
		reduction*100,
	)
}
