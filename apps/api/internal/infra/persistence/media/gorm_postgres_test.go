package inframedia

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCleanupTaskPostgreSQLFencingAndDeadline(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set")
	}

	db := openMediaPostgres(t, dsn)
	repository := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	task, err := domainmedia.NewCleanupTask(
		0, domainmedia.StorageBackendS3, "moderation/1/frame.jpg",
		now.Add(time.Hour), 3,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateCleanupTasks(context.Background(), []*domainmedia.CleanupTask{task}); err != nil {
		t.Fatal(err)
	}
	earlier, err := domainmedia.NewCleanupTask(
		0, domainmedia.StorageBackendS3, task.ObjectKey, now.Add(time.Minute), 3,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateCleanupTasks(context.Background(), []*domainmedia.CleanupTask{earlier}); err != nil {
		t.Fatal(err)
	}
	var cleanupCount int64
	if err := db.Model(&CleanupTaskModel{}).Count(&cleanupCount).Error; err != nil || cleanupCount != 1 {
		t.Fatalf("cleanup task count=%d err=%v", cleanupCount, err)
	}
	var stored CleanupTaskModel
	if err := db.Where("object_key = ?", task.ObjectKey).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.NotBefore.Equal(earlier.NotBefore) {
		t.Fatalf("not_before = %v, want %v", stored.NotBefore, earlier.NotBefore)
	}
	leased, err := repository.LeaseCleanupTasks(
		context.Background(), "cleanup-owner", now.Add(2*time.Minute),
		now.Add(7*time.Minute), 1,
	)
	if err != nil || len(leased) != 1 {
		t.Fatalf("leased = %#v err=%v", leased, err)
	}
	if err := repository.RenewCleanupTaskLease(
		context.Background(), leased[0].ID, "cleanup-owner", 5*time.Minute,
	); err != nil {
		t.Fatal(err)
	}
	leased[0].State = domainmedia.CleanupStateCompleted
	finishedAt := now.Add(3 * time.Minute)
	leased[0].CompletedAt = &finishedAt
	if err := repository.UpdateCleanupTaskOwned(
		context.Background(), leased[0], "stale-owner",
	); err == nil {
		t.Fatal("stale cleanup owner updated task")
	}
	if err := repository.UpdateCleanupTaskOwned(
		context.Background(), leased[0], "cleanup-owner",
	); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredProcessingLeaseRecordsReasonAndCanBeReclaimed(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set")
	}
	db := openMediaPostgres(t, dsn)
	repository := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	expiredAt := now.Add(-time.Minute)
	job := ProcessingJobModel{
		AssetID: 41, ProfileVersion: "v1",
		State: domainmedia.JobStateProcessing, Attempts: 1, MaxAttempts: 5,
		LeaseOwner: "stopped-worker", LeaseUntil: &expiredAt,
		NextAttemptAt: now.Add(-time.Hour),
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	released, err := repository.ReleaseExpiredProcessingLeases(context.Background(), now)
	if err != nil || released != 1 {
		t.Fatalf("released=%d err=%v", released, err)
	}
	var recovered ProcessingJobModel
	if err := db.First(&recovered, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if recovered.State != domainmedia.JobStateRetryable ||
		recovered.ErrorCode != "lease_expired" ||
		recovered.ErrorMessage != "processing lease expired before finalization" ||
		recovered.LeaseOwner != "" || recovered.LeaseUntil != nil {
		t.Fatalf("recovered job = %+v", recovered)
	}
	claimed, err := repository.LeaseProcessingJobs(
		context.Background(), "new-worker", now, now.Add(10*time.Minute), 1,
	)
	if err != nil || len(claimed) != 1 ||
		claimed[0].State != domainmedia.JobStateProcessing ||
		claimed[0].Attempts != 2 {
		t.Fatalf("claimed=%+v err=%v", claimed, err)
	}
}

func TestAdminProcessingOverviewRetryAndReplay(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set")
	}
	db := openMediaPostgres(t, dsn)
	repository := New(db, WithAdminAuditWriter(mediaAuditWriterStub{}))
	now := time.Now().UTC().Truncate(time.Microsecond)
	asset := AssetModel{
		OwnerID: 4, Kind: domainmedia.AssetKindVideo,
		StorageBackend: domainmedia.StorageBackendS3,
		ObjectKey:      "uploads/4/source.mp4", ContentType: "video/mp4",
		SizeBytes: 10, ChecksumSHA256: strings.Repeat("a", 64),
		State: domainmedia.AssetStateFailed, ErrorCode: "transcode_failed",
	}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	completedAt := now.Add(-time.Minute)
	job := ProcessingJobModel{
		AssetID: asset.ID, ProfileVersion: "v2",
		State: domainmedia.JobStateFailed, Attempts: 5, MaxAttempts: 5,
		ErrorCode: "transcode_failed", ErrorMessage: "failed",
		ProcessingStep: domainmedia.ProcessingStepFailed,
		NextAttemptAt:  now.Add(-time.Hour), CompletedAt: &completedAt,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	video := mediaAdminVideoTestModel{
		AuthorID: 4, Title: "Failed video",
		MediaAssetID: int64Pointer(asset.ID),
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatal(err)
	}
	summary, err := repository.SummarizeAdminProcessing(context.Background())
	if err != nil || summary.Failed != 1 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	history, err := repository.ListAdminProcessingHistory(
		context.Background(),
		domainmedia.AdminProcessingHistoryQuery{State: domainmedia.JobStateFailed, Limit: 10},
	)
	if err != nil || len(history) != 1 || history[0].ID != job.ID {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	command := domainmedia.AdminProcessingRetryCommand{
		ActorID: 7, JobID: job.ID, VideoID: video.ID,
		ReasonCode:     domainmedia.ProcessingRetryReasonTemporaryFailure,
		Route:          "/api/admin/media-processing/jobs/:jobId/retry",
		IdempotencyKey: "retry-key", OccurredAt: now,
	}
	buildAudit := func(
		input domainmedia.ProcessingRetryAuditInput,
	) (*domainadminaudit.Fact, error) {
		digest, err := domainadminaudit.DigestIdempotencyKey(command.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		return domainadminaudit.NewFact(domainadminaudit.FactInput{
			ActorID: command.ActorID, Permission: domainaccount.PermissionContentEnforce,
			Action:     domainadminaudit.ActionMediaProcessingRetry,
			TargetType: domainadminaudit.TargetMediaProcessingJob,
			TargetID:   fmt.Sprint(command.JobID), Outcome: domainadminaudit.OutcomeSuccess,
			RequestID: domainadminaudit.NewRequestID(), IdempotencyKeyHash: digest,
			CreatedAt: command.OccurredAt,
			Detail: map[string]string{
				"http_method": "POST", "route": command.Route,
				"reason_code": command.ReasonCode, "video_id": fmt.Sprint(input.VideoID),
				"previous_state": input.PreviousState, "new_state": input.NewState,
				"previous_attempts": fmt.Sprint(input.PreviousAttempts),
			},
		})
	}
	result, err := repository.CommitAdminProcessingRetry(
		context.Background(), command, buildAudit,
	)
	if err != nil || result.Job.State != domainmedia.JobStateRetryable ||
		result.Job.Attempts != 0 {
		t.Fatalf("retry result=%+v err=%v", result, err)
	}
	replayed, err := repository.CommitAdminProcessingRetry(
		context.Background(), command, buildAudit,
	)
	if err != nil || !replayed.Replayed {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	backlog, err := repository.CountPendingProcessingRetryNotifications(context.Background())
	if err != nil || backlog != 1 {
		t.Fatalf("outbox backlog=%d err=%v", backlog, err)
	}
}

func TestAdminProcessingRetryRollsBackWhenAuditFails(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set")
	}
	db := openMediaPostgres(t, dsn)
	repository := New(db, WithAdminAuditWriter(failingMediaAuditWriterStub{}))
	now := time.Now().UTC().Truncate(time.Microsecond)
	asset := AssetModel{
		OwnerID: 4, Kind: domainmedia.AssetKindVideo,
		StorageBackend: domainmedia.StorageBackendS3,
		ObjectKey:      "uploads/4/rollback.mp4", ContentType: "video/mp4",
		SizeBytes: 10, ChecksumSHA256: strings.Repeat("b", 64),
		State: domainmedia.AssetStateFailed, ErrorCode: "transcode_failed",
	}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	completedAt := now.Add(-time.Minute)
	job := ProcessingJobModel{
		AssetID: asset.ID, ProfileVersion: "v2",
		State: domainmedia.JobStateFailed, Attempts: 5, MaxAttempts: 5,
		ErrorCode: "transcode_failed", ProcessingStep: domainmedia.ProcessingStepFailed,
		NextAttemptAt: now.Add(-time.Hour), CompletedAt: &completedAt,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	video := mediaAdminVideoTestModel{
		AuthorID: 4, Title: "Rollback",
		MediaAssetID: int64Pointer(asset.ID),
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatal(err)
	}
	_, err := repository.CommitAdminProcessingRetry(
		context.Background(),
		domainmedia.AdminProcessingRetryCommand{
			ActorID: 7, JobID: job.ID, VideoID: video.ID,
			ReasonCode:     domainmedia.ProcessingRetryReasonTemporaryFailure,
			Route:          "/api/admin/media-processing/jobs/:jobId/retry",
			IdempotencyKey: "rollback-key", OccurredAt: now,
		},
		func(input domainmedia.ProcessingRetryAuditInput) (*domainadminaudit.Fact, error) {
			digest, digestErr := domainadminaudit.DigestIdempotencyKey("rollback-key")
			if digestErr != nil {
				return nil, digestErr
			}
			return domainadminaudit.NewFact(domainadminaudit.FactInput{
				ActorID: 7, Permission: domainaccount.PermissionContentEnforce,
				Action:     domainadminaudit.ActionMediaProcessingRetry,
				TargetType: domainadminaudit.TargetMediaProcessingJob,
				TargetID:   fmt.Sprint(job.ID), Outcome: domainadminaudit.OutcomeSuccess,
				RequestID: domainadminaudit.NewRequestID(), IdempotencyKeyHash: digest,
				CreatedAt: now,
				Detail: map[string]string{
					"http_method":    "POST",
					"route":          "/api/admin/media-processing/jobs/:jobId/retry",
					"reason_code":    domainmedia.ProcessingRetryReasonTemporaryFailure,
					"video_id":       fmt.Sprint(input.VideoID),
					"previous_state": input.PreviousState, "new_state": input.NewState,
					"previous_attempts": fmt.Sprint(input.PreviousAttempts),
				},
			})
		},
	)
	if err == nil {
		t.Fatal("expected audit failure")
	}
	var current ProcessingJobModel
	if err := db.First(&current, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.State != domainmedia.JobStateFailed || current.Attempts != 5 {
		t.Fatalf("job changed despite rollback: %+v", current)
	}
	var receiptCount, outboxCount int64
	if err := db.Model(&ProcessingRetryReceiptModel{}).Count(&receiptCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&ProcessingRetryOutboxModel{}).Count(&outboxCount).Error; err != nil {
		t.Fatal(err)
	}
	if receiptCount != 0 || outboxCount != 0 {
		t.Fatalf("receipt=%d outbox=%d", receiptCount, outboxCount)
	}
}

func TestVideoLifecycleTaskPostgreSQLDuplicateLeaseAndReclaim(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set")
	}
	db := openMediaPostgres(t, dsn)
	repository := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	task, err := domainmedia.NewVideoLifecycleTask(
		"video-private:7:1", 7, 11, 12,
		domainmedia.LifecycleActionProtect, 0, "private", 3, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := AppendVideoLifecycleTask(tx, task); err != nil {
			return err
		}
		return AppendVideoLifecycleTask(tx, task)
	}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&VideoLifecycleTaskModel{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("duplicate count=%d err=%v", count, err)
	}
	leased, err := repository.ClaimVideoLifecycleTasks(
		context.Background(), "owner-one", now, now.Add(time.Minute), 1,
	)
	if err != nil || len(leased) != 1 || leased[0].Attempts != 1 {
		t.Fatalf("first lease=%+v err=%v", leased, err)
	}
	if second, err := repository.ClaimVideoLifecycleTasks(
		context.Background(), "owner-two", now.Add(30*time.Second),
		now.Add(90*time.Second), 1,
	); err != nil || len(second) != 0 {
		t.Fatalf("live lease reclaimed=%+v err=%v", second, err)
	}
	reclaimed, err := repository.ClaimVideoLifecycleTasks(
		context.Background(), "owner-two", now.Add(2*time.Minute),
		now.Add(3*time.Minute), 1,
	)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].Attempts != 2 {
		t.Fatalf("expired lease reclaim=%+v err=%v", reclaimed, err)
	}
	count, oldest, err := repository.VideoLifecycleBacklog(context.Background())
	if err != nil || count != 1 || oldest == nil {
		t.Fatalf("lifecycle backlog count=%d oldest=%v err=%v", count, oldest, err)
	}
	finishedAt := now.Add(2 * time.Minute)
	leased[0].State = domainmedia.JobStateCompleted
	leased[0].CompletedAt = &finishedAt
	if err := repository.UpdateVideoLifecycleTaskOwned(
		context.Background(), leased[0], "owner-one",
	); !errors.Is(err, domainmedia.ErrLifecycleTaskLeaseLost) {
		t.Fatalf("stale owner error=%v", err)
	}
	reclaimed[0].State = domainmedia.JobStateCompleted
	reclaimed[0].ErrorCode = "success"
	reclaimed[0].CompletedAt = &finishedAt
	if err := repository.UpdateVideoLifecycleTaskOwned(
		context.Background(), reclaimed[0], "owner-two",
	); err != nil {
		t.Fatal(err)
	}
	count, oldest, err = repository.VideoLifecycleBacklog(context.Background())
	if err != nil || count != 0 || oldest != nil {
		t.Fatalf("completed lifecycle backlog count=%d oldest=%v err=%v", count, oldest, err)
	}
}

func TestUploadSessionPostgreSQLIdempotentReplayIgnoresNewGeneratedID(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set")
	}
	db := openMediaPostgres(t, dsn)
	repository := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	first, err := domainmedia.NewUploadSession(
		"session-one", 7, domainmedia.AssetKindVideo, domainmedia.StorageBackendS3,
		"uploads/7/session-one/video/source.mp4", "video/mp4", 1024,
		strings.Repeat("a", 64), "same-key", "same-fingerprint",
		now.Add(time.Hour), now,
	)
	if err != nil {
		t.Fatal(err)
	}
	stored, created, err := repository.CreateUploadSession(context.Background(), first)
	if err != nil || !created {
		t.Fatalf("first session = %#v created=%v err=%v", stored, created, err)
	}
	replay := *first
	replay.ID = "session-two"
	replay.ObjectKey = "uploads/7/session-two/video/source.mp4"
	stored, created, err = repository.CreateUploadSession(context.Background(), &replay)
	if err != nil || created || stored.ID != first.ID {
		t.Fatalf("replay session = %#v created=%v err=%v", stored, created, err)
	}
	replay.RequestFingerprint = "different"
	if _, _, err := repository.CreateUploadSession(
		context.Background(), &replay,
	); !errors.Is(err, domainmedia.ErrUploadSessionConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
}

func TestProtectedAssetAccessUsesPostgreSQLReadyVariant(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set")
	}
	db := openMediaPostgres(t, dsn)
	repository := New(db)
	asset := AssetModel{
		OwnerID: 7, Kind: domainmedia.AssetKindVideo,
		StorageBackend: domainmedia.StorageBackendS3,
		ObjectKey:      "uploads/7/source.mov", ContentType: "video/quicktime",
		SizeBytes: 100, ChecksumSHA256: strings.Repeat("a", 64),
		State: domainmedia.AssetStateReady,
	}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&VariantModel{
		AssetID: asset.ID, ProfileVersion: "v1",
		SourceType: domainmedia.SourceTypeMP4, Format: "mp4",
		ObjectKey: "processed/7/baseline.mp4",
		Role:      domainmedia.VariantRoleBaseline, SortOrder: 10,
		State:          domainmedia.VariantStateReady,
		ChecksumSHA256: strings.Repeat("b", 64), SizeBytes: 80,
	}).Error; err != nil {
		t.Fatal(err)
	}
	resolver := &postgresProtectedResolver{}
	service := applicationmedia.New(
		repository, nil, domainmedia.StorageBackendS3,
		time.Minute, "v1", 3,
		applicationmedia.WithURLResolver(resolver, 5*time.Minute),
	)
	access, err := service.GetProtectedAssetAccess(
		context.Background(), 7, asset.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.objectKey != "processed/7/baseline.mp4" ||
		access.URL != "https://protected.example/processed/7/baseline.mp4" {
		t.Fatalf("protected access=%#v key=%q", access, resolver.objectKey)
	}
	if _, err := service.GetProtectedAssetAccess(
		context.Background(), 8, asset.ID,
	); !errors.Is(err, domainmedia.ErrMediaAssetPermissionDenied) {
		t.Fatalf("non-owner access error = %v", err)
	}
}

type postgresProtectedResolver struct {
	objectKey string
}

func (*postgresProtectedResolver) PublicURL(string) (string, error) {
	return "", domainmedia.ErrPresignUnsupported
}

func (r *postgresProtectedResolver) ProtectedURL(
	_ context.Context,
	objectKey string,
	expiry time.Duration,
) (string, time.Time, error) {
	r.objectKey = objectKey
	return "https://protected.example/" + objectKey, time.Now().UTC().Add(expiry), nil
}

func TestVariantExposurePostgreSQLCompareAndSwap(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set")
	}
	db := openMediaPostgres(t, dsn)
	repository := New(db)
	model := VariantModel{
		AssetID: 31, ProfileVersion: "v2", SourceType: domainmedia.SourceTypeMP4,
		Format: "mp4", ObjectKey: "processed/31/v2/checksum/source.mp4",
		Role: domainmedia.VariantRoleBaseline, State: domainmedia.VariantStateReady,
		ChecksumSHA256: strings.Repeat("a", 64), SizeBytes: 100,
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := repository.UpdateVariantExposure(
		context.Background(), model.ID, model.ObjectKey, false, "", true, "generation-a",
	)
	if err != nil || !updated {
		t.Fatalf("publish updated=%t err=%v", updated, err)
	}
	if stale, err := repository.UpdateVariantExposure(
		context.Background(), model.ID, model.ObjectKey, false, "", true, "generation-b",
	); err != nil || stale {
		t.Fatalf("stale publish updated=%t err=%v", stale, err)
	}
	updated, err = repository.UpdateVariantExposure(
		context.Background(), model.ID, model.ObjectKey, true, "generation-a", false, "",
	)
	if err != nil || !updated {
		t.Fatalf("protect updated=%t err=%v", updated, err)
	}
	var stored VariantModel
	if err := db.Where("id = ?", model.ID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Public || stringValue(stored.ExposureGeneration) != "" {
		t.Fatalf("stored variant = %+v", stored)
	}
}

func openMediaPostgres(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("frux_media_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = admin.Close()
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	sqlDB, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(
		gormpostgres.New(gormpostgres.Config{Conn: sqlDB}),
		&gorm.Config{TranslateError: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&CleanupTaskModel{}, &VideoLifecycleTaskModel{},
		&UploadSessionModel{}, &AssetModel{}, &VariantModel{}, &ProcessingJobModel{},
		&ProcessingRetryReceiptModel{}, &ProcessingRetryOutboxModel{},
		&mediaAdminVideoTestModel{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

type mediaAuditWriterStub struct{}

func (mediaAuditWriterStub) AppendInTransaction(
	context.Context,
	*gorm.DB,
	*domainadminaudit.Fact,
) error {
	return nil
}

func (mediaAuditWriterStub) RecordCommittedWrite(*domainadminaudit.Fact) {}

type failingMediaAuditWriterStub struct{}

func (failingMediaAuditWriterStub) AppendInTransaction(
	context.Context,
	*gorm.DB,
	*domainadminaudit.Fact,
) error {
	return errors.New("audit unavailable")
}

func (failingMediaAuditWriterStub) RecordCommittedWrite(*domainadminaudit.Fact) {}

func int64Pointer(value int64) *int64 {
	return &value
}

type mediaAdminVideoTestModel struct {
	ID           int64  `gorm:"column:id;primaryKey;autoIncrement"`
	AuthorID     int64  `gorm:"column:author_id;not null"`
	Title        string `gorm:"column:title;size:128;not null"`
	MediaAssetID *int64 `gorm:"column:media_asset_id;uniqueIndex"`
}

func (mediaAdminVideoTestModel) TableName() string {
	return "video"
}
