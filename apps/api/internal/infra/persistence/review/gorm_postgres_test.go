package infrareview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infraadminaudit "github.com/shiyudesu/frux/internal/infra/persistence/adminaudit"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestReviewRepositoryPostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}
	t.Run("backfills pending human priority", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "priority_backfill")
		model := CaseModel{
			VideoID: 9001, ReviewVersion: 1, Status: domainreview.CaseStatusPendingHuman,
			PolicyVersion: 1, Priority: 0, Version: 1, CreatedAt: time.Now().UTC(),
		}
		if err := db.Create(&model).Error; err != nil {
			t.Fatalf("create legacy pending-human case: %v", err)
		}
		if err := EnsureHumanReviewPriorities(db); err != nil {
			t.Fatalf("backfill pending-human priority: %v", err)
		}
		if err := db.Where("id = ?", model.ID).Take(&model).Error; err != nil {
			t.Fatalf("reload pending-human case: %v", err)
		}
		if model.Priority != 1 {
			t.Fatalf("priority = %d, want 1", model.Priority)
		}
	})
	t.Run("duplicate intake and result", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "duplicate")
		insertReviewVideo(t, db, 101, 1)
		repo := New(db)
		first, created, err := repo.CreateOrGetCase(context.Background(), 101)
		if err != nil || !created {
			t.Fatalf("first intake = %#v created=%v err=%v", first, created, err)
		}
		second, created, err := repo.CreateOrGetCase(context.Background(), 101)
		if err != nil || created || second.ID != first.ID {
			t.Fatalf("duplicate intake = %#v created=%v err=%v", second, created, err)
		}
		setReviewPolicy(t, db, domainreview.PolicyConfiguration{
			DefaultOutcome: domainreview.OutcomeApprove,
			Rules:          []domainreview.LabelRule{{Label: domainreview.LabelSafe}},
		})
		result := newPostgresMachineResult(t, first, "result-1", []domainreview.MachineSignal{{
			Label: domainreview.LabelSafe, Confidence: 0.99, EvidenceRefs: []string{"frame://1"},
		}})
		processed, err := repo.ProcessMachineResult(context.Background(), result)
		if err != nil || processed.Duplicate || processed.Decision.Outcome != domainreview.OutcomeApprove {
			t.Fatalf("first result = %#v err=%v", processed, err)
		}
		replayed, err := repo.ProcessMachineResult(context.Background(), result)
		if err != nil || !replayed.Duplicate || replayed.Decision.ID != processed.Decision.ID {
			t.Fatalf("duplicate result = %#v err=%v", replayed, err)
		}
		if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", 101).Update("review_version", 2).Error; err != nil {
			t.Fatal(err)
		}
		staleReplay, err := repo.ProcessMachineResult(context.Background(), result)
		if err != nil || !staleReplay.Duplicate || staleReplay.ApplySideEffects {
			t.Fatalf("stale duplicate side effects = %#v err=%v", staleReplay, err)
		}
		assertReviewCounts(t, db, 1, 1, 1)
		var video infravideo.VideoModel
		if err := db.Where("id = ?", 101).Take(&video).Error; err != nil {
			t.Fatal(err)
		}
		if video.Status != domainvideo.StatusPublished || video.PublishedAt == nil {
			t.Fatalf("video outcome = %#v", video)
		}
		assertReviewNotification(
			t, db, domainmessage.PublicationEventID(101, 1),
			domainmessage.LifecycleStagePublished, domainmessage.LifecycleResultPublic,
		)
		var publicationReady bool
		if err := db.Model(&infravideo.NotificationOutboxModel{}).
			Select("delivery_ready").
			Where("event_id = ?", domainmessage.PublicationEventID(101, 1)).
			Scan(&publicationReady).Error; err != nil || !publicationReady {
			t.Fatalf("legacy publication ready=%v err=%v", publicationReady, err)
		}
		var event infravideo.PublicationEventOutboxModel
		if err := db.Where("event_id = ?", domainmessage.PublicationEventID(101, 1)).
			Take(&event).Error; err != nil || !event.DeliveryReady {
			t.Fatalf("legacy publication event=%#v err=%v", event, err)
		}
	})

	t.Run("disabled policy blocks new result", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "disabled_policy")
		insertReviewVideo(t, db, 105, 1)
		repo := New(db)
		reviewCase, _, err := repo.CreateOrGetCase(context.Background(), 105)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&PolicyModel{}).Where("version = ?", reviewCase.PolicyVersion).Update("enabled", false).Error; err != nil {
			t.Fatal(err)
		}
		result := newPostgresMachineResult(t, reviewCase, "disabled-result", []domainreview.MachineSignal{{
			Label: domainreview.LabelSafe, Confidence: 1,
		}})
		if _, err := repo.ProcessMachineResult(context.Background(), result); !errors.Is(err, domainreview.ErrReviewPolicyNotFound) {
			t.Fatalf("disabled policy error = %v", err)
		}
		assertReviewCounts(t, db, 0, 0, 0)
	})

	t.Run("approval before media readiness is notified without publication", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "approval_before_media")
		insertReviewVideo(t, db, 106, 1)
		if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", 106).
			Update("media_status", domainmedia.MediaStatusProcessing).Error; err != nil {
			t.Fatal(err)
		}
		setReviewPolicy(t, db, domainreview.PolicyConfiguration{
			DefaultOutcome: domainreview.OutcomeApprove,
			Rules:          []domainreview.LabelRule{{Label: domainreview.LabelSafe}},
		})

		t.Run("production approval creates a pending publication readiness marker", func(t *testing.T) {
			db := openReviewPostgres(t, dsn, "production_approval_marker")
			insertReviewVideo(t, db, 109, 1)
			if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", 109).
				Update("media_asset_id", 9001).Error; err != nil {
				t.Fatal(err)
			}
			setReviewPolicy(t, db, domainreview.PolicyConfiguration{
				DefaultOutcome: domainreview.OutcomeApprove,
				Rules:          []domainreview.LabelRule{{Label: domainreview.LabelSafe}},
			})
			repo := New(db)
			reviewCase, _, err := repo.CreateOrGetCase(context.Background(), 109)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.ProcessMachineResult(
				context.Background(),
				newPostgresMachineResult(t, reviewCase, "production-approve", []domainreview.MachineSignal{{
					Label: domainreview.LabelSafe, Confidence: 0.99,
				}}),
			); err != nil {
				t.Fatal(err)
			}
			var marker infravideo.NotificationOutboxModel
			if err := db.Where(
				"event_id = ?", domainmessage.PublicationEventID(109, 1),
			).Take(&marker).Error; err != nil {
				t.Fatal(err)
			}
			if marker.DeliveryReady {
				t.Fatal("review transaction marked production media publication ready")
			}
		})
		repo := New(db)
		reviewCase, created, err := repo.CreateOrGetCase(context.Background(), 106)
		if err != nil || !created {
			t.Fatalf("intake=%#v created=%v err=%v", reviewCase, created, err)
		}
		processed, err := repo.ProcessMachineResult(
			context.Background(),
			newPostgresMachineResult(t, reviewCase, "processing-approve", []domainreview.MachineSignal{{
				Label: domainreview.LabelSafe, Confidence: 0.99,
			}}),
		)
		if err != nil || processed.Decision.Outcome != domainreview.OutcomeApprove {
			t.Fatalf("approval=%#v err=%v", processed, err)
		}
		assertReviewNotification(
			t, db, domainmessage.ReviewEventID(
				106, 1, domainmessage.LifecycleResultApproved,
			),
			domainmessage.LifecycleStageReview, domainmessage.LifecycleResultApproved,
		)
		var published int64
		if err := db.Model(&NotificationOutboxModel{}).
			Where("event_id = ?", domainmessage.PublicationEventID(106, 1)).
			Count(&published).Error; err != nil || published != 0 {
			t.Fatalf("premature publication count=%d err=%v", published, err)
		}
	})

	t.Run("same identity different payload conflicts", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "identity_conflict")
		insertReviewVideo(t, db, 102, 1)
		repo := New(db)
		reviewCase, _, err := repo.CreateOrGetCase(context.Background(), 102)
		if err != nil {
			t.Fatal(err)
		}
		first := newPostgresMachineResult(t, reviewCase, "result-conflict", []domainreview.MachineSignal{{Label: domainreview.LabelSafe, Confidence: 0.7}})
		if _, err := repo.ProcessMachineResult(context.Background(), first); err != nil {
			t.Fatal(err)
		}
		changed := newPostgresMachineResult(t, reviewCase, "result-conflict", []domainreview.MachineSignal{{Label: domainreview.LabelSafe, Confidence: 0.8}})
		if _, err := repo.ProcessMachineResult(context.Background(), changed); !errors.Is(err, domainreview.ErrResultIdentityConflict) {
			t.Fatalf("identity conflict error = %v", err)
		}
		assertReviewCounts(t, db, 1, 1, 1)
	})

	t.Run("unregistered automated reject reason falls back safely", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "safe_reject_reason")
		insertReviewVideo(t, db, 108, 1)
		reject := 0.5
		setReviewPolicy(t, db, domainreview.PolicyConfiguration{
			DefaultOutcome: domainreview.OutcomeHuman,
			Rules: []domainreview.LabelRule{{
				Label: domainreview.LabelSafe, RejectThreshold: &reject,
			}},
		})
		repo := New(db)
		reviewCase, _, err := repo.CreateOrGetCase(context.Background(), 108)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.ProcessMachineResult(
			context.Background(),
			newPostgresMachineResult(t, reviewCase, "safe-reject", []domainreview.MachineSignal{{
				Label: domainreview.LabelSafe, Confidence: 0.9,
			}}),
		); err != nil {
			t.Fatal(err)
		}
		var notification NotificationOutboxModel
		if err := db.Where(
			"event_id = ?",
			domainmessage.ReviewEventID(108, 1, domainmessage.LifecycleResultRejected),
		).Take(&notification).Error; err != nil {
			t.Fatal(err)
		}
		if notification.ReasonCode != domainreview.ReasonRejectOther {
			t.Fatalf("reason code = %q", notification.ReasonCode)
		}
	})

	t.Run("automated outcome rolls back when notification outbox fails", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "notification_rollback")
		insertReviewVideo(t, db, 107, 1)
		setReviewPolicy(t, db, domainreview.PolicyConfiguration{
			DefaultOutcome: domainreview.OutcomeApprove,
			Rules:          []domainreview.LabelRule{{Label: domainreview.LabelSafe}},
		})
		repo := New(db)
		reviewCase, _, err := repo.CreateOrGetCase(context.Background(), 107)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`
			ALTER TABLE review_notification_outbox
			ADD CONSTRAINT reject_published_notification CHECK (stage <> 'published')
		`).Error; err != nil {
			t.Fatal(err)
		}
		_, err = repo.ProcessMachineResult(
			context.Background(),
			newPostgresMachineResult(t, reviewCase, "notification-rollback", []domainreview.MachineSignal{{
				Label: domainreview.LabelSafe, Confidence: 0.99,
			}}),
		)
		if err == nil {
			t.Fatal("expected notification insertion failure")
		}
		var video infravideo.VideoModel
		if err := db.Where("id = ?", 107).Take(&video).Error; err != nil {
			t.Fatal(err)
		}
		if video.Status != domainvideo.StatusPendingReview || video.PublishedAt != nil {
			t.Fatalf("video changed despite notification rollback: %#v", video)
		}
		assertReviewCounts(t, db, 0, 0, 0)
		var notifications int64
		if err := db.Model(&NotificationOutboxModel{}).Count(&notifications).Error; err != nil ||
			notifications != 0 {
			t.Fatalf("notification count=%d err=%v", notifications, err)
		}
	})

	t.Run("stale version rolls back evidence", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "stale")
		insertReviewVideo(t, db, 103, 1)
		repo := New(db)
		reviewCase, _, err := repo.CreateOrGetCase(context.Background(), 103)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", 103).Update("review_version", 2).Error; err != nil {
			t.Fatal(err)
		}
		result := newPostgresMachineResult(t, reviewCase, "stale-result", []domainreview.MachineSignal{{Label: domainreview.LabelSafe, Confidence: 1}})
		if _, err := repo.ProcessMachineResult(context.Background(), result); !errors.Is(err, domainreview.ErrReviewSubjectStale) {
			t.Fatalf("stale result error = %v", err)
		}
		assertReviewCounts(t, db, 0, 0, 0)
	})

	t.Run("reject transition is atomic", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "reject")
		insertReviewVideo(t, db, 104, 1)
		repo := New(db)
		reviewCase, _, err := repo.CreateOrGetCase(context.Background(), 104)
		if err != nil {
			t.Fatal(err)
		}
		reject := 0.8
		setReviewPolicy(t, db, domainreview.PolicyConfiguration{
			DefaultOutcome: domainreview.OutcomeApprove,
			Rules: []domainreview.LabelRule{{
				Label: domainreview.LabelHate, RejectThreshold: &reject,
			}},
		})
		result := newPostgresMachineResult(t, reviewCase, "reject-result", []domainreview.MachineSignal{{Label: domainreview.LabelHate, Confidence: 0.9}})
		processed, err := repo.ProcessMachineResult(context.Background(), result)
		if err != nil || processed.Decision.Outcome != domainreview.OutcomeReject ||
			processed.Case.Status != domainreview.CaseStatusRejected {
			t.Fatalf("reject result = %#v err=%v", processed, err)
		}
		var video infravideo.VideoModel
		if err := db.Where("id = ?", 104).Take(&video).Error; err != nil {
			t.Fatal(err)
		}
		if video.Status != domainvideo.StatusRejected || video.PublishedAt != nil {
			t.Fatalf("rejected video = %#v", video)
		}
		assertReviewNotification(
			t, db, domainmessage.ReviewEventID(
				104, 1, domainmessage.LifecycleResultRejected,
			),
			domainmessage.LifecycleStageReview, domainmessage.LifecycleResultRejected,
		)
		assertReviewCounts(t, db, 1, 1, 1)
	})

	t.Run("human priority is persisted and orders queue", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "human_priority")
		human := 0.1
		setReviewPolicy(t, db, domainreview.PolicyConfiguration{
			DefaultOutcome: domainreview.OutcomeApprove,
			Rules: []domainreview.LabelRule{{
				Label: domainreview.LabelHate, HumanThreshold: &human,
			}},
		})
		repo := New(db)
		for _, item := range []struct {
			id         int64
			confidence float64
		}{
			{151, 0.25},
			{152, 0.85},
		} {
			insertReviewVideo(t, db, item.id, 1)
			reviewCase, _, err := repo.CreateOrGetCase(context.Background(), item.id)
			if err != nil {
				t.Fatal(err)
			}
			processed, err := repo.ProcessMachineResult(
				context.Background(),
				newPostgresMachineResult(t, reviewCase, fmt.Sprintf("priority-%d", item.id), []domainreview.MachineSignal{{
					Label: domainreview.LabelHate, Confidence: item.confidence,
				}}),
			)
			if err != nil || processed.Case.Priority == 0 {
				t.Fatalf("priority result %d = %#v err=%v", item.id, processed, err)
			}
		}
		queue, err := repo.ListHumanQueue(context.Background(), domainreview.HumanQueueFilter{
			MinPriority: 0, MaxPriority: 100, Limit: 10,
		})
		if err != nil || len(queue) != 2 || queue[0].Case.VideoID != 152 ||
			queue[0].Case.Priority <= queue[1].Case.Priority {
			t.Fatalf("priority queue = %#v err=%v", queue, err)
		}
	})

	t.Run("stable queue concurrent claim and expired recovery", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "human_queue")
		now := time.Now().UTC().Truncate(time.Microsecond)
		for _, item := range []struct {
			id       int64
			priority int
			created  time.Time
		}{
			{201, 90, now.Add(-time.Hour)},
			{202, 90, now.Add(-30 * time.Minute)},
			{203, 10, now.Add(-2 * time.Hour)},
		} {
			insertReviewVideo(t, db, item.id, 1)
			if err := db.Create(&CaseModel{
				ID: item.id, VideoID: item.id, ReviewVersion: 1, Status: domainreview.CaseStatusPendingHuman,
				PolicyVersion: 1, Priority: item.priority, Version: 1,
				CreatedAt: item.created, UpdatedAt: item.created,
			}).Error; err != nil {
				t.Fatal(err)
			}
		}
		repo := New(db)
		firstPage, err := repo.ListHumanQueue(context.Background(), domainreview.HumanQueueFilter{
			MinPriority: 0, MaxPriority: 100, Limit: 2,
		})
		if err != nil || len(firstPage) != 2 || firstPage[0].Case.ID != 201 || firstPage[1].Case.ID != 202 {
			t.Fatalf("queue page = %#v err=%v", firstPage, err)
		}
		next, err := repo.ListHumanQueue(context.Background(), domainreview.HumanQueueFilter{
			MinPriority: 0, MaxPriority: 100, Limit: 2,
			Cursor: &domainreview.QueueCursor{
				Scope:    domainreview.HumanQueueScopeAvailable,
				Priority: firstPage[1].Case.Priority, SortTime: firstPage[1].Case.CreatedAt,
				CaseID: firstPage[1].Case.ID,
			},
		})
		if err != nil || len(next) != 1 || next[0].Case.ID != 203 {
			t.Fatalf("next queue page = %#v err=%v", next, err)
		}

		type claimResult struct {
			reviewer int64
			err      error
		}
		results := make(chan claimResult, 2)
		var wait sync.WaitGroup
		for reviewer := int64(1); reviewer <= 2; reviewer++ {
			wait.Add(1)
			go func(reviewerID int64) {
				defer wait.Done()
				tokenByte := "a"
				if reviewerID == 2 {
					tokenByte = "b"
				}
				_, claimErr := repo.ClaimHumanCase(
					context.Background(), 201, reviewerID, strings.Repeat(tokenByte, 64),
					1,
					domainreview.DefaultHumanLeaseDuration,
				)
				results <- claimResult{reviewer: reviewerID, err: claimErr}
			}(reviewer)
		}
		wait.Wait()
		close(results)
		successes := 0
		for result := range results {
			if result.err == nil {
				successes++
			} else if !errors.Is(result.err, domainreview.ErrReviewCaseClaimed) {
				t.Fatalf("reviewer %d claim error = %v", result.reviewer, result.err)
			}
		}
		if successes != 1 {
			t.Fatalf("successful claims = %d", successes)
		}
		if err := db.Model(&CaseModel{}).Where("id = ?", 201).
			Update("lease_expires_at", now.Add(-time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
		expiredPage, err := repo.ListHumanQueue(context.Background(), domainreview.HumanQueueFilter{
			MinPriority: 0, MaxPriority: 100, Limit: 3,
		})
		if err != nil || len(expiredPage) != 3 || expiredPage[0].Case.ID != 201 ||
			expiredPage[0].Case.AssignedReviewerID != 0 || expiredPage[0].Case.LeaseExpiresAt != nil {
			t.Fatalf("expired lease queue = %#v err=%v", expiredPage, err)
		}
		reclaimed, err := repo.ClaimHumanCase(
			context.Background(), 201, 9, strings.Repeat("c", 64),
			expiredPage[0].Case.Version, domainreview.DefaultHumanLeaseDuration,
		)
		if err != nil || reclaimed.Version != 4 || reclaimed.AssignedReviewerID != 9 {
			t.Fatalf("expired lease reclaim = %#v err=%v", reclaimed, err)
		}
		if _, err := repo.ClaimHumanCase(
			context.Background(), 202, 9, strings.Repeat("e", 64),
			1, domainreview.DefaultHumanLeaseDuration,
		); err != nil {
			t.Fatalf("claim second reviewer task: %v", err)
		}
		mine, err := repo.ListHumanAssigned(context.Background(), domainreview.HumanQueueFilter{
			Scope: domainreview.HumanQueueScopeMine, ReviewerID: 9,
			MinPriority: 0, MaxPriority: 100, Limit: 1,
		})
		if err != nil || len(mine) != 1 || mine[0].Case.ID != 201 || mine[0].SnapshotAt.IsZero() {
			t.Fatalf("reviewer work = %#v err=%v", mine, err)
		}
		resumed, err := repo.ResumeHumanLease(
			context.Background(), 201, 9, strings.Repeat("d", 64),
			4, domainreview.DefaultHumanLeaseDuration,
		)
		if err != nil || resumed.Version != 5 || resumed.LeaseTokenHash != strings.Repeat("d", 64) {
			t.Fatalf("resumed lease = %#v err=%v", resumed, err)
		}
		nextMine, err := repo.ListHumanAssigned(context.Background(), domainreview.HumanQueueFilter{
			Scope: domainreview.HumanQueueScopeMine, ReviewerID: 9,
			MinPriority: 0, MaxPriority: 100, Limit: 10,
			Cursor: &domainreview.QueueCursor{
				Scope: domainreview.HumanQueueScopeMine, Priority: mine[0].Case.Priority,
				SortTime:    mine[0].Case.LeaseExpiresAt.UTC(),
				SnapshotAt:  resumed.UpdatedAt.Add(time.Second),
				SeenCaseIDs: []int64{mine[0].Case.ID}, CaseID: mine[0].Case.ID,
			},
		})
		if err != nil || len(nextMine) != 1 || nextMine[0].Case.ID != 202 {
			t.Fatalf("snapshot reviewer work = %#v err=%v", nextMine, err)
		}
		if _, err := repo.RenewHumanLease(
			context.Background(), 201, 9, strings.Repeat("c", 64),
			5, domainreview.DefaultHumanLeaseDuration,
		); !errors.Is(err, domainreview.ErrReviewLeaseNotOwned) {
			t.Fatalf("old token renew error = %v", err)
		}
		var expiredEvents int64
		if err := db.Model(&AssignmentModel{}).
			Where("case_id = ? AND event = ?", 201, domainreview.AssignmentEventExpired).
			Count(&expiredEvents).Error; err != nil || expiredEvents != 1 {
			t.Fatalf("expired events = %d err=%v", expiredEvents, err)
		}
	})

	t.Run("terminal and superseded subjects retire once", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "human_stale_subjects")
		now := time.Now().UTC().Truncate(time.Microsecond)
		for _, id := range []int64{221, 222, 223} {
			insertReviewVideo(t, db, id, 1)
			if err := db.Create(&CaseModel{
				ID: id, VideoID: id, ReviewVersion: 1, Status: domainreview.CaseStatusPendingHuman,
				PolicyVersion: 1, Priority: 50, Version: 1,
				CreatedAt: now, UpdatedAt: now,
			}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", 221).
			Update("status", domainvideo.StatusDeleted).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", 222).
			Update("review_version", 2).Error; err != nil {
			t.Fatal(err)
		}
		repo := New(db)
		queue, err := repo.ListHumanQueue(context.Background(), domainreview.HumanQueueFilter{
			MinPriority: 0, MaxPriority: 100, Limit: 10,
		})
		if err != nil || len(queue) != 1 || queue[0].Case.ID != 223 {
			t.Fatalf("stale subject queue = %#v err=%v", queue, err)
		}
		available, _, err := repo.HumanQueueStats(context.Background(), 0, 100)
		if err != nil || available != 1 {
			t.Fatalf("stale subject stats = %d err=%v", available, err)
		}

		tests := []struct {
			caseID       int64
			firstErr     error
			status       string
			historyEvent string
		}{
			{221, domainreview.ErrReviewSubjectState, domainreview.CaseStatusCancelled, domainreview.AssignmentEventCancelled},
			{222, domainreview.ErrReviewSubjectStale, domainreview.CaseStatusSuperseded, domainreview.AssignmentEventSuperseded},
		}
		for _, test := range tests {
			if _, err := repo.ClaimHumanCase(
				context.Background(), test.caseID, 9, strings.Repeat("c", 64),
				1, domainreview.DefaultHumanLeaseDuration,
			); !errors.Is(err, test.firstErr) {
				t.Fatalf("first claim for %d error = %v", test.caseID, err)
			}
			if _, err := repo.ClaimHumanCase(
				context.Background(), test.caseID, 9, strings.Repeat("d", 64),
				2, domainreview.DefaultHumanLeaseDuration,
			); !errors.Is(err, domainreview.ErrReviewCaseNotHuman) {
				t.Fatalf("repeat claim for %d error = %v", test.caseID, err)
			}
			var caseModel CaseModel
			if err := db.Where("id = ?", test.caseID).Take(&caseModel).Error; err != nil {
				t.Fatal(err)
			}
			if caseModel.Status != test.status || caseModel.Version != 2 ||
				caseModel.AssignedReviewerID != 0 || caseModel.LeaseExpiresAt != nil ||
				caseModel.ClosedAt == nil {
				t.Fatalf("retired case %d = %#v", test.caseID, caseModel)
			}
			var history []AssignmentModel
			if err := db.Where("case_id = ?", test.caseID).Order("id ASC").Find(&history).Error; err != nil {
				t.Fatal(err)
			}
			if len(history) != 1 || history[0].Event != test.historyEvent ||
				history[0].CaseVersion != 2 {
				t.Fatalf("retirement history for %d = %#v", test.caseID, history)
			}
		}
	})

	t.Run("lease time is sampled after row locks", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "lease_lock_time")
		now := time.Now().UTC().Truncate(time.Microsecond)
		insertReviewVideo(t, db, 251, 1)
		if err := db.Create(&CaseModel{
			ID: 251, VideoID: 251, ReviewVersion: 1, Status: domainreview.CaseStatusPendingHuman,
			PolicyVersion: 1, Priority: 50, Version: 1, AssignedReviewerID: 4,
			LeaseTokenHash: strings.Repeat("d", 64), LeaseExpiresAt: ptrTime(now.Add(time.Minute)),
			CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&CaseModel{}).Where("id = ?", 251).
			Update("lease_expires_at", gorm.Expr("clock_timestamp() + interval '2 seconds'")).Error; err != nil {
			t.Fatal(err)
		}
		blocker := db.Begin()
		if blocker.Error != nil {
			t.Fatal(blocker.Error)
		}
		if err := blocker.Exec("SELECT id FROM review_case WHERE id = ? FOR UPDATE", 251).Error; err != nil {
			t.Fatal(err)
		}
		repo := New(db)
		result := make(chan error, 1)
		go func() {
			_, err := repo.ClaimHumanCase(
				context.Background(), 251, 9, strings.Repeat("e", 64),
				1, domainreview.DefaultHumanLeaseDuration,
			)
			result <- err
		}()
		time.Sleep(2200 * time.Millisecond)
		if err := blocker.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		if err := <-result; err != nil {
			t.Fatalf("claim after blocked expiry = %v", err)
		}
	})

	t.Run("human decision stale version and audit rollback", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "human_decision")
		insertReviewVideo(t, db, 301, 1)
		now := time.Now().UTC().Truncate(time.Microsecond)
		if err := db.Create(&CaseModel{
			ID: 301, VideoID: 301, ReviewVersion: 1, Status: domainreview.CaseStatusPendingHuman,
			PolicyVersion: 1, Priority: 50, Version: 1, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		auditRepo := infraadminaudit.New(db)
		repo := New(db, WithAuditWriter(auditRepo))
		tokenHash := strings.Repeat("a", 64)
		claimed, err := repo.ClaimHumanCase(
			context.Background(), 301, 7, tokenHash, 1, domainreview.DefaultHumanLeaseDuration,
		)
		if err != nil {
			t.Fatal(err)
		}
		decision := newPostgresHumanDecision(
			t, claimed, 7, domainreview.OutcomeApprove, domainreview.ReasonApproveCompliant, "approve-301",
		)
		fact := newPostgresReviewAuditFact(t, decision, "approve-301")
		if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", 301).
			Update("review_version", 2).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := repo.CommitHumanDecision(context.Background(), decision, tokenHash, fact); !errors.Is(err, domainreview.ErrReviewSubjectStale) {
			t.Fatalf("stale decision error = %v", err)
		}
		var decisions int64
		if err := db.Model(&HumanDecisionModel{}).Count(&decisions).Error; err != nil || decisions != 0 {
			t.Fatalf("stale decision count = %d err=%v", decisions, err)
		}

		if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", 301).
			Update("review_version", 1).Error; err != nil {
			t.Fatal(err)
		}
		failingRepo := New(db, WithAuditWriter(failingReviewAuditWriter{}))
		if _, err := failingRepo.CommitHumanDecision(context.Background(), decision, tokenHash, fact); err == nil {
			t.Fatal("expected audit failure")
		}
		for _, model := range []any{
			&HumanDecisionModel{}, &HumanDecisionIdempotencyModel{}, &NotificationOutboxModel{},
		} {
			var count int64
			if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("rollback count for %T = %d err=%v", model, count, err)
			}
		}
		var caseAfter CaseModel
		if err := db.Where("id = ?", 301).Take(&caseAfter).Error; err != nil {
			t.Fatal(err)
		}
		if caseAfter.Status != domainreview.CaseStatusPendingHuman || caseAfter.AssignedReviewerID != 7 {
			t.Fatalf("case changed after audit failure = %#v", caseAfter)
		}
		committed, err := repo.CommitHumanDecision(context.Background(), decision, tokenHash, fact)
		if err != nil || committed.Duplicate || committed.Case.Status != domainreview.CaseStatusApproved {
			t.Fatalf("committed decision = %#v err=%v", committed, err)
		}
		assertReviewNotification(
			t, db, domainmessage.PublicationEventID(301, 1),
			domainmessage.LifecycleStagePublished, domainmessage.LifecycleResultPublic,
		)
		recent, err := repo.ListHumanRecent(context.Background(), domainreview.HumanQueueFilter{
			Scope: domainreview.HumanQueueScopeRecent, ReviewerID: 7,
			MinPriority: 0, MaxPriority: 100, Limit: 10,
		})
		if err != nil || len(recent) != 1 || recent[0].Case.ID != 301 {
			t.Fatalf("recent decisions = %#v err=%v", recent, err)
		}
		replayed, err := repo.CommitHumanDecision(context.Background(), decision, tokenHash, fact)
		if err != nil || !replayed.Duplicate || replayed.Decision.ID != committed.Decision.ID ||
			!replayed.ApplySideEffects {
			t.Fatalf("replayed decision = %#v err=%v", replayed, err)
		}
		if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", 301).
			Update("review_version", 2).Error; err != nil {
			t.Fatal(err)
		}
		staleReplay, err := repo.CommitHumanDecision(context.Background(), decision, tokenHash, fact)
		if err != nil || !staleReplay.Duplicate || staleReplay.ApplySideEffects {
			t.Fatalf("stale replay side effects = %#v err=%v", staleReplay, err)
		}
		changed := newPostgresHumanDecision(
			t, claimed, 7, domainreview.OutcomeApprove, domainreview.ReasonApproveFalsePositive, "approve-301",
		)
		if _, err := repo.CommitHumanDecision(context.Background(), changed, tokenHash, fact); !errors.Is(err, domainreview.ErrDecisionIdentityConflict) {
			t.Fatalf("decision payload conflict = %v", err)
		}
		for _, model := range []any{
			&HumanDecisionModel{}, &HumanDecisionIdempotencyModel{},
			&NotificationOutboxModel{}, &infraadminaudit.EventModel{},
		} {
			var count int64
			if err := db.Model(model).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("committed count for %T = %d err=%v", model, count, err)
			}
		}
		var videoAfter infravideo.VideoModel
		if err := db.Where("id = ?", 301).Take(&videoAfter).Error; err != nil {
			t.Fatal(err)
		}
		if videoAfter.Status != domainvideo.StatusPublished || videoAfter.PublishedAt == nil {
			t.Fatalf("video after decision = %#v", videoAfter)
		}
	})

	t.Run("decision checks lease after video lock", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "decision_lock_time")
		insertReviewVideo(t, db, 401, 1)
		now := time.Now().UTC().Truncate(time.Microsecond)
		if err := db.Create(&CaseModel{
			ID: 401, VideoID: 401, ReviewVersion: 1, Status: domainreview.CaseStatusPendingHuman,
			PolicyVersion: 1, Priority: 50, Version: 1, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		auditRepo := infraadminaudit.New(db)
		repo := New(db, WithAuditWriter(auditRepo))
		tokenHash := strings.Repeat("f", 64)
		claimed, err := repo.ClaimHumanCase(
			context.Background(), 401, 7, tokenHash, 1, domainreview.DefaultHumanLeaseDuration,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&CaseModel{}).Where("id = ?", 401).
			Update("lease_expires_at", gorm.Expr("clock_timestamp() + interval '2 seconds'")).Error; err != nil {
			t.Fatal(err)
		}
		decision := newPostgresHumanDecision(
			t, claimed, 7, domainreview.OutcomeApprove, domainreview.ReasonApproveCompliant, "approve-401",
		)
		fact := newPostgresReviewAuditFact(t, decision, "approve-401")
		blocker := db.Begin()
		if blocker.Error != nil {
			t.Fatal(blocker.Error)
		}
		if err := blocker.Exec("SELECT id FROM video WHERE id = ? FOR UPDATE", 401).Error; err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() {
			_, commitErr := repo.CommitHumanDecision(context.Background(), decision, tokenHash, fact)
			result <- commitErr
		}()
		time.Sleep(2200 * time.Millisecond)
		if err := blocker.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		if err := <-result; !errors.Is(err, domainreview.ErrReviewLeaseExpired) {
			t.Fatalf("decision after blocked expiry = %v", err)
		}
	})
	t.Run("moderation jobs are atomic idempotent and leased by database time", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "moderation_jobs")
		insertReviewVideo(t, db, 501, 1)
		config := domainreview.ModerationJobConfig{
			Mode: domainreview.ModerationModeObserve, ProviderConfigVersion: 7,
			InputProfileVersion: "frames-v1", MaxAttempts: 3,
		}
		repo := New(db, WithModerationJobConfig(config))
		reviewCase, created, err := repo.CreateOrGetCase(context.Background(), 501)
		if err != nil || !created {
			t.Fatalf("intake = %#v created=%v err=%v", reviewCase, created, err)
		}
		if _, created, err = repo.CreateOrGetCase(context.Background(), 501); err != nil || created {
			t.Fatalf("duplicate intake created=%v err=%v", created, err)
		}
		var jobs []ModerationJobModel
		if err := db.Find(&jobs).Error; err != nil {
			t.Fatal(err)
		}
		if len(jobs) != 1 ||
			jobs[0].ResultID != domainreview.ModerationResultID(reviewCase.ID, 1, 7) {
			t.Fatalf("jobs = %#v", jobs)
		}
		first, err := repo.ClaimModerationJobs(context.Background(), "worker-a", 1, time.Minute)
		if err != nil || len(first) != 1 || first[0].Attempts != 1 {
			t.Fatalf("first claim = %#v err=%v", first, err)
		}
		if err := repo.RenewModerationJobLease(
			context.Background(), first[0].ID, "worker-a", 2*time.Minute,
		); err != nil {
			t.Fatalf("renew lease: %v", err)
		}
		second, err := repo.ClaimModerationJobs(context.Background(), "worker-b", 1, time.Minute)
		if err != nil || len(second) != 0 {
			t.Fatalf("concurrent claim = %#v err=%v", second, err)
		}
		if err := db.Model(&ModerationJobModel{}).Where("id = ?", jobs[0].ID).
			Update("lease_until", gorm.Expr("clock_timestamp() - interval '1 second'")).Error; err != nil {
			t.Fatal(err)
		}
		second, err = repo.ClaimModerationJobs(context.Background(), "worker-b", 1, time.Minute)
		if err != nil || len(second) != 1 || second[0].Attempts != 2 ||
			second[0].RequestID != first[0].RequestID {
			t.Fatalf("expired claim = %#v err=%v", second, err)
		}
		if err := db.Model(&ModerationJobModel{}).Where("id = ?", jobs[0].ID).
			Update("lease_until", gorm.Expr("clock_timestamp() - interval '1 second'")).Error; err != nil {
			t.Fatal(err)
		}
		if err := repo.MarkModerationJobSubmitted(
			context.Background(), second[0].ID, "worker-b", time.Now(),
		); !errors.Is(err, domainreview.ErrModerationJobNotOwned) {
			t.Fatalf("expired owner mutation error = %v", err)
		}
		expiredResult, err := domainreview.NewMachineResult(domainreview.MachineResultInput{
			CaseID: reviewCase.ID, VideoID: 501, ReviewVersion: 1,
			ResultID: second[0].ResultID, Provider: "test-provider", ModelVersion: "v1",
			SourceKind:  domainreview.MachineSourceProductionProvider,
			GeneratedAt: time.Now(), RolloutMode: domainreview.ModerationModeObserve,
			ModerationJobID: second[0].ID, ModerationLeaseOwner: "worker-b",
			PolicyVersion: 1,
			Signals: []domainreview.MachineSignal{{
				Label: domainreview.LabelSafe, Confidence: 1,
			}},
			ReceivedAt: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.ProcessMachineResult(
			context.Background(), expiredResult,
		); !errors.Is(err, domainreview.ErrModerationJobNotOwned) {
			t.Fatalf("expired result commit error = %v", err)
		}
	})
	t.Run("moderation reconciliation recovers leases and cancels stale subjects", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "moderation_reconcile")
		insertReviewVideo(t, db, 502, 1)
		config := domainreview.ModerationJobConfig{
			Mode: domainreview.ModerationModeEnforce, ProviderConfigVersion: 2,
			InputProfileVersion: "frames-v1", MaxAttempts: 3,
		}
		repo := New(db, WithModerationJobConfig(config))
		reviewCase, _, err := repo.CreateOrGetCase(context.Background(), 502)
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := repo.ClaimModerationJobs(context.Background(), "worker", 1, time.Second)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("claim = %#v err=%v", claimed, err)
		}
		if err := db.Model(&ModerationJobModel{}).Where("case_id = ?", reviewCase.ID).
			Update("lease_until", gorm.Expr("clock_timestamp() - interval '1 second'")).Error; err != nil {
			t.Fatal(err)
		}
		stats, err := repo.ReconcileModerationJobs(context.Background(), config, 100)
		if err != nil || stats.RecoveredLeases != 1 {
			t.Fatalf("recovery stats = %#v err=%v", stats, err)
		}
		if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", 502).
			Update("status", domainvideo.StatusPublished).Error; err != nil {
			t.Fatal(err)
		}
		stats, err = repo.ReconcileModerationJobs(context.Background(), config, 100)
		if err != nil || stats.Cancelled != 1 {
			t.Fatalf("cancel stats = %#v err=%v", stats, err)
		}
		var job ModerationJobModel
		if err := db.Where("case_id = ?", reviewCase.ID).Take(&job).Error; err != nil {
			t.Fatal(err)
		}
		if job.Status != domainreview.ModerationJobCancelled {
			t.Fatalf("job status = %q", job.Status)
		}
	})
	t.Run("machine provenance backfill classifies seed and legacy rows", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "provenance_backfill")
		now := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
		results := []ResultModel{
			{CaseID: 1, VideoID: 1, ReviewVersion: 1, Provider: " manual-seed ",
				ResultID: "seed", PayloadHash: strings.Repeat("a", 64), ModelVersion: "v1",
				PolicyVersion: 1, Outcome: domainreview.OutcomeHuman, CreatedAt: now},
			{CaseID: 2, VideoID: 2, ReviewVersion: 1, Provider: "old-provider",
				ResultID: "legacy", PayloadHash: strings.Repeat("b", 64), ModelVersion: "v1",
				PolicyVersion: 1, Outcome: domainreview.OutcomeHuman, CreatedAt: now},
		}
		if err := db.Create(&results).Error; err != nil {
			t.Fatal(err)
		}
		for _, result := range results {
			if err := db.Create(&SignalModel{
				CaseID: result.CaseID, ResultReceiptID: result.ID, Label: domainreview.LabelSafe,
				Confidence: 1, EvidenceRefsJSON: "[]", Provider: result.Provider,
				ModelVersion: "v1", PolicyVersion: 1, CreatedAt: now,
			}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&DecisionModel{
				CaseID: result.CaseID, ResultReceiptID: result.ID,
				Outcome: domainreview.OutcomeHuman, PolicyVersion: 1, CreatedAt: now,
			}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := EnsureMachineResultProvenance(db); err != nil {
			t.Fatal(err)
		}
		var restored []ResultModel
		if err := db.Order("id ASC").Find(&restored).Error; err != nil {
			t.Fatal(err)
		}
		if restored[0].SourceKind != domainreview.MachineSourceTestSeed ||
			restored[1].SourceKind != domainreview.MachineSourceLegacyUnknown ||
			restored[0].RolloutMode != domainreview.ModerationModeEnforce ||
			!restored[0].GeneratedAt.Equal(now) {
			t.Fatalf("backfilled results = %#v", restored)
		}
	})
	t.Run("manual seed backfill preserves legacy replay identity", func(t *testing.T) {
		db := openReviewPostgres(t, dsn, "manual_seed_replay")
		insertReviewVideo(t, db, 503, 1)
		repo := New(db)
		reviewCase, _, err := repo.CreateOrGetCase(context.Background(), 503)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		incoming, err := domainreview.NewMachineResult(domainreview.MachineResultInput{
			CaseID: reviewCase.ID, VideoID: 503, ReviewVersion: 1,
			ResultID: "legacy-seed", Provider: "manual-seed", ModelVersion: "seed-v1",
			SourceKind: domainreview.MachineSourceTestSeed, GeneratedAt: now,
			RolloutMode: domainreview.ModerationModeEnforce, PolicyVersion: 1,
			Signals: []domainreview.MachineSignal{{
				Label: domainreview.LabelSafe, Confidence: 1,
			}},
			ReceivedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		receipt := ResultModel{
			CaseID: reviewCase.ID, VideoID: 503, ReviewVersion: 1,
			Provider: "manual-seed", ResultID: incoming.ResultID,
			PayloadHash: incoming.LegacyPayloadHash, ModelVersion: incoming.ModelVersion,
			SourceKind: domainreview.MachineSourceLegacyUnknown, GeneratedAt: now,
			RolloutMode:   domainreview.ModerationModeEnforce,
			PolicyVersion: 1, Outcome: domainreview.OutcomeHuman, CreatedAt: now,
		}
		if err := db.Create(&receipt).Error; err != nil {
			t.Fatal(err)
		}
		decision := DecisionModel{
			CaseID: reviewCase.ID, ResultReceiptID: receipt.ID,
			Outcome: domainreview.OutcomeHuman, PolicyVersion: 1,
			RolloutMode: domainreview.ModerationModeEnforce, CreatedAt: now,
		}
		if err := db.Create(&decision).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&receipt).Update("decision_id", decision.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := EnsureMachineResultProvenance(db); err != nil {
			t.Fatal(err)
		}
		processed, err := repo.ProcessMachineResult(context.Background(), incoming)
		if err != nil || !processed.Duplicate {
			t.Fatalf("legacy replay = %#v err=%v", processed, err)
		}
	})
}

func openReviewPostgres(t *testing.T, dsn, suffix string) *gorm.DB {
	t.Helper()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("frux_review_%s_%d", suffix, time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = admin.Close()
	})
	sqlDB, err := sql.Open("pgx", postgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&infravideo.VideoModel{}, &infravideo.VideoStatModel{}, &infravideo.UserContentStatModel{},
		&infravideo.NotificationOutboxModel{}, &infravideo.PublicationEventOutboxModel{},
		&CaseModel{}, &ResultModel{}, &SignalModel{}, &DecisionModel{}, &ModerationJobModel{}, &PolicyModel{},
		&AssignmentModel{}, &HumanDecisionModel{}, &HumanDecisionIdempotencyModel{},
		&NotificationOutboxModel{}, &infraadminaudit.EventModel{},
	); err != nil {
		t.Fatal(err)
	}

	if err := EnsurePolicyIndexes(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureInitialPolicy(db); err != nil {
		t.Fatal(err)
	}
	return db
}

type failingReviewAuditWriter struct{}

func (failingReviewAuditWriter) AppendInTransaction(context.Context, *gorm.DB, *domainadminaudit.Fact) error {
	return errors.New("forced audit failure")
}

func (failingReviewAuditWriter) RecordCommittedWrite(*domainadminaudit.Fact) {}

func newPostgresHumanDecision(
	t *testing.T,
	reviewCase *domainreview.ReviewCase,
	reviewerID int64,
	outcome, reasonCode, idempotencyKey string,
) *domainreview.HumanDecision {
	t.Helper()
	decision, err := domainreview.NewHumanDecision(domainreview.HumanDecisionInput{
		CaseID: reviewCase.ID, ReviewerID: reviewerID, Outcome: outcome, ReasonCode: reasonCode,
		ReviewVersion: reviewCase.ReviewVersion, ExpectedCaseVersion: reviewCase.Version,
		IdempotencyKey: idempotencyKey, DecidedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func newPostgresReviewAuditFact(
	t *testing.T,
	decision *domainreview.HumanDecision,
	idempotencyKey string,
) *domainadminaudit.Fact {
	t.Helper()
	keyHash, err := domainadminaudit.DigestIdempotencyKey(idempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := domainadminaudit.NewFact(domainadminaudit.FactInput{
		ActorID: decision.ReviewerID, Permission: domainaccount.PermissionReviewDecide,
		Action: domainadminaudit.ActionReviewDecide, TargetType: domainadminaudit.TargetReviewCase,
		TargetID: strconv.FormatInt(decision.CaseID, 10), Outcome: domainadminaudit.OutcomeSuccess,
		RequestID: domainadminaudit.NewRequestID(), IdempotencyKeyHash: keyHash,
		Detail: map[string]string{
			"decision": decisionAuditOutcome(decision.Outcome), "http_method": "POST",
			"reason_code": decision.ReasonCode, "review_version": strconv.Itoa(decision.ReviewVersion),
			"route": "/api/admin/review/cases/:caseId/decision",
		},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return fact
}

func decisionAuditOutcome(outcome string) string {
	if outcome == domainreview.OutcomeApprove {
		return "approved"
	}
	return "rejected"
}

func ptrTime(value time.Time) *time.Time { return &value }

func insertReviewVideo(t *testing.T, db *gorm.DB, id int64, reviewVersion int) {
	t.Helper()
	video := infravideo.VideoModel{
		ID: id, AuthorID: 7, Title: "review", MediaURL: "media", CoverURL: "cover",
		MediaStatus: domainmedia.MediaStatusLegacyReady, Status: domainvideo.StatusPendingReview,
		Visibility: domainvideo.VisibilityPublic, ReviewVersion: reviewVersion,
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&infravideo.VideoStatModel{VideoID: id}).Error; err != nil {
		t.Fatal(err)
	}
}

func setReviewPolicy(t *testing.T, db *gorm.DB, config domainreview.PolicyConfiguration) {
	t.Helper()
	policy, err := domainreview.NewPolicy(1, true, config, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	value, err := json.Marshal(policy.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&PolicyModel{}).Where("version = ?", 1).Update("config_json", string(value)).Error; err != nil {
		t.Fatal(err)
	}
}

func newPostgresMachineResult(t *testing.T, reviewCase *domainreview.ReviewCase, resultID string, signals []domainreview.MachineSignal) *domainreview.MachineResult {
	t.Helper()
	result, err := domainreview.NewMachineResult(domainreview.MachineResultInput{
		CaseID: reviewCase.ID, VideoID: reviewCase.VideoID, ReviewVersion: reviewCase.ReviewVersion,
		ResultID: resultID, Provider: "test-provider", ModelVersion: "model-v1",
		SourceKind: domainreview.MachineSourceProductionProvider, GeneratedAt: time.Now(),
		RolloutMode:   domainreview.ModerationModeEnforce,
		PolicyVersion: reviewCase.PolicyVersion, Signals: signals, ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertReviewCounts(t *testing.T, db *gorm.DB, results, signals, decisions int64) {
	t.Helper()
	for _, item := range []struct {
		model any
		want  int64
	}{
		{&ResultModel{}, results}, {&SignalModel{}, signals}, {&DecisionModel{}, decisions},
	} {
		var got int64
		if err := db.Model(item.model).Count(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got != item.want {
			t.Fatalf("count for %T = %d, want %d", item.model, got, item.want)
		}
	}
}

func assertReviewNotification(
	t *testing.T,
	db *gorm.DB,
	eventID string,
	stage string,
	result string,
) {
	t.Helper()
	var model NotificationOutboxModel
	if err := db.Where("event_id = ?", eventID).Take(&model).Error; err != nil {
		t.Fatalf("load review notification %q: %v", eventID, err)
	}
	if model.Stage != stage || model.Result != result {
		t.Fatalf("review notification = %#v", model)
	}
}

func postgresDSNWithSchema(dsn, schema string) string {
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
