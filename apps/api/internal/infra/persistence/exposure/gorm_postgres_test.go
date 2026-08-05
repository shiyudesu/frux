package infraexposure

import (
	domainexposure "github.com/shiyudesu/frux/internal/domain/exposure"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestSaveViewEventIdempotencyAndConflict(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("frux_exposure_idempotency_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})

	sqlDB, err := sql.Open("pgx", exposurePostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	if err := db.AutoMigrate(&infravideo.VideoModel{}, &ViewEventModel{}, &ExposureModel{}, &ViewHistoryModel{}, &ViewHistoryDeletionModel{}, &ViewEventOutboxModel{}); err != nil {
		t.Fatalf("migrate exposure tables: %v", err)
	}
	publishedAt := time.Now().UTC()
	if err := db.Create(&infravideo.VideoModel{
		ID: 1001, AuthorID: 9, Title: "video", MediaURL: "https://example.com/video.mp4",
		CoverURL: "https://example.com/cover.jpg", Status: domainvideo.StatusPublished,
		Visibility: domainvideo.VisibilityPublic, PublishedAt: &publishedAt,
	}).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}

	occurredAt := publishedAt.Add(-time.Second).Truncate(time.Microsecond)
	duration := 60_000
	event, err := domainexposure.NewViewEvent(domainexposure.NewViewEventInput{
		UserID: 42, VideoID: 1001, Scene: "timeline", EventType: domainexposure.EventTypeProgress,
		EventID: "event-idempotent", PlaybackSessionID: "session-1", Sequence: 2,
		OccurredAt: occurredAt, PositionMs: 20_000, WatchMs: 18_000, DurationMs: &duration,
	})
	if err != nil {
		t.Fatalf("new view event: %v", err)
	}
	repo := New(db)
	first, err := repo.SaveViewEvent(context.Background(), event)
	if err != nil || first.Replayed {
		t.Fatalf("save first event: result=%+v err=%v", first, err)
	}
	replayed, err := repo.SaveViewEvent(context.Background(), event)
	if err != nil || !replayed.Replayed || replayed.Event.ID != first.Event.ID {
		t.Fatalf("replay event: result=%+v err=%v", replayed, err)
	}

	conflicting := *event
	conflicting.PositionMs++
	if _, err := repo.SaveViewEvent(context.Background(), &conflicting); !errors.Is(err, domainexposure.ErrEventIDConflict) {
		t.Fatalf("expected event identity conflict, got %v", err)
	}
	var eventCount, outboxCount int64
	if err := db.Model(&ViewEventModel{}).Count(&eventCount).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	if err := db.Model(&ViewEventOutboxModel{}).Count(&outboxCount).Error; err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if eventCount != 1 || outboxCount != 1 {
		t.Fatalf("idempotent write duplicated rows: events=%d outbox=%d", eventCount, outboxCount)
	}

	if err := repo.DeleteHistory(context.Background(), event.UserID, event.VideoID); err != nil {
		t.Fatalf("delete history with watermark: %v", err)
	}
	delayed := *event
	delayed.EventID = "event-delayed-before-delete"
	delayed.Sequence = 3
	delayed.PositionMs = 30_000
	delayed.WatchMs = 25_000
	if _, err := repo.SaveViewEvent(context.Background(), &delayed); err != nil {
		t.Fatalf("save delayed event after delete: %v", err)
	}
	var historyCount int64
	if err := db.Model(&ViewHistoryModel{}).Where("user_id = ? AND video_id = ?", event.UserID, event.VideoID).Count(&historyCount).Error; err != nil {
		t.Fatalf("count history after delayed event: %v", err)
	}
	if historyCount != 0 {
		t.Fatalf("delayed event recreated deleted history: count=%d", historyCount)
	}
	fresh := delayed
	fresh.EventID = "event-after-delete"
	fresh.Sequence = 1
	fresh.PlaybackSessionID = "session-after-delete"
	fresh.OccurredAt = time.Now().UTC().Add(domainexposure.MaxFutureOccurrenceSkew + time.Second).Truncate(time.Microsecond)
	if _, err := repo.SaveViewEvent(context.Background(), &fresh); err != nil {
		t.Fatalf("save fresh event after delete: %v", err)
	}
	if err := db.Model(&ViewHistoryModel{}).Where("user_id = ? AND video_id = ?", event.UserID, event.VideoID).Count(&historyCount).Error; err != nil {
		t.Fatalf("count recreated history after fresh event: %v", err)
	}
	if historyCount != 1 {
		t.Fatalf("fresh event did not recreate history: count=%d", historyCount)
	}

	firstExposureOccurredAt := time.Now().UTC().Truncate(time.Microsecond)
	firstExposure, err := domainexposure.NewViewEvent(domainexposure.NewViewEventInput{
		UserID: 43, VideoID: 1001, Scene: "timeline", EventType: domainexposure.EventTypeExposed,
		EventID: "exposure-snapshot-first", PlaybackSessionID: "exposure-session-1",
		Sequence: 1, OccurredAt: firstExposureOccurredAt,
	})
	if err != nil {
		t.Fatalf("new first exposure snapshot event: %v", err)
	}
	firstExposureResult, err := repo.SaveViewEvent(context.Background(), firstExposure)
	if err != nil {
		t.Fatalf("save first exposure snapshot event: %v", err)
	}
	secondExposure, err := domainexposure.NewViewEvent(domainexposure.NewViewEventInput{
		UserID: 43, VideoID: 1001, Scene: "hot", EventType: domainexposure.EventTypeExposed,
		EventID: "exposure-snapshot-second", PlaybackSessionID: "exposure-session-2",
		Sequence: 1, OccurredAt: firstExposureOccurredAt.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("new second exposure snapshot event: %v", err)
	}
	if _, err := repo.SaveViewEvent(context.Background(), secondExposure); err != nil {
		t.Fatalf("save second exposure snapshot event: %v", err)
	}
	firstReplay, err := repo.SaveViewEvent(context.Background(), firstExposure)
	if err != nil {
		t.Fatalf("replay first exposure snapshot event: %v", err)
	}
	if !firstReplay.Replayed || firstReplay.Exposure == nil ||
		firstReplay.Exposure.ExposureCount != firstExposureResult.Exposure.ExposureCount ||
		!firstReplay.Exposure.LastExposedAt.Equal(firstExposureResult.Exposure.LastExposedAt) {
		t.Fatalf("exposure replay returned mutated aggregate: first=%+v replay=%+v", firstExposureResult.Exposure, firstReplay.Exposure)
	}
}

func TestViewHistoryUpsertRejectsOutOfOrderEvents(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("frux_exposure_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		if err := admin.Close(); err != nil {
			t.Errorf("close PostgreSQL: %v", err)
		}
	})

	sqlDB, err := sql.Open("pgx", exposurePostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	if err := db.AutoMigrate(&ViewEventModel{}, &ViewHistoryModel{}, &ViewHistoryDeletionModel{}); err != nil {
		t.Fatalf("migrate view event and history: %v", err)
	}

	base := time.Now().UTC().Truncate(time.Microsecond)
	newerEventID := "event-20"
	newer := ViewEventModel{
		ID: 20, UserID: 7, VideoID: 9, Scene: "profile",
		EventType: "complete", EventID: &newerEventID, PositionMs: 2000, WatchMs: 2000,
		Completed: true, OccurredAt: base.Add(2 * time.Second), CreatedAt: base.Add(2 * time.Second),
	}
	olderEventID := "event-10"
	older := ViewEventModel{
		ID: 10, UserID: 7, VideoID: 9, Scene: "timeline",
		EventType: "play", EventID: &olderEventID, PositionMs: 500, WatchMs: 500,
		OccurredAt: base.Add(time.Second), CreatedAt: base.Add(time.Second),
	}
	if err := upsertViewHistory(db, newer); err != nil {
		t.Fatalf("upsert newer event: %v", err)
	}
	if err := upsertViewHistory(db, older); err != nil {
		t.Fatalf("upsert older event: %v", err)
	}

	var history ViewHistoryModel
	if err := db.Where("user_id = ? AND video_id = ?", 7, 9).Take(&history).Error; err != nil {
		t.Fatalf("load history: %v", err)
	}
	if history.LastEventID != newerEventID || history.LastEventType != newer.EventType || !history.Completed || history.LastWatchMs != newer.WatchMs {
		t.Fatalf("older event regressed projection: %+v", history)
	}
	if !history.FirstWatchedAt.Equal(older.CreatedAt) {
		t.Fatalf("first watched time was not preserved: got=%s want=%s", history.FirstWatchedAt, older.CreatedAt)
	}

	tieWinner := newer
	tieWinner.ID = 21
	tieWinnerEventID := "event-21"
	tieWinner.EventID = &tieWinnerEventID
	tieWinner.EventType = "skip"
	tieWinner.Completed = false
	tieWinner.WatchMs = 1700
	if err := upsertViewHistory(db, tieWinner); err != nil {
		t.Fatalf("upsert tie winner: %v", err)
	}
	tieLoser := newer
	tieLoser.ID = 19
	tieLoserEventID := "event-19"
	tieLoser.EventID = &tieLoserEventID
	tieLoser.EventType = "play"
	tieLoser.Completed = false
	tieLoser.WatchMs = 100
	if err := upsertViewHistory(db, tieLoser); err != nil {
		t.Fatalf("upsert tie loser: %v", err)
	}
	if err := db.Where("user_id = ? AND video_id = ?", 7, 9).Take(&history).Error; err != nil {
		t.Fatalf("reload history: %v", err)
	}
	if history.LastEventID != tieWinnerEventID || history.LastEventType != tieWinner.EventType || history.LastWatchMs != newer.WatchMs {
		t.Fatalf("event ID tie-breaker regressed projection: %+v", history)
	}

	sessionID := "same-session"
	lowerSequence := int64(4)
	lowerSequenceEventID := "event-z"
	sameTime := base.Add(3 * time.Second)
	lowerSequenceEvent := ViewEventModel{
		ID: 30, UserID: 8, VideoID: 10, Scene: "timeline",
		EventType: "progress", EventID: &lowerSequenceEventID,
		PlaybackSessionID: &sessionID, Sequence: &lowerSequence,
		PositionMs: 4_000, WatchMs: 3_500, OccurredAt: sameTime, CreatedAt: sameTime,
	}
	if err := upsertViewHistory(db, lowerSequenceEvent); err != nil {
		t.Fatalf("upsert lower sequence event: %v", err)
	}
	higherSequence := int64(5)
	higherSequenceEventID := "event-a"
	higherSequenceEvent := lowerSequenceEvent
	higherSequenceEvent.ID = 31
	higherSequenceEvent.EventID = &higherSequenceEventID
	higherSequenceEvent.Sequence = &higherSequence
	higherSequenceEvent.PositionMs = 5_000
	higherSequenceEvent.WatchMs = 4_500
	if err := upsertViewHistory(db, higherSequenceEvent); err != nil {
		t.Fatalf("upsert higher sequence event: %v", err)
	}
	history = ViewHistoryModel{}
	if err := db.Where("user_id = ? AND video_id = ?", 8, 10).Take(&history).Error; err != nil {
		t.Fatalf("load same-session history: %v", err)
	}
	if history.LastEventID != higherSequenceEventID ||
		history.LastSequence == nil || *history.LastSequence != higherSequence ||
		history.LastPositionMs != higherSequenceEvent.PositionMs {
		t.Fatalf("higher same-session sequence did not win equal occurred_at: %+v", history)
	}
	olderOccurredHigherSequence := int64(6)
	olderOccurredHigherSequenceID := "event-older-sequence-6"
	olderOccurredEvent := higherSequenceEvent
	olderOccurredEvent.ID = 32
	olderOccurredEvent.EventID = &olderOccurredHigherSequenceID
	olderOccurredEvent.Sequence = &olderOccurredHigherSequence
	olderOccurredEvent.OccurredAt = sameTime.Add(-time.Second)
	olderOccurredEvent.CreatedAt = olderOccurredEvent.OccurredAt
	olderOccurredEvent.PositionMs = 6_000
	olderOccurredEvent.WatchMs = 5_500
	if err := upsertViewHistory(db, olderOccurredEvent); err != nil {
		t.Fatalf("upsert older occurred higher sequence event: %v", err)
	}
	if err := db.Where("user_id = ? AND video_id = ?", 8, 10).Take(&history).Error; err != nil {
		t.Fatalf("reload same-session history: %v", err)
	}
	if history.LastEventID != higherSequenceEventID ||
		history.LastSequence == nil || *history.LastSequence != higherSequence ||
		!history.LastOccurredAt.Equal(sameTime) {
		t.Fatalf("older occurred_at moved same-session history backward: %+v", history)
	}

	backfillSessionID := "backfill-session"
	backfillLowerSequence := int64(7)
	backfillHigherSequence := int64(8)
	backfillOlderHigherSequence := int64(9)
	backfillLowerEventID := "backfill-z"
	backfillHigherEventID := "backfill-a"
	backfillOlderHigherEventID := "backfill-older-9"
	backfillTime := base.Add(4 * time.Second)
	backfillEvents := []ViewEventModel{
		{
			UserID: 9, VideoID: 11, Scene: "timeline", EventType: "progress",
			EventID: &backfillLowerEventID, PlaybackSessionID: &backfillSessionID,
			Sequence: &backfillLowerSequence, OccurredAt: backfillTime,
			PositionMs: 7_000, WatchMs: 6_500, CreatedAt: backfillTime,
		},
		{
			UserID: 9, VideoID: 11, Scene: "timeline", EventType: "progress",
			EventID: &backfillHigherEventID, PlaybackSessionID: &backfillSessionID,
			Sequence: &backfillHigherSequence, OccurredAt: backfillTime,
			PositionMs: 8_000, WatchMs: 7_500, CreatedAt: backfillTime,
		},
		{
			UserID: 9, VideoID: 11, Scene: "timeline", EventType: "progress",
			EventID: &backfillOlderHigherEventID, PlaybackSessionID: &backfillSessionID,
			Sequence: &backfillOlderHigherSequence, OccurredAt: backfillTime.Add(-time.Second),
			PositionMs: 9_000, WatchMs: 8_500, CreatedAt: backfillTime.Add(-time.Second),
		},
	}
	if err := db.Create(&backfillEvents).Error; err != nil {
		t.Fatalf("create backfill events: %v", err)
	}
	if err := EnsureViewHistory(db); err != nil {
		t.Fatalf("backfill view history: %v", err)
	}
	history = ViewHistoryModel{}
	if err := db.Where("user_id = ? AND video_id = ?", 9, 11).Take(&history).Error; err != nil {
		t.Fatalf("load backfilled history: %v", err)
	}
	if history.LastEventID != backfillHigherEventID ||
		history.LastSequence == nil || *history.LastSequence != backfillHigherSequence ||
		!history.LastOccurredAt.Equal(backfillTime) ||
		history.LastPositionMs != 9_000 {
		t.Fatalf("backfill did not prioritize higher same-session sequence: %+v", history)
	}

	aggregateCompleteEventID := "aggregate-complete"
	aggregateSkipEventID := "aggregate-skip"
	aggregateCompleteSession := "aggregate-session-a"
	aggregateSkipSession := "aggregate-session-b"
	aggregateCompleteSequence := int64(2)
	aggregateSkipSequence := int64(1)
	aggregateEvents := []ViewEventModel{
		{
			UserID: 10, VideoID: 12, Scene: "timeline", EventType: "complete",
			EventID: &aggregateCompleteEventID, PlaybackSessionID: &aggregateCompleteSession,
			Sequence: &aggregateCompleteSequence, OccurredAt: base.Add(5 * time.Second),
			PositionMs: 10_000, WatchMs: 9_000, Completed: true, CreatedAt: base.Add(5 * time.Second),
		},
		{
			UserID: 10, VideoID: 12, Scene: "profile", EventType: "skip",
			EventID: &aggregateSkipEventID, PlaybackSessionID: &aggregateSkipSession,
			Sequence: &aggregateSkipSequence, OccurredAt: base.Add(6 * time.Second),
			PositionMs: 1_000, WatchMs: 500, Completed: false, CreatedAt: base.Add(6 * time.Second),
		},
	}
	if err := db.Create(&aggregateEvents).Error; err != nil {
		t.Fatalf("create aggregate backfill events: %v", err)
	}
	if err := EnsureViewHistory(db); err != nil {
		t.Fatalf("rerun fresh-install backfill query: %v", err)
	}
	history = ViewHistoryModel{}
	if err := db.Where("user_id = ? AND video_id = ?", 10, 12).Take(&history).Error; err != nil {
		t.Fatalf("load aggregate backfilled history: %v", err)
	}
	if history.LastEventID != aggregateSkipEventID || history.LastEventType != "skip" ||
		history.LastPositionMs != 10_000 || history.LastWatchMs != 9_000 || !history.Completed {
		t.Fatalf("backfill latest metadata erased aggregate progress/completion: %+v", history)
	}

	liveNewerEventID := "live-newer-play"
	liveOlderCompleteEventID := "live-older-complete"
	liveNewer := ViewEventModel{
		UserID: 11, VideoID: 13, Scene: "profile", EventType: "play",
		EventID: &liveNewerEventID, OccurredAt: base.Add(8 * time.Second),
		PositionMs: 1_000, WatchMs: 800, CreatedAt: base.Add(8 * time.Second),
	}
	liveOlderComplete := ViewEventModel{
		UserID: 11, VideoID: 13, Scene: "timeline", EventType: "complete",
		EventID: &liveOlderCompleteEventID, OccurredAt: base.Add(7 * time.Second),
		PositionMs: 5_000, WatchMs: 4_500, Completed: true, CreatedAt: base.Add(7 * time.Second),
	}
	if err := upsertViewHistory(db, liveNewer); err != nil {
		t.Fatalf("upsert live newer metadata: %v", err)
	}
	if err := upsertViewHistory(db, liveOlderComplete); err != nil {
		t.Fatalf("upsert delayed older completion: %v", err)
	}
	history = ViewHistoryModel{}
	if err := db.Where("user_id = ? AND video_id = ?", 11, 13).Take(&history).Error; err != nil {
		t.Fatalf("load live aggregate history: %v", err)
	}
	if history.LastEventID != liveNewerEventID || history.LastEventType != "play" ||
		history.LastPositionMs != 5_000 || history.LastWatchMs != 4_500 || !history.Completed {
		t.Fatalf("delayed completion/progress was lost or replaced latest metadata: %+v", history)
	}

	repairEventID := "repair-complete"
	repairEvent := ViewEventModel{
		UserID: 12, VideoID: 14, Scene: "timeline", EventType: "complete",
		EventID: &repairEventID, OccurredAt: base.Add(9 * time.Second),
		PositionMs: 8_000, WatchMs: 7_500, Completed: true, CreatedAt: base.Add(9 * time.Second),
	}
	if err := db.Create(&repairEvent).Error; err != nil {
		t.Fatalf("create repair source event: %v", err)
	}
	if err := db.Create(&ViewHistoryModel{
		UserID: 12, VideoID: 14, LastScene: "timeline", LastEventType: "play",
		LastPositionMs: 1_000, LastWatchMs: 900, Completed: false,
		FirstWatchedAt: base, LastWatchedAt: base, LastOccurredAt: base,
		LastEventID: "older-history", CreatedAt: base, UpdatedAt: base,
	}).Error; err != nil {
		t.Fatalf("create repair target history: %v", err)
	}
	deletedHistoryEventID := "deleted-history-complete"
	if err := db.Create(&ViewEventModel{
		UserID: 13, VideoID: 15, Scene: "timeline", EventType: "complete",
		EventID: &deletedHistoryEventID, OccurredAt: base.Add(10 * time.Second),
		PositionMs: 9_000, WatchMs: 8_000, Completed: true, CreatedAt: base.Add(10 * time.Second),
	}).Error; err != nil {
		t.Fatalf("create deleted-history source event: %v", err)
	}
	if err := RepairExistingViewHistoryAggregates(db); err != nil {
		t.Fatalf("repair existing history aggregates: %v", err)
	}
	history = ViewHistoryModel{}
	if err := db.Where("user_id = ? AND video_id = ?", 12, 14).Take(&history).Error; err != nil {
		t.Fatalf("load repaired history: %v", err)
	}
	if history.LastPositionMs != 8_000 || history.LastWatchMs != 7_500 || !history.Completed {
		t.Fatalf("existing history aggregates were not repaired: %+v", history)
	}
	var deletedHistoryCount int64
	if err := db.Model(&ViewHistoryModel{}).Where("user_id = ? AND video_id = ?", 13, 15).Count(&deletedHistoryCount).Error; err != nil {
		t.Fatalf("count deleted history projection: %v", err)
	}
	if deletedHistoryCount != 0 {
		t.Fatalf("aggregate repair recreated deleted history: count=%d", deletedHistoryCount)
	}
}

func exposurePostgresDSNWithSchema(dsn, schema string) string {
	if strings.Contains(dsn, "://") {
		parsed, err := url.Parse(dsn)
		if err == nil {
			query := parsed.Query()
			query.Set("search_path", schema)
			query.Set("TimeZone", "UTC")
			parsed.RawQuery = query.Encode()
			return parsed.String()
		}
	}
	return strings.TrimSpace(dsn) + " search_path=" + schema + " TimeZone=UTC"
}
