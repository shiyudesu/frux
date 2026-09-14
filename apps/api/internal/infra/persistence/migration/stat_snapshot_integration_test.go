package migration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	infracache "github.com/shiyudesu/frux/internal/infra/cache"
	infrafeed "github.com/shiyudesu/frux/internal/infra/persistence/feed"
)

type snapshotTestPublisher struct {
	check func(*applicationinteraction.ActionChangedEvent)
}

func (p snapshotTestPublisher) PublishActionChanged(_ context.Context, event *applicationinteraction.ActionChangedEvent) error {
	if p.check != nil {
		p.check(event)
	}
	return errors.New("Kafka unavailable after durable acceptance")
}

func TestPostgreSQLStatSnapshotsRecoverAfterRedisLoss(t *testing.T) {
	db, repo, _, actor, video := newPostgresInteractionEventFixture(t)
	ctx := context.Background()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	defer client.Close()
	source := infrafeed.New(db)
	cache := infracache.NewFeedCache(client, infracache.WithFeedStatSource(source))
	publisher := snapshotTestPublisher{check: func(event *applicationinteraction.ActionChangedEvent) {
		state, err := repo.GetActionState(ctx, actor.ID, video.ID, event.ActionType)
		if err != nil || state.EventID != event.EventID || state.Active != event.Active {
			t.Fatalf("Kafka publication preceded durable acceptance: %+v, %v", state, err)
		}
	}}
	service := applicationinteraction.New(repo, applicationinteraction.WithAsyncActionPipeline(cache, publisher), applicationinteraction.WithStatCache(cache))
	liked, err := service.Like(ctx, actor.ID, video.ID, "durable-like")
	if err != nil || !liked.Active || liked.LikeCount != 1 {
		t.Fatalf("like: %+v %v", liked, err)
	}
	first, err := repo.GetVideoStat(ctx, video.ID)
	if err != nil || first.Revision != 1 || first.LikeCount != 1 {
		t.Fatalf("first snapshot: %+v %v", first, err)
	}

	// Redis contains no counters/receipts/fences after the loss. Even an old
	// callback must reload PostgreSQL rather than repopulate an old snapshot.
	server.FlushAll()
	if err := cache.SetVideoStat(ctx, &domaininteraction.VideoStat{VideoID: video.ID}); err != nil {
		t.Fatal(err)
	}
	stats, err := cache.GetStats(ctx, []int64{video.ID})
	if err != nil || stats[video.ID] == nil || stats[video.ID].LikeCount != 1 || stats[video.ID].Revision != 1 {
		t.Fatalf("Redis recovery: %+v %v", stats, err)
	}
	replayed, err := service.Like(ctx, actor.ID, video.ID, "durable-like")
	if err != nil || replayed.LikeCount != 1 {
		t.Fatalf("post-loss replay: %+v %v", replayed, err)
	}
	afterReplay, err := repo.GetVideoStat(ctx, video.ID)
	if err != nil || afterReplay.Revision != 1 {
		t.Fatalf("replay advanced count revision: %+v %v", afterReplay, err)
	}
	unliked, err := service.Unlike(ctx, actor.ID, video.ID, "durable-unlike")
	if err != nil || unliked.Active || unliked.LikeCount != 0 {
		t.Fatalf("unlike: %+v %v", unliked, err)
	}
	if err := cache.SetVideoStat(ctx, first); err != nil {
		t.Fatal(err)
	}
	stats, err = cache.GetStats(ctx, []int64{video.ID})
	if err != nil || stats[video.ID].Revision != 2 || stats[video.ID].LikeCount != 0 {
		t.Fatalf("stale like overwrote unlike: %+v %v", stats, err)
	}
	server.FlushAll()
	if _, err := service.Like(ctx, actor.ID, video.ID, "durable-like"); err != nil {
		t.Fatal(err)
	}
	currentState, err := repo.GetActionState(ctx, actor.ID, video.ID, domaininteraction.ActionTypeLike)
	if err != nil || currentState.Active {
		t.Fatalf("old request resurrected a canceled like after Redis loss: %+v %v", currentState, err)
	}
	afterOldReplay, err := repo.GetVideoStat(ctx, video.ID)
	if err != nil || afterOldReplay.Revision != 2 || afterOldReplay.LikeCount != 0 {
		t.Fatalf("old HTTP key was applied again: %+v %v", afterOldReplay, err)
	}

	// Comment snapshots use the same global count revision and are immutable.
	if _, err := service.CreateComment(ctx, actor.ID, video.ID, "first", "comment-1"); err != nil {
		t.Fatal(err)
	}
	olderComment, err := repo.GetVideoStat(ctx, video.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateComment(ctx, actor.ID, video.ID, "second", "comment-2"); err != nil {
		t.Fatal(err)
	}
	server.FlushAll()
	if err := cache.SetVideoStat(ctx, olderComment); err != nil {
		t.Fatal(err)
	}
	stats, err = cache.GetStats(ctx, []int64{video.ID})
	if err != nil || stats[video.ID].Revision != 4 || stats[video.ID].CommentCount != 2 || stats[video.ID].LikeCount != 0 {
		t.Fatalf("comment recovery regressed snapshot: %+v %v", stats, err)
	}

	// Legacy shards left over from the previous version cannot be mixed in.
	if err := client.HSet(ctx, "video:stat:counter:v1:1:shard:00", "like_count", 999).Err(); err != nil {
		t.Fatal(err)
	}
	server.FastForward(25 * time.Hour)
	if err := cache.SetStats(ctx, map[int64]*domainfeed.FeedStat{video.ID: {VideoID: video.ID, Revision: first.Revision, LikeCount: first.LikeCount}}, 15*time.Second); err != nil {
		t.Fatal(err)
	}
	stats, err = cache.GetStats(ctx, []int64{video.ID})
	if err != nil || stats[video.ID].Revision != 4 || stats[video.ID].LikeCount != 0 || stats[video.ID].CommentCount != 2 {
		t.Fatalf("expiry lost durable counts: %+v %v", stats, err)
	}
}

func TestPostgreSQLStatRevisionCommitsAtomicallyWithCounts(t *testing.T) {
	db, repo, author, actor, video := newPostgresInteractionEventFixture(t)
	ctx := context.Background()
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, userID := range []int64{actor.ID, author.ID} {
		wg.Add(1)
		go func(userID int64) {
			defer wg.Done()
			<-start
			_, _, _, err := repo.SetAction(ctx, userID, video.ID, domaininteraction.ActionTypeLike, true, "")
			errs <- err
		}(userID)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := repo.GetVideoStat(ctx, video.ID)
	if err != nil || snapshot.Revision != 2 || snapshot.LikeCount != 2 {
		t.Fatalf("concurrent revision: %+v %v", snapshot, err)
	}
	if _, _, _, err := repo.SetAction(ctx, actor.ID, video.ID, domaininteraction.ActionTypeLike, true, ""); err != nil {
		t.Fatal(err)
	}
	same, _ := repo.GetVideoStat(ctx, video.ID)
	if same.Revision != 2 {
		t.Fatalf("no-op advanced revision: %+v", same)
	}
	if _, _, _, err := repo.SetAction(ctx, actor.ID, video.ID, domaininteraction.ActionTypeLike, false, ""); err != nil {
		t.Fatal(err)
	}
	batch, err := infrafeed.New(db).BatchGetFeedStats(ctx, []int64{video.ID})
	if err != nil || batch[video.ID].Revision != 3 || batch[video.ID].LikeCount != 1 {
		t.Fatalf("batch count/revision disagree: %+v %v", batch, err)
	}
	first := mustAcceptedActionEvent(t, "revision-conflict", actor.ID, video.ID, domaininteraction.ActionTypeFavorite, true, "favorite-a", 0, time.Now().UTC())
	if _, _, _, err := repo.SetActionWithAcceptedEvent(ctx, first); err != nil {
		t.Fatal(err)
	}
	conflicting := mustAcceptedActionEvent(t, "revision-conflict", author.ID, video.ID, domaininteraction.ActionTypeFavorite, true, "favorite-b", 0, time.Now().UTC())
	if _, _, _, err := repo.SetActionWithAcceptedEvent(ctx, conflicting); !errors.Is(err, domaininteraction.ErrActionEventConflict) {
		t.Fatalf("expected transactional rollback: %v", err)
	}
	afterRollback, err := repo.GetVideoStat(ctx, video.ID)
	if err != nil || afterRollback.Revision != 4 || afterRollback.FavoriteCount != 1 {
		t.Fatalf("rolled back transaction advanced counts/revision: %+v %v", afterRollback, err)
	}
}
