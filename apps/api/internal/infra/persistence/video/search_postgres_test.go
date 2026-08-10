package infravideo_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infraaccount "github.com/shiyudesu/frux/internal/infra/persistence/account"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type failingAdminAuditWriter struct{}

func (failingAdminAuditWriter) AppendInTransaction(
	context.Context,
	*gorm.DB,
	*domainadminaudit.Fact,
) error {
	return errors.New("audit unavailable")
}

func (failingAdminAuditWriter) RecordCommittedWrite(*domainadminaudit.Fact) {}

type successfulAdminAuditWriter struct{}

func (successfulAdminAuditWriter) AppendInTransaction(
	context.Context,
	*gorm.DB,
	*domainadminaudit.Fact,
) error {
	return nil
}

func (successfulAdminAuditWriter) RecordCommittedWrite(*domainadminaudit.Fact) {}

func TestPostgresPublicSearchRankingVisibilityAndLiteralWildcards(t *testing.T) {
	db := openSearchPostgres(t)
	now := time.Date(2026, 8, 4, 5, 0, 0, 0, time.UTC)
	users := []infraaccount.UserModel{
		{ID: 1, Account: "author", Password: "hash", Nickname: "Author", Status: 1, Role: "user", UpdatedAt: now},
		{ID: 2, Account: "alice", Password: "hash", Nickname: "Exact", Status: 1, Role: "user", UpdatedAt: now},
		{ID: 3, Account: "alice-two", Password: "hash", Nickname: "Account prefix", Status: 1, Role: "user", UpdatedAt: now.Add(-time.Minute)},
		{ID: 4, Account: "other-one", Password: "hash", Nickname: "Alice Nick", Status: 1, Role: "user", UpdatedAt: now.Add(-2 * time.Minute)},
		{ID: 5, Account: "xxalicexx", Password: "hash", Nickname: "Account contains", Status: 1, Role: "user", UpdatedAt: now.Add(-3 * time.Minute)},
		{ID: 6, Account: "other-two", Password: "hash", Nickname: "The Alice Person", Status: 1, Role: "user", UpdatedAt: now.Add(-4 * time.Minute)},
		{ID: 7, Account: "literal%_\\user", Password: "hash", Nickname: "Literal", Status: 1, Role: "user", UpdatedAt: now},
		{ID: 8, Account: "literalxxzuser", Password: "hash", Nickname: "Wildcard decoy", Status: 1, Role: "user", UpdatedAt: now},
		{ID: 9, Account: "alice-frozen", Password: "hash", Nickname: "Alice Frozen", Status: 2, Role: "user", UpdatedAt: now.Add(time.Hour)},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed accounts: %v", err)
	}

	published := now
	videos := []infravideo.VideoModel{
		searchVideoModel(101, "cat", "exact", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, published),
		searchVideoModel(102, "Cat videos", "prefix", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, published.Add(-time.Minute)),
		searchVideoModel(103, "My cAt video", "contains", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusLegacyReady, published.Add(-2*time.Minute)),
		searchVideoModel(104, "Other", "A CAT appears here", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, published.Add(-3*time.Minute)),
		searchVideoModel(105, "cat private", "hidden", domainvideo.StatusPublished, domainvideo.VisibilityPrivate, domainmedia.MediaStatusReady, published.Add(time.Hour)),
		searchVideoModel(106, "cat offline", "hidden", domainvideo.StatusOffline, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, published.Add(time.Hour)),
		searchVideoModel(107, "cat processing", "hidden", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusProcessing, published.Add(time.Hour)),
		searchVideoModel(108, `100%_off\sale`, "literal", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, published),
		searchVideoModel(109, "100XXoffZsale", "wildcard decoy", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, published),
	}
	if err := db.Create(&videos).Error; err != nil {
		t.Fatalf("seed videos: %v", err)
	}

	videoRepo := infravideo.New(db)
	videoItems, err := videoRepo.SearchVideos(context.Background(), "CAT", nil, 20)
	if err != nil {
		t.Fatalf("search videos: %v", err)
	}
	if got := videoSearchIDs(videoItems); fmt.Sprint(got) != "[101 102 103 104]" {
		t.Fatalf("video relevance/visibility order = %v, want [101 102 103 104]", got)
	}
	for index, want := range []int{1, 2, 3, 4} {
		if videoItems[index].Relevance != want {
			t.Fatalf("video %d relevance = %d, want %d", videoItems[index].ID, videoItems[index].Relevance, want)
		}
	}
	literalVideos, err := videoRepo.SearchVideos(context.Background(), `100%_off\sale`, nil, 20)
	if err != nil {
		t.Fatalf("search literal video wildcards: %v", err)
	}
	if got := videoSearchIDs(literalVideos); fmt.Sprint(got) != "[108]" {
		t.Fatalf("literal video wildcard search = %v, want [108]", got)
	}

	userRepo := infraaccount.New(db)
	userItems, err := userRepo.SearchUsers(context.Background(), "ALICE", nil, 20)
	if err != nil {
		t.Fatalf("search users: %v", err)
	}
	if got := userSearchIDs(userItems); fmt.Sprint(got) != "[2 3 4 5 6]" {
		t.Fatalf("user relevance/status order = %v, want [2 3 4 5 6]", got)
	}
	for index, want := range []int{1, 2, 3, 4, 5} {
		if userItems[index].Relevance != want {
			t.Fatalf("user %d relevance = %d, want %d", userItems[index].ID, userItems[index].Relevance, want)
		}
	}
	literalUsers, err := userRepo.SearchUsers(context.Background(), `literal%_\user`, nil, 20)
	if err != nil {
		t.Fatalf("search literal user wildcards: %v", err)
	}
	if got := userSearchIDs(literalUsers); fmt.Sprint(got) != "[7]" {
		t.Fatalf("literal user wildcard search = %v, want [7]", got)
	}
}

func TestPostgresAdminVideoSearchFiltersAndStableOrder(t *testing.T) {
	db := openSearchPostgres(t)
	now := time.Date(2026, 8, 6, 5, 0, 0, 0, time.UTC)
	if err := db.Create(&infraaccount.UserModel{
		ID: 21, Account: "operator-target", Password: "hash", Nickname: "Target",
		Status: 1, Role: "user", UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	videos := []infravideo.VideoModel{
		searchVideoModel(201, "Policy match newest", "review", domainvideo.StatusRejected, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, now),
		searchVideoModel(202, "Policy match older", "review", domainvideo.StatusRejected, domainvideo.VisibilityPrivate, domainmedia.MediaStatusReady, now.Add(-time.Minute)),
		searchVideoModel(203, "Policy published", "review", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, now.Add(-2*time.Minute)),
		searchVideoModel(204, "Deleted policy", "review", domainvideo.StatusDeleted, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, now.Add(time.Minute)),
	}
	for index := range videos {
		videos[index].AuthorID = 21
		videos[index].Version = index + 1
	}
	if err := db.Create(&videos).Error; err != nil {
		t.Fatalf("seed admin videos: %v", err)
	}
	repository := infravideo.New(db)
	from, to := now.Add(-time.Hour), now.Add(time.Hour)
	items, err := repository.ListAdminVideos(context.Background(), domainvideo.AdminVideoQuery{
		Status: domainvideo.StatusRejected, AuthorID: 21, Keyword: "policy",
		CreatedFrom: &from, CreatedTo: &to, Limit: 10,
	})
	if err != nil {
		t.Fatalf("admin search: %v", err)
	}
	if len(items) != 2 || items[0].ID != 201 || items[1].ID != 202 {
		t.Fatalf("admin search order = %#v", items)
	}
	if items[0].Version != 1 || items[1].Version != 2 {
		t.Fatalf("admin versions = %d,%d", items[0].Version, items[1].Version)
	}
	cursorItems, err := repository.ListAdminVideos(context.Background(), domainvideo.AdminVideoQuery{
		AuthorID: 21, CreatedFrom: &from, CreatedTo: &to, Limit: 10,
		Cursor: &domainvideo.AdminVideoCursor{CreatedAt: items[0].CreatedAt, VideoID: items[0].ID},
	})
	if err != nil {
		t.Fatalf("admin cursor search: %v", err)
	}
	for _, item := range cursorItems {
		if item.ID == 201 || item.ID == 204 {
			t.Fatalf("cursor/deleted item leaked: %d", item.ID)
		}
	}
}

func TestPostgresAdminTransitionRollsBackWhenAuditFails(t *testing.T) {
	db := openSearchPostgres(t)
	if err := db.AutoMigrate(
		&infravideo.EnforcementActionModel{},
		&infravideo.AdminTransitionIntentModel{},
		&infravideo.NotificationOutboxModel{},
	); err != nil {
		t.Fatalf("migrate admin transition tables: %v", err)
	}
	now := time.Date(2026, 8, 6, 6, 0, 0, 0, time.UTC)
	video := searchVideoModel(
		301, "Published", "", domainvideo.StatusPublished,
		domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, now,
	)
	video.Version = 4
	if err := db.Create(&video).Error; err != nil {
		t.Fatalf("seed transition video: %v", err)
	}
	fact, err := domainadminaudit.NewFact(domainadminaudit.FactInput{
		ActorID: 9, Permission: domainaccount.PermissionContentEnforce,
		Action:     domainadminaudit.ActionContentEnforce,
		TargetType: domainadminaudit.TargetVideo, TargetID: "301",
		Outcome:   domainadminaudit.OutcomeSuccess,
		RequestID: "audit-0123456789abcdef0123456789abcdef",
		Detail: map[string]string{
			"http_method": "POST", "previous_status": "published",
			"new_status": "offline", "reason_code": "policy_violation",
			"route": "/api/admin/videos/:videoId/enforcement",
		},
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("build audit fact: %v", err)
	}
	repository := infravideo.New(db, infravideo.WithAdminAuditWriter(failingAdminAuditWriter{}))
	_, err = repository.CommitAdminTransition(context.Background(), domainvideo.AdminTransitionCommand{
		VideoID: 301, ActorID: 9, ExpectedVersion: 4,
		Transition: domainvideo.LifecycleTakeOffline,
		ReasonCode: domainvideo.EnforcementReasonPolicy, OccurredAt: now,
	}, fact)
	if err == nil {
		t.Fatal("expected audit failure")
	}
	var current infravideo.VideoModel
	if err := db.Where("id = ?", 301).Take(&current).Error; err != nil {
		t.Fatalf("reload video: %v", err)
	}
	if current.Status != domainvideo.StatusPublished || current.Version != 4 {
		t.Fatalf("video changed despite rollback: status=%d version=%d", current.Status, current.Version)
	}
	var actionCount, outboxCount, notificationCount int64
	if err := db.Model(&infravideo.EnforcementActionModel{}).Count(&actionCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&infravideo.AdminTransitionIntentModel{}).Count(&outboxCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&infravideo.NotificationOutboxModel{}).Count(&notificationCount).Error; err != nil {
		t.Fatal(err)
	}
	if actionCount != 0 || outboxCount != 0 || notificationCount != 0 {
		t.Fatalf(
			"rollback counts action=%d outbox=%d notification=%d",
			actionCount, outboxCount, notificationCount,
		)
	}
}

func TestPostgresVideoLifecycleNotificationFactsAreAtomicAndIdempotent(t *testing.T) {
	db := openSearchPostgres(t)
	if err := db.AutoMigrate(
		&infravideo.EnforcementActionModel{},
		&infravideo.AdminTransitionIntentModel{},
		&infravideo.NotificationOutboxModel{},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 7, 1, 0, 0, 0, time.UTC)
	repository := infravideo.New(db, infravideo.WithAdminAuditWriter(successfulAdminAuditWriter{}))
	video := &domainvideo.Video{
		AuthorID: 1, Title: "Lifecycle", Description: "",
		MediaURL: "", CoverURL: "", MediaAssetID: 41, CoverAssetID: 42,
		MediaStatus:   domainmedia.MediaStatusProcessing,
		ReviewVersion: 1, Version: 1, Status: domainvideo.StatusPendingReview,
		Visibility: domainvideo.VisibilityPublic,
	}
	if err := repository.Save(context.Background(), video); err != nil {
		t.Fatalf("save video: %v", err)
	}
	assertVideoNotification(t, db, domainmessage.SubmissionEventID(video.ID, 1),
		domainmessage.LifecycleStageSubmitted, domainmessage.LifecycleResultPending)

	video.Status = domainvideo.StatusPublished
	video.MediaStatus = domainmedia.MediaStatusReady
	video.MediaURL = "https://example.com/ready.mp4"
	video.CoverURL = "https://example.com/cover.jpg"
	if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", video.ID).
		Updates(map[string]any{"status": domainvideo.StatusPublished, "published_at": now}).
		Error; err != nil {
		t.Fatal(err)
	}
	if eligible, err := repository.UpdateMediaProjection(context.Background(), video); err != nil || !eligible {
		t.Fatalf("ready projection eligible=%v err=%v", eligible, err)
	}
	if eligible, err := repository.UpdateMediaProjection(context.Background(), video); err != nil || !eligible {
		t.Fatalf("replayed projection eligible=%v err=%v", eligible, err)
	}
	eventID := domainmessage.PublicationEventID(video.ID, 1)
	assertVideoNotification(
		t, db, eventID,
		domainmessage.LifecycleStagePublished, domainmessage.LifecycleResultPublic,
	)
	var publicationCount int64
	if err := db.Model(&infravideo.NotificationOutboxModel{}).
		Where("event_id = ?", eventID).Count(&publicationCount).Error; err != nil ||
		publicationCount != 1 {
		t.Fatalf("publication count=%d err=%v", publicationCount, err)
	}
	if ready, err := repository.LifecyclePublicationReady(
		context.Background(), eventID,
	); err != nil || !ready {
		t.Fatalf("production projection ready=%v err=%v", ready, err)
	}
	var publicationEvent infravideo.PublicationEventOutboxModel
	if err := db.Where("event_id = ?", eventID).Take(&publicationEvent).Error; err != nil ||
		!publicationEvent.DeliveryReady {
		t.Fatalf("publication event=%#v err=%v", publicationEvent, err)
	}
	legacyCandidate := searchVideoModel(
		98, "Legacy candidate", "", domainvideo.StatusPublished,
		domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, now,
	)
	legacyCandidate.AuthorID = 1
	legacyCandidate.ReviewVersion = 1
	if err := db.Create(&legacyCandidate).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return infravideo.AppendLifecycleNotification(tx, domainmessage.LifecycleNotification{
			EventID:     domainmessage.SubmissionEventID(legacyCandidate.ID, 1),
			RecipientID: 1, VideoID: legacyCandidate.ID, ReviewVersion: 1,
			Stage:  domainmessage.LifecycleStageSubmitted,
			Result: domainmessage.LifecycleResultPending, OccurredAt: now,
		})
	}); err != nil {
		t.Fatal(err)
	}
	if created, err := repository.ReconcileLifecyclePublicationNotifications(
		context.Background(), 10,
	); err != nil || created != 1 {
		t.Fatalf("legacy reconciliation created=%d err=%v", created, err)
	}
	assertVideoNotification(
		t, db, domainmessage.PublicationEventID(legacyCandidate.ID, 1),
		domainmessage.LifecycleStagePublished, domainmessage.LifecycleResultPublic,
	)
	historical := searchVideoModel(
		99, "Historical", "", domainvideo.StatusPublished,
		domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, now,
	)
	historical.AuthorID = 1
	historical.ReviewVersion = 1
	if err := db.Create(&historical).Error; err != nil {
		t.Fatal(err)
	}
	if created, err := repository.ReconcileLifecyclePublicationNotifications(
		context.Background(), 10,
	); err != nil || created != 0 {
		t.Fatalf("historical reconciliation created=%d err=%v", created, err)
	}
	var historicalNotifications int64
	if err := db.Model(&infravideo.NotificationOutboxModel{}).
		Where("video_id = ?", historical.ID).Count(&historicalNotifications).Error; err != nil ||
		historicalNotifications != 0 {
		t.Fatalf("historical notifications=%d err=%v", historicalNotifications, err)
	}
	if err := repository.MarkLifecyclePublicationReady(
		context.Background(),
		domainmessage.PublicationEventID(historical.ID, historical.ReviewVersion),
		now,
	); err != nil {
		t.Fatalf("untracked historical readiness: %v", err)
	}
	rejected := searchVideoModel(
		100, "Rejected", "", domainvideo.StatusRejected,
		domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, now,
	)
	rejected.AuthorID = 1
	rejected.ReviewVersion = 1
	rejected.MediaAssetID = int64Pointer(100)
	if err := db.Create(&rejected).Error; err != nil {
		t.Fatal(err)
	}
	rejectedDomain := &domainvideo.Video{
		ID: rejected.ID, AuthorID: rejected.AuthorID,
		Status: rejected.Status, Visibility: rejected.Visibility,
		ReviewVersion: rejected.ReviewVersion, MediaAssetID: 100,
		MediaStatus:         domainmedia.MediaStatusFailed,
		MediaProfileVersion: "v1",
	}
	if _, err := repository.UpdateMediaProjection(context.Background(), rejectedDomain); err != nil {
		t.Fatal(err)
	}
	var rejectedFailureNotifications int64
	if err := db.Model(&infravideo.NotificationOutboxModel{}).
		Where("video_id = ? AND stage = ?", rejected.ID, domainmessage.LifecycleStageMediaProcessing).
		Count(&rejectedFailureNotifications).Error; err != nil ||
		rejectedFailureNotifications != 0 {
		t.Fatalf(
			"rejected failure notifications=%d err=%v",
			rejectedFailureNotifications, err,
		)
	}

	video.MediaStatus = domainmedia.MediaStatusFailed
	video.MediaErrorCode = "probe_invalid"
	video.MediaProfileVersion = "v1"
	video.MediaURL = ""
	video.CoverURL = ""
	if _, err := repository.UpdateMediaProjection(context.Background(), video); err != nil {
		t.Fatalf("failed projection: %v", err)
	}
	assertVideoNotification(
		t, db, domainmessage.MediaFailureEventID(video.ID, video.MediaAssetID, "v1"),
		domainmessage.LifecycleStageMediaProcessing, domainmessage.LifecycleResultFailed,
	)

	video.MediaStatus = domainmedia.MediaStatusReady
	video.MediaErrorCode = ""
	video.Status = domainvideo.StatusPublished
	video.Version = 1
	video.PublishedAt = &now
	if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", video.ID).
		Updates(map[string]any{
			"status": domainvideo.StatusPublished, "version": 1,
			"media_status": domainmedia.MediaStatusReady, "published_at": now,
		}).Error; err != nil {
		t.Fatal(err)
	}
	fact, err := domainadminaudit.NewFact(domainadminaudit.FactInput{
		ActorID: 9, Permission: domainaccount.PermissionContentEnforce,
		Action:     domainadminaudit.ActionContentEnforce,
		TargetType: domainadminaudit.TargetVideo, TargetID: fmt.Sprint(video.ID),
		Outcome:   domainadminaudit.OutcomeSuccess,
		RequestID: domainadminaudit.NewRequestID(),
		Detail: map[string]string{
			"http_method": "POST", "previous_status": "published",
			"new_status": "offline", "reason_code": "policy_violation",
			"route": "/api/admin/videos/:videoId/enforcement",
		},
		CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := repository.CommitAdminTransition(
		context.Background(),
		domainvideo.AdminTransitionCommand{
			VideoID: video.ID, ActorID: 9, ExpectedVersion: 1,
			Transition: domainvideo.LifecycleTakeOffline,
			ReasonCode: domainvideo.EnforcementReasonPolicy, OccurredAt: now,
		},
		fact,
	)
	if err != nil {
		t.Fatalf("take down: %v", err)
	}
	var action infravideo.EnforcementActionModel
	if err := db.Where("video_id = ?", video.ID).Order("id DESC").Take(&action).Error; err != nil {
		t.Fatal(err)
	}
	assertVideoNotification(
		t, db, domainmessage.EnforcementEventID(video.ID, action.ID),
		domainmessage.LifecycleStageEnforcement, domainmessage.LifecycleResultTakenDown,
	)
	if result.Video.Status != domainvideo.StatusOffline {
		t.Fatalf("offline result=%#v", result.Video)
	}
	restoreAt := now.Add(time.Minute)
	restoreFact, err := domainadminaudit.NewFact(domainadminaudit.FactInput{
		ActorID: 9, Permission: domainaccount.PermissionContentEnforce,
		Action:     domainadminaudit.ActionContentRestore,
		TargetType: domainadminaudit.TargetVideo, TargetID: fmt.Sprint(video.ID),
		Outcome:   domainadminaudit.OutcomeSuccess,
		RequestID: domainadminaudit.NewRequestID(),
		Detail: map[string]string{
			"http_method": "POST", "previous_status": "offline",
			"new_status": "published", "reason_code": "compliance_restored",
			"route": "/api/admin/videos/:videoId/restoration",
		},
		CreatedAt: restoreAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := repository.CommitAdminTransition(
		context.Background(),
		domainvideo.AdminTransitionCommand{
			VideoID: video.ID, ActorID: 9, ExpectedVersion: result.Video.Version,
			Transition: domainvideo.LifecycleRestore,
			ReasonCode: domainvideo.RestorationReasonAllowed, OccurredAt: restoreAt,
		},
		restoreFact,
	)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	action = infravideo.EnforcementActionModel{}
	if err := db.Where("video_id = ?", video.ID).
		Order("id DESC").Take(&action).Error; err != nil {
		t.Fatal(err)
	}
	assertVideoNotification(
		t, db, domainmessage.RestorationEventID(video.ID, action.ID),
		domainmessage.LifecycleStageRestoration, domainmessage.LifecycleResultRestored,
	)
	if restored.Video.Status != domainvideo.StatusPublished {
		t.Fatalf("restored result=%#v", restored.Video)
	}

	privateVideo := searchVideoModel(
		101, "Private first publish", "", domainvideo.StatusPublished,
		domainvideo.VisibilityPrivate, domainmedia.MediaStatusReady, now,
	)
	privateVideo.AuthorID = 1
	privateVideo.ReviewVersion = 1
	privateVideo.MediaAssetID = int64Pointer(101)
	if err := db.Create(&privateVideo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return infravideo.AppendLifecycleNotification(tx, domainmessage.LifecycleNotification{
			EventID:     domainmessage.SubmissionEventID(privateVideo.ID, 1),
			RecipientID: 1, VideoID: privateVideo.ID, ReviewVersion: 1,
			Stage:  domainmessage.LifecycleStageSubmitted,
			Result: domainmessage.LifecycleResultPending, OccurredAt: now,
		})
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ApplyBatch(
		context.Background(), 1, domainvideo.BatchActionMakePublic,
		[]int64{privateVideo.ID}, "public-private-target", "fingerprint",
	); err != nil {
		t.Fatal(err)
	}
	privatePublicationID := domainmessage.PublicationEventID(privateVideo.ID, 1)
	var pendingPublication infravideo.NotificationOutboxModel
	if err := db.Where("event_id = ?", privatePublicationID).
		Take(&pendingPublication).Error; err != nil {
		t.Fatal(err)
	}
	if pendingPublication.DeliveryReady {
		t.Fatal("visibility transaction marked media-backed notification ready")
	}
	var blockedEvent infravideo.PublicationEventOutboxModel
	if err := db.Where("event_id = ?", privatePublicationID).
		Take(&blockedEvent).Error; err != nil {
		t.Fatal(err)
	}
	if blockedEvent.DeliveryReady {
		t.Fatal("visibility transaction made media-backed event dispatchable")
	}
	var immutableEvent applicationvideo.PublishedEvent
	if err := json.Unmarshal([]byte(blockedEvent.PayloadJSON), &immutableEvent); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&infravideo.VideoModel{}).
		Where("id = ?", privateVideo.ID).
		Updates(map[string]any{
			"title": "Current public title", "description": "Current public description",
		}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdateMediaProjection(context.Background(), &domainvideo.Video{
		ID: privateVideo.ID, AuthorID: 1, ReviewVersion: 1,
		Status: domainvideo.StatusPublished, Visibility: domainvideo.VisibilityPublic,
		MediaAssetID: 101, MediaStatus: domainmedia.MediaStatusReady,
		MediaURL: "https://example.com/private-promoted.mp4",
		CoverURL: "https://example.com/private-promoted.jpg",
	}); err != nil {
		t.Fatal(err)
	}
	if ready, err := repository.LifecyclePublicationReady(
		context.Background(), privatePublicationID,
	); err != nil || !ready {
		t.Fatalf("completed private publication ready=%v err=%v", ready, err)
	}
	if err := db.Where("event_id = ?", privatePublicationID).
		Take(&blockedEvent).Error; err != nil || !blockedEvent.DeliveryReady {
		t.Fatalf("completed publication event=%#v err=%v", blockedEvent, err)
	}
	var refreshedEvent applicationvideo.PublishedEvent
	if err := json.Unmarshal([]byte(blockedEvent.PayloadJSON), &refreshedEvent); err != nil {
		t.Fatal(err)
	}
	if refreshedEvent.Title != "Current public title" ||
		refreshedEvent.Description != "Current public description" ||
		refreshedEvent.MediaURL != "https://example.com/private-promoted.mp4" ||
		refreshedEvent.CoverURL != "https://example.com/private-promoted.jpg" {
		t.Fatalf("undispatched payload was not refreshed: %#v", refreshedEvent)
	}
	var publicationFact infravideo.PublicationEventFactModel
	if err := db.Where("event_id = ?", privatePublicationID).Take(&publicationFact).Error; err != nil {
		t.Fatal(err)
	}
	var retainedEvent applicationvideo.PublishedEvent
	if err := json.Unmarshal([]byte(publicationFact.PayloadJSON), &retainedEvent); err != nil {
		t.Fatal(err)
	}
	if retainedEvent != immutableEvent {
		t.Fatalf("immutable publication fact changed: before=%#v after=%#v", immutableEvent, retainedEvent)
	}
	historicalPrivate := searchVideoModel(
		102, "Historical private", "", domainvideo.StatusPublished,
		domainvideo.VisibilityPrivate, domainmedia.MediaStatusReady, now,
	)
	historicalPrivate.AuthorID = 1
	historicalPrivate.ReviewVersion = 1
	if err := db.Create(&historicalPrivate).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ApplyBatch(
		context.Background(), 1, domainvideo.BatchActionMakePublic,
		[]int64{historicalPrivate.ID}, "public-historical-private", "fingerprint-historical",
	); err != nil {
		t.Fatal(err)
	}
	var synthesized int64
	if err := db.Model(&infravideo.NotificationOutboxModel{}).
		Where("event_id = ?", domainmessage.PublicationEventID(historicalPrivate.ID, 1)).
		Count(&synthesized).Error; err != nil || synthesized != 1 {
		t.Fatalf("historical make-public notifications=%d err=%v", synthesized, err)
	}
}

func TestPostgresVideoCreationRollsBackWhenNotificationFails(t *testing.T) {
	db := openSearchPostgres(t)
	if err := db.AutoMigrate(&infravideo.NotificationOutboxModel{}); err != nil {
		t.Fatal(err)
	}

	t.Run("outbox failure rolls back public edge", func(t *testing.T) {
		db := openSearchPostgres(t)
		now := time.Now().UTC()
		video := searchVideoModel(
			701, "atomic", "", domainvideo.StatusPublished,
			domainvideo.VisibilityPrivate, domainmedia.MediaStatusLegacyReady, now,
		)
		video.AuthorID = 1
		video.ReviewVersion = 1
		if err := db.Create(&video).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			return infravideo.AppendLifecycleNotification(tx, domainmessage.LifecycleNotification{
				EventID:     domainmessage.SubmissionEventID(video.ID, 1),
				RecipientID: 1, VideoID: video.ID, ReviewVersion: 1,
				Stage:  domainmessage.LifecycleStageSubmitted,
				Result: domainmessage.LifecycleResultPending, OccurredAt: now,
			})
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`
				ALTER TABLE video_publication_event_outbox
				ADD CONSTRAINT reject_publication_event CHECK (event_type <> 'video_published.v1')
			`).Error; err != nil {
			t.Fatal(err)
		}
		repository := infravideo.New(db)
		if _, _, err := repository.ApplyBatch(
			context.Background(), 1, domainvideo.BatchActionMakePublic,
			[]int64{video.ID}, "atomic-edge", "atomic-edge",
		); err == nil {
			t.Fatal("expected publication outbox failure")
		}
		var current infravideo.VideoModel
		if err := db.Where("id = ?", video.ID).Take(&current).Error; err != nil {
			t.Fatal(err)
		}
		if current.Visibility != domainvideo.VisibilityPrivate {
			t.Fatalf("visibility committed without outbox: %s", current.Visibility)
		}
	})

	t.Run("concurrent public edge creates one stable event", func(t *testing.T) {
		db := openSearchPostgres(t)
		now := time.Now().UTC()
		video := searchVideoModel(
			702, "race", "", domainvideo.StatusPublished,
			domainvideo.VisibilityPrivate, domainmedia.MediaStatusLegacyReady, now,
		)
		video.AuthorID = 1
		video.ReviewVersion = 1
		if err := db.Create(&video).Error; err != nil {
			t.Fatal(err)
		}
		repository := infravideo.New(db)
		var wait sync.WaitGroup
		errs := make(chan error, 8)
		for index := range 8 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				key := fmt.Sprintf("race-%d", index)
				_, _, err := repository.ApplyBatch(
					context.Background(), 1, domainvideo.BatchActionMakePublic,
					[]int64{video.ID}, key, key,
				)
				errs <- err
			}()
		}
		wait.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		eventID := domainmessage.PublicationEventID(video.ID, 1)
		var notifications, events int64
		if err := db.Model(&infravideo.NotificationOutboxModel{}).
			Where("event_id = ?", eventID).Count(&notifications).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&infravideo.PublicationEventOutboxModel{}).
			Where("event_id = ?", eventID).Count(&events).Error; err != nil {
			t.Fatal(err)
		}
		if notifications != 1 || events != 1 {
			t.Fatalf("handoff rows notification=%d event=%d", notifications, events)
		}
	})

	t.Run("delivered notification repairs missing event without historical scan", func(t *testing.T) {
		db := openSearchPostgres(t)
		now := time.Now().UTC()
		tracked := searchVideoModel(
			703, "repair", "", domainvideo.StatusPublished,
			domainvideo.VisibilityPublic, domainmedia.MediaStatusLegacyReady, now,
		)
		tracked.AuthorID = 1
		tracked.ReviewVersion = 1
		historical := searchVideoModel(
			704, "historical", "", domainvideo.StatusPublished,
			domainvideo.VisibilityPublic, domainmedia.MediaStatusLegacyReady, now,
		)
		historical.AuthorID = 1
		historical.ReviewVersion = 1
		if err := db.Create(&[]infravideo.VideoModel{tracked, historical}).Error; err != nil {
			t.Fatal(err)
		}
		eventID := domainmessage.PublicationEventID(tracked.ID, 1)
		deliveredAt := now
		if err := db.Create(&infravideo.NotificationOutboxModel{
			EventID: eventID, RecipientID: 1, VideoID: tracked.ID, ReviewVersion: 1,
			Stage:  domainmessage.LifecycleStagePublished,
			Result: domainmessage.LifecycleResultPublic, OccurredAt: now,
			DeliveryReady: true, State: domainmessage.LifecycleOutboxDelivered,
			AvailableAt: now, DeliveredAt: &deliveredAt, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		repository := infravideo.New(db)
		created, err := repository.ReconcilePublicationEvents(context.Background(), 100, now)
		if err != nil || created != 1 {
			t.Fatalf("reconcile created=%d err=%v", created, err)
		}
		var repaired, historicalEvents, facts int64
		_ = db.Model(&infravideo.PublicationEventOutboxModel{}).
			Where("event_id = ?", eventID).Count(&repaired).Error
		_ = db.Model(&infravideo.PublicationEventFactModel{}).
			Where("event_id = ?", eventID).Count(&facts).Error
		_ = db.Model(&infravideo.PublicationEventOutboxModel{}).
			Where("video_id = ?", historical.ID).Count(&historicalEvents).Error
		if repaired != 1 || facts != 1 || historicalEvents != 0 {
			t.Fatalf("repaired=%d facts=%d historical=%d", repaired, facts, historicalEvents)
		}
		var notification infravideo.NotificationOutboxModel
		if err := db.Where("event_id = ?", eventID).Take(&notification).Error; err != nil {
			t.Fatal(err)
		}
		if notification.State != domainmessage.LifecycleOutboxDelivered {
			t.Fatalf("notification state changed: %s", notification.State)
		}
		dispatchedAt := now.Add(time.Minute)
		if err := db.Model(&infravideo.PublicationEventOutboxModel{}).
			Where("event_id = ?", eventID).
			Update("dispatched_at", dispatchedAt).Error; err != nil {
			t.Fatal(err)
		}
		deleted, err := repository.CleanupPublicationEvents(
			context.Background(), dispatchedAt.Add(time.Second), 100,
		)
		if err != nil || deleted != 1 {
			t.Fatalf("cleanup deleted=%d err=%v", deleted, err)
		}
		created, err = repository.ReconcilePublicationEvents(
			context.Background(), 100, dispatchedAt.Add(2*time.Second),
		)
		if err != nil || created != 0 {
			t.Fatalf("post-cleanup reconcile created=%d err=%v", created, err)
		}
		_ = db.Model(&infravideo.PublicationEventOutboxModel{}).
			Where("event_id = ?", eventID).Count(&repaired).Error
		_ = db.Model(&infravideo.PublicationEventFactModel{}).
			Where("event_id = ?", eventID).Count(&facts).Error
		if repaired != 0 || facts != 1 {
			t.Fatalf("post-cleanup outbox=%d facts=%d", repaired, facts)
		}
	})
	if err := db.Exec(`
		ALTER TABLE video_notification_outbox
		ADD CONSTRAINT reject_submission_notification CHECK (stage <> 'submitted')
	`).Error; err != nil {
		t.Fatal(err)
	}
	repository := infravideo.New(db)
	video := &domainvideo.Video{
		AuthorID: 1, Title: "Rollback", MediaAssetID: 51, CoverAssetID: 52,
		MediaStatus: domainmedia.MediaStatusProcessing, ReviewVersion: 1, Version: 1,
		Status: domainvideo.StatusPendingReview, Visibility: domainvideo.VisibilityPublic,
	}
	if err := repository.Save(context.Background(), video); err == nil {
		t.Fatal("expected notification insertion failure")
	}
	var videos, stats int64
	if err := db.Model(&infravideo.VideoModel{}).Count(&videos).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&infravideo.VideoStatModel{}).Count(&stats).Error; err != nil {
		t.Fatal(err)
	}
	if videos != 0 || stats != 0 {
		t.Fatalf("creation rollback videos=%d stats=%d", videos, stats)
	}
}

func assertVideoNotification(
	t *testing.T,
	db *gorm.DB,
	eventID string,
	stage string,
	result string,
) {
	t.Helper()
	var model infravideo.NotificationOutboxModel
	if err := db.Where("event_id = ?", eventID).Take(&model).Error; err != nil {
		var all []infravideo.NotificationOutboxModel
		_ = db.Order("event_id ASC").Find(&all).Error
		t.Fatalf("load notification %q: %v all=%#v", eventID, err, all)
	}

	if model.Stage != stage || model.Result != result {
		t.Fatalf("notification=%#v", model)
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}

func openSearchPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL search integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("frux_search_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})
	sqlDB, err := sql.Open("pgx", searchPostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	if err := db.AutoMigrate(
		&infraaccount.UserModel{}, &infravideo.VideoModel{},
		&infravideo.VideoStatModel{}, &infravideo.UserContentStatModel{},
		&infravideo.BatchOperationModel{},
		&infravideo.NotificationOutboxModel{},
		&infravideo.PublicationEventFactModel{},
		&infravideo.PublicationEventOutboxModel{},
	); err != nil {
		t.Fatalf("migrate search tables: %v", err)
	}
	return db
}

func searchVideoModel(id int64, title, description string, status int, visibility, mediaStatus string, publishedAt time.Time) infravideo.VideoModel {
	return infravideo.VideoModel{
		ID: id, AuthorID: 1, Title: title, Description: description,
		MediaURL: fmt.Sprintf("https://example.com/%d.mp4", id),
		CoverURL: fmt.Sprintf("https://example.com/%d.jpg", id),
		Status:   status, Visibility: visibility, MediaStatus: mediaStatus, Version: 1,
		PublishedAt: &publishedAt, CreatedAt: publishedAt, UpdatedAt: publishedAt,
	}
}

func videoSearchIDs(items []*domainsearch.VideoIndexItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func userSearchIDs(items []*domainsearch.UserIndexItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func searchPostgresDSNWithSchema(dsn, schema string) string {
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
