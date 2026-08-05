package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainexposure "github.com/shiyudesu/frux/internal/domain/exposure"
	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	domainlibrary "github.com/shiyudesu/frux/internal/domain/library"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainplayback "github.com/shiyudesu/frux/internal/domain/playback"
	domainrelation "github.com/shiyudesu/frux/internal/domain/relation"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infraaccount "github.com/shiyudesu/frux/internal/infra/persistence/account"
	infraembedding "github.com/shiyudesu/frux/internal/infra/persistence/embedding"
	infraexposure "github.com/shiyudesu/frux/internal/infra/persistence/exposure"
	infrafeed "github.com/shiyudesu/frux/internal/infra/persistence/feed"
	infrainteraction "github.com/shiyudesu/frux/internal/infra/persistence/interaction"
	infralibrary "github.com/shiyudesu/frux/internal/infra/persistence/library"
	inframedia "github.com/shiyudesu/frux/internal/infra/persistence/media"
	inframessage "github.com/shiyudesu/frux/internal/infra/persistence/message"
	infraplayback "github.com/shiyudesu/frux/internal/infra/persistence/playback"
	infrarecommendation "github.com/shiyudesu/frux/internal/infra/persistence/recommendation"
	infrarelation "github.com/shiyudesu/frux/internal/infra/persistence/relation"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const postgresTestDSNEnv = "FRUX_POSTGRES_TEST_DSN"

type postgresFixture struct {
	admin  *sql.DB
	dsn    string
	schema string
}

type queryCounterLogger struct {
	logger.Interface
	count atomic.Int64
}

func (l *queryCounterLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	l.count.Add(1)
	l.Interface.Trace(ctx, begin, fc, err)
}

func newPostgresFixture(t *testing.T) *postgresFixture {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv(postgresTestDSNEnv))
	if dsn == "" {
		t.Skipf("%s is not set; skipping real PostgreSQL integration test", postgresTestDSNEnv)
	}

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL test database: %v", err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatalf("ping PostgreSQL test database: %v", err)
	}

	schema := fmt.Sprintf("frux_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = admin.Close()
		t.Fatalf("create PostgreSQL test schema: %v", err)
	}

	fixture := &postgresFixture{admin: admin, dsn: dsn, schema: schema}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop PostgreSQL test schema: %v", err)
		}
		if err := admin.Close(); err != nil {
			t.Errorf("close PostgreSQL admin connection: %v", err)
		}
	})
	return fixture
}

func (f *postgresFixture) openGORM(t *testing.T) *gorm.DB {
	return f.openGORMWithMaxOpenConns(t, 20)
}

func (f *postgresFixture) openGORMWithMaxOpenConns(t *testing.T, maxOpenConns int) *gorm.DB {
	t.Helper()

	connConfig, err := pgx.ParseConfig(postgresDSNWithSchema(f.dsn, f.schema))
	if err != nil {
		t.Fatalf("parse schema PostgreSQL connection: %v", err)
	}
	sqlDB := stdlib.OpenDB(*connConfig, stdlib.OptionAfterConnect(configureTestUTCCodecs))
	sqlDB.SetMaxIdleConns(maxOpenConns)
	sqlDB.SetMaxOpenConns(maxOpenConns)
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		t.Fatalf("ping schema PostgreSQL connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close schema PostgreSQL connection: %v", err)
		}
	})

	db, err := gorm.Open(
		gormpostgres.New(gormpostgres.Config{Conn: sqlDB}),
		&gorm.Config{TranslateError: true},
	)
	if err != nil {
		t.Fatalf("open GORM PostgreSQL connection: %v", err)
	}

	var currentSchema string
	if err := db.Raw("SELECT current_schema()").Scan(&currentSchema).Error; err != nil {
		t.Fatalf("read current schema: %v", err)
	}
	if currentSchema != f.schema {
		t.Fatalf("expected schema %q, got %q", f.schema, currentSchema)
	}
	return db
}

func configureTestUTCCodecs(_ context.Context, conn *pgx.Conn) error {
	conn.TypeMap().RegisterType(&pgtype.Type{
		Name:  "timestamp",
		OID:   pgtype.TimestampOID,
		Codec: &pgtype.TimestampCodec{ScanLocation: time.UTC},
	})
	conn.TypeMap().RegisterType(&pgtype.Type{
		Name:  "timestamptz",
		OID:   pgtype.TimestamptzOID,
		Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC},
	})
	return nil
}

func postgresDSNWithSchema(dsn string, schema string) string {
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

func TestPostgreSQLMigration(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("clean migration: %v", err)
	}
	if err := db.Model(&infrarecommendation.PolicyModel{}).
		Where("scene = ? AND version = ?", "recommend", 1).
		Update("enabled", false).Error; err != nil {
		t.Fatalf("set operator policy state: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	var bootstrapPolicies []infrarecommendation.PolicyModel
	if err := db.Where("scene = ?", "recommend").Order("version").Find(&bootstrapPolicies).Error; err != nil {
		t.Fatalf("load bootstrap policies: %v", err)
	}
	if len(bootstrapPolicies) != 2 || bootstrapPolicies[0].Version != 1 || bootstrapPolicies[0].Enabled ||
		bootstrapPolicies[1].Version != 2 || !bootstrapPolicies[1].Enabled {
		t.Fatalf("bootstrap policies were missing or overwritten: %#v", bootstrapPolicies)
	}

	requiredTables := []string{
		"account",
		"account_profile_setting",
		"video_embedding",
		"video",
		"media_asset",
		"media_variant",
		"media_processing_profile",
		"media_processing_job",
		"media_upload_session",
		"media_cleanup_task",
		"local_upload_asset",
		"video_stat",
		"user_content_stat",
		"video_collection",
		"video_collection_item",
		"video_batch_operation",
		"feed_inbox",
		"video_view_events",
		"exposures",
		"video_view_history",
		"video_view_history_deletion",
		"view_event_outbox",
		"recommendation_behavior_event",
		"recommendation_policy",
		"user_interest_profile",
		"recommendation_applied_profile_event",
		"recommendation_feedback",
		"recommendation_feedback_profile_outbox",
		"recommendation_request_log",
		"recommendation_served_candidate",
		"recommendation_outcome",
		"user_watch_later",
		"interaction_action",
		"interaction_action_event",
		"interaction_comment",
		"interaction_comment_like",
		"interaction_comment_like_idempotency_receipt",
		"user_message",
		"playback_config",
		"playback_qos_log",
		"playback_telemetry_batch",
		"playback_telemetry_event",
		"user_follow",
		"user_relation_stat",
		"relation_profile_projection_outbox",
		"app_migration",
	}
	for _, table := range requiredTables {
		if !db.Migrator().HasTable(table) {
			t.Errorf("missing table %s", table)
		}
	}

	requiredIndexes := []struct {
		model any
		name  string
	}{
		{&infraaccount.UserModel{}, "uk_account_account"},
		{&infravideo.VideoModel{}, "idx_video_timeline"},
		{&infravideo.VideoModel{}, "idx_video_public_timeline"},
		{&inframedia.AssetModel{}, "uk_media_asset_backend_key"},
		{&inframedia.VariantModel{}, "idx_media_variant_video_order"},
		{&inframedia.ProcessingJobModel{}, "uk_media_processing_job_asset_profile"},
		{&inframedia.UploadSessionModel{}, "idx_media_upload_session_expiry"},
		{&infrafeed.InboxModel{}, "uk_feed_inbox_user_video"},
		{&infraexposure.ExposureModel{}, "uk_exposures_user_video"},
		{&infraexposure.ViewEventModel{}, "uk_video_view_events_user_event"},
		{&infraexposure.ViewHistoryModel{}, "idx_video_view_history_user_last"},
		{&infraexposure.ViewEventOutboxModel{}, "idx_view_event_outbox_pending"},
		{&infrarecommendation.BehaviorEventModel{}, "idx_recommendation_behavior_user_occurred"},
		{&infrarecommendation.PolicyModel{}, "uk_recommendation_policy_scene_version"},
		{&infrarecommendation.PolicyModel{}, "idx_recommendation_policy_scene_enabled_version"},
		{&infrarecommendation.UserInterestProfileModel{}, "idx_recommendation_profile_updated"},
		{&infrarecommendation.AppliedProfileEventModel{}, "idx_recommendation_applied_profile_event_time"},
		{&infrarecommendation.FeedbackModel{}, "uk_recommendation_feedback_user_key"},
		{&infrarecommendation.FeedbackProfileOutboxModel{}, "idx_recommendation_feedback_profile_outbox_pending"},
		{&infrarecommendation.RequestLogModel{}, "uk_recommendation_request_log_user_request"},
		{&infrarecommendation.RequestLogModel{}, "idx_recommendation_request_log_created"},
		{&infrarecommendation.ServedCandidateEvidenceModel{}, "uk_recommendation_served_candidate_identity"},
		{&infrarecommendation.ServedCandidateEvidenceModel{}, "idx_recommendation_served_candidate_request"},
		{&infrarecommendation.ServedCandidateEvidenceModel{}, "idx_recommendation_served_candidate_expiry"},
		{&infrarecommendation.OutcomeModel{}, "idx_recommendation_outcome_request"},
		{&infrainteraction.ActionModel{}, "idx_interaction_action_user_type_status_updated"},
		{&infrainteraction.CommentModel{}, "idx_interaction_comment_root_latest"},
		{&infrainteraction.CommentModel{}, "idx_interaction_comment_root_hot"},
		{&infrainteraction.CommentModel{}, "idx_interaction_comment_replies"},
		{&infrainteraction.CommentModel{}, "idx_interaction_comment_direct_target"},
		{&infrainteraction.CommentLikeModel{}, "uk_interaction_comment_like_user_comment"},
		{&infrainteraction.CommentLikeModel{}, "idx_interaction_comment_like_comment_status"},
		{&infrainteraction.CommentNotificationOutboxModel{}, "idx_interaction_comment_notification_outbox_pending"},
		{&infralibrary.WatchLaterModel{}, "idx_user_watch_later_user_status_updated"},
		{&infrarelation.FollowProfileOutboxModel{}, "idx_relation_profile_outbox_pending"},
		{&inframessage.MessageModel{}, "uk_user_message_user_event"},
		{&infraplayback.TelemetryBatchModel{}, "uk_playback_telemetry_batch_user_batch"},
		{&infraplayback.TelemetryBatchModel{}, "uk_playback_telemetry_batch_anon_batch"},
		{&infraplayback.TelemetryEventModel{}, "uk_playback_telemetry_event_user_event"},
		{&infraplayback.TelemetryEventModel{}, "uk_playback_telemetry_event_anon_event"},
		{&infraplayback.TelemetryEventModel{}, "idx_playback_telemetry_event_created"},
	}
	for _, index := range requiredIndexes {
		if !db.Migrator().HasIndex(index.model, index.name) {
			t.Errorf("missing index %s", index.name)
		}
		var threadedMarkerCount int64
		if err := db.Model(&markerModel{}).Where("key = ?", threadedCommentBackfillKey).Count(&threadedMarkerCount).Error; err != nil {
			t.Fatalf("load threaded comment migration marker: %v", err)
		}
		if threadedMarkerCount != 1 {
			t.Fatalf("expected one threaded comment migration marker, got %d", threadedMarkerCount)
		}
	}

	assertColumnType(t, db, "account", "status", "smallint")
	assertColumnType(t, db, "video", "status", "smallint")
	assertColumnType(t, db, "account", "gender", "smallint")
	assertColumnType(t, db, "video_embedding", "embedding_json", "jsonb")

	var timeZone string
	if err := db.Raw("SHOW TIME ZONE").Scan(&timeZone).Error; err != nil {
		t.Fatalf("show time zone: %v", err)
	}
	if !strings.EqualFold(timeZone, "UTC") {
		t.Fatalf("expected UTC time zone, got %q", timeZone)
	}

	user := infraaccount.UserModel{
		ID: 1, Account: "migration-user", Password: "hash", Nickname: "migration user",
		Status: domainaccount.StatusNormal, Role: domainaccount.RoleUser,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create migration account: %v", err)
	}

	publishedAt := time.Now().UTC()
	video := infravideo.VideoModel{
		AuthorID:    1,
		Title:       "migration backfill",
		MediaURL:    "/uploads/video/migration.mp4",
		CoverURL:    "/uploads/cover/migration.jpg",
		Status:      domainvideo.StatusPublished,
		PublishedAt: &publishedAt,
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatalf("create video without stat: %v", err)
	}
	if video.ID == 0 {
		t.Fatal("expected PostgreSQL-generated video ID")
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migration with stat backfill: %v", err)
	}
	if video.Visibility != domainvideo.VisibilityPublic {
		var restored infravideo.VideoModel
		if err := db.Where("id = ?", video.ID).Take(&restored).Error; err != nil {
			t.Fatalf("reload migrated video: %v", err)
		}
		if restored.Visibility != domainvideo.VisibilityPublic {
			t.Fatalf("expected existing video public, got %q", restored.Visibility)
		}
	}
	var migratedAssets []infravideo.LocalAssetModel
	if err := db.Order("asset_url").Find(&migratedAssets).Error; err != nil {
		t.Fatalf("load backfilled local assets: %v", err)
	}
	if len(migratedAssets) != 2 {
		t.Fatalf("expected two backfilled local assets, got %+v", migratedAssets)
	}
	for _, asset := range migratedAssets {
		if asset.OwnerID != 1 {
			t.Fatalf("unexpected backfilled owner: %+v", asset)
		}
	}
	var migratedVideo infravideo.VideoModel
	if err := db.Where("id = ?", video.ID).Take(&migratedVideo).Error; err != nil {
		t.Fatalf("reload legacy-ready video: %v", err)
	}
	if migratedVideo.MediaStatus != domainmedia.MediaStatusLegacyReady {
		t.Fatalf("expected legacy media status, got %q", migratedVideo.MediaStatus)
	}

	if err := db.Model(&infravideo.VideoStatModel{}).Where("video_id = ?", video.ID).Update("like_count", 5).Error; err != nil {
		t.Fatalf("seed video likes: %v", err)
	}
	privateVideo := infravideo.VideoModel{
		AuthorID: 1, Title: "private migration", MediaURL: "/uploads/private.mp4",
		CoverURL: "/uploads/private.jpg", Status: domainvideo.StatusPublished,
		Visibility: domainvideo.VisibilityPrivate, PublishedAt: &publishedAt,
	}
	if err := db.Create(&privateVideo).Error; err != nil {
		t.Fatalf("create private video: %v", err)
	}
	offlineVideo := infravideo.VideoModel{
		AuthorID: 1, Title: "offline migration", MediaURL: "/uploads/offline.mp4",
		CoverURL: "/uploads/offline.jpg", Status: domainvideo.StatusOffline,
		Visibility: domainvideo.VisibilityPublic, PublishedAt: &publishedAt,
	}
	if err := db.Create(&offlineVideo).Error; err != nil {
		t.Fatalf("create offline video: %v", err)
	}
	if err := db.Create(&infravideo.CollectionModel{
		OwnerID: 1, Title: "migration collection", Visibility: domainvideo.VisibilityPublic,
		Status: domainvideo.CollectionStatusActive,
	}).Error; err != nil {
		t.Fatalf("create migration collection: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migration with aggregate backfill: %v", err)
	}

	var setting infraaccount.ProfileSettingModel
	if err := db.Where("user_id = ?", 1).Take(&setting).Error; err != nil {
		t.Fatalf("load profile setting: %v", err)
	}
	if setting.LikedVisibility != domainaccount.ProfileVisibilityPrivate || setting.FavoriteVisibility != domainaccount.ProfileVisibilityPrivate {
		t.Fatalf("unexpected profile privacy defaults: %+v", setting)
	}
	var contentStat infravideo.UserContentStatModel
	if err := db.Where("user_id = ?", 1).Take(&contentStat).Error; err != nil {
		t.Fatalf("load content stat: %v", err)
	}
	if contentStat.PublicWorkCount != 1 || contentStat.PrivateWorkCount != 1 || contentStat.ReceivedLikeCount != 5 || contentStat.CollectionCount != 1 {
		t.Fatalf("unexpected content stat backfill: %+v", contentStat)
	}
	var missingStats int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM video AS v
		LEFT JOIN video_stat AS vs ON vs.video_id = v.id
		WHERE vs.video_id IS NULL
	`).Scan(&missingStats).Error; err != nil {
		t.Fatalf("count videos without stats: %v", err)
	}
	if missingStats != 0 {
		t.Fatalf("expected complete video_stat rows, found %d missing", missingStats)
	}
}

func TestPostgreSQLMigrationReplacesGlobalRequestLogIdentity(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("initial migration: %v", err)
	}
	if err := db.Exec("DROP INDEX IF EXISTS uk_recommendation_request_log_user_request").Error; err != nil {
		t.Fatalf("drop composite request-log index: %v", err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX uk_recommendation_request_log_request ON recommendation_request_log (request_id)").Error; err != nil {
		t.Fatalf("create legacy global request-log index: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("replace legacy request-log index: %v", err)
	}
	if db.Migrator().HasIndex(&infrarecommendation.RequestLogModel{}, "uk_recommendation_request_log_request") {
		t.Fatal("legacy global request-log identity index still exists")
	}
	if !db.Migrator().HasIndex(&infrarecommendation.RequestLogModel{}, "uk_recommendation_request_log_user_request") {
		t.Fatal("composite request-log identity index was not created")
	}

	now := time.Now().UTC()
	logs := []infrarecommendation.RequestLogModel{
		{RequestID: "shared-request", UserID: 1, Scene: "recommend", PolicyVersion: 1, PayloadJSON: `{"candidates":[]}`, CreatedAt: now},
		{RequestID: "shared-request", UserID: 2, Scene: "recommend", PolicyVersion: 1, PayloadJSON: `{"candidates":[]}`, CreatedAt: now},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("same request ID should be independent per user: %v", err)
	}
	if err := db.Create(&infrarecommendation.RequestLogModel{
		RequestID: "shared-request", UserID: 1, Scene: "recommend", PolicyVersion: 1, PayloadJSON: `{"candidates":[]}`, CreatedAt: now,
	}).Error; err == nil {
		t.Fatal("same user request-log identity was not unique")
	}
}

func TestPostgreSQLPlaybackTelemetryBatchWrite(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate telemetry schema: %v", err)
	}

	repository := infraplayback.New(db)
	sentAt := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	first := newTelemetryPersistenceBatch(t, "batch-1", sentAt, []domainplayback.NewTelemetryEventInput{{
		EventID: "event-1", EventType: domainplayback.TelemetryEventLoadStart,
	}})
	result, err := repository.CreateTelemetryBatch(context.Background(), first)
	if err != nil {
		t.Fatalf("create telemetry batch: %v", err)
	}
	if !result.Created || result.AcceptedCount != 1 || result.DuplicateCount != 0 {
		t.Fatalf("unexpected first telemetry result: %+v", result)
	}

	replay, err := repository.CreateTelemetryBatch(context.Background(), first)
	if err != nil {
		t.Fatalf("replay telemetry batch: %v", err)
	}
	if replay.Created || replay.AcceptedCount != 1 || replay.DuplicateCount != 0 {
		t.Fatalf("unexpected replay result: %+v", replay)
	}

	second := newTelemetryPersistenceBatch(t, "batch-2", sentAt.Add(time.Second), []domainplayback.NewTelemetryEventInput{
		{EventID: "event-1", EventType: domainplayback.TelemetryEventLoadStart},
		{EventID: "event-2", EventType: domainplayback.TelemetryEventMetadataReady, OffsetMs: 25},
	})
	result, err = repository.CreateTelemetryBatch(context.Background(), second)
	if err != nil {
		t.Fatalf("create partially duplicate telemetry batch: %v", err)
	}
	if result.AcceptedCount != 1 || result.DuplicateCount != 1 {
		t.Fatalf("unexpected duplicate accounting: %+v", result)
	}

	batchConflict := newTelemetryPersistenceBatch(t, "batch-1", sentAt, []domainplayback.NewTelemetryEventInput{{
		EventID: "event-1", EventType: domainplayback.TelemetryEventMetadataReady,
	}})
	if _, err := repository.CreateTelemetryBatch(context.Background(), batchConflict); !errors.Is(err, domainplayback.ErrTelemetryBatchConflict) {
		t.Fatalf("expected batch conflict, got %v", err)
	}

	eventConflict := newTelemetryPersistenceBatch(t, "batch-3", sentAt.Add(2*time.Second), []domainplayback.NewTelemetryEventInput{{
		EventID: "event-1", EventType: domainplayback.TelemetryEventMetadataReady,
	}})
	if _, err := repository.CreateTelemetryBatch(context.Background(), eventConflict); !errors.Is(err, domainplayback.ErrTelemetryEventConflict) {
		t.Fatalf("expected event conflict, got %v", err)
	}
	var rolledBackBatches int64
	if err := db.Model(&infraplayback.TelemetryBatchModel{}).Where("batch_id = ?", "batch-3").Count(&rolledBackBatches).Error; err != nil {
		t.Fatalf("count rolled back telemetry batch: %v", err)
	}
	if rolledBackBatches != 0 {
		t.Fatalf("event conflict left a partial batch row: %d", rolledBackBatches)
	}

	oldCreatedAt := sentAt.Add(-8 * 24 * time.Hour)
	if err := db.Model(&infraplayback.TelemetryEventModel{}).Where("1 = 1").Update("created_at", oldCreatedAt).Error; err != nil {
		t.Fatalf("age telemetry events: %v", err)
	}
	if err := db.Model(&infraplayback.TelemetryBatchModel{}).Where("1 = 1").Update("created_at", oldCreatedAt).Error; err != nil {
		t.Fatalf("age telemetry batches: %v", err)
	}
	cleanup, err := repository.DeleteTelemetryBefore(context.Background(), sentAt.Add(-7*24*time.Hour), 100)
	if err != nil {
		t.Fatalf("cleanup telemetry: %v", err)
	}
	if cleanup.DeletedEvents != 2 || cleanup.DeletedBatches != 2 {
		t.Fatalf("unexpected telemetry cleanup result: %+v", cleanup)
	}
}

func newTelemetryPersistenceBatch(t *testing.T, batchID string, sentAt time.Time, events []domainplayback.NewTelemetryEventInput) *domainplayback.TelemetryBatch {
	t.Helper()
	batch, err := domainplayback.NewTelemetryBatch(domainplayback.NewTelemetryBatchInput{
		UserID: 42, SchemaVersion: domainplayback.TelemetrySchemaVersionV1,
		BatchID: batchID, PlaybackSessionID: "playback-1", ClientSentAt: sentAt,
		Context: domainplayback.TelemetryContext{
			VideoID: 7, Scene: "recommend",
			PlayerAdapter: domainplayback.TelemetryPlayerAdapterNativeMP4,
			SourceType:    domainplayback.TelemetrySourceMP4,
		},
		Events: events,
	})
	if err != nil {
		t.Fatalf("new telemetry persistence batch: %v", err)
	}
	return batch
}

func TestPostgreSQLMediaVariantOrdering(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate media schema: %v", err)
	}

	asset := &domainmedia.MediaAsset{
		OwnerID: 1, Kind: domainmedia.AssetKindVideo, StorageBackend: domainmedia.StorageBackendS3,
		ObjectKey: "assets/1/source.mp4", ContentType: "video/mp4", SizeBytes: 100,
		ChecksumSHA256: strings.Repeat("a", 64), State: domainmedia.AssetStateReady,
	}
	repo := inframedia.New(db)
	if err := repo.CreateAsset(context.Background(), asset); err != nil {
		t.Fatalf("create media asset: %v", err)
	}
	videoID := int64(91)
	variants := []*domainmedia.MediaVariant{
		{AssetID: asset.ID, VideoID: videoID, ProfileVersion: "v1", SourceType: domainmedia.SourceTypeMP4, Format: "mp4", ObjectKey: "assets/1/720.mp4", Role: domainmedia.VariantRoleRendition, SortOrder: 20, State: domainmedia.VariantStateReady, ChecksumSHA256: strings.Repeat("b", 64), SizeBytes: 70, Bitrate: 2_000_000, Public: true},
		{AssetID: asset.ID, VideoID: videoID, ProfileVersion: "v1", SourceType: domainmedia.SourceTypeDASH, Format: "mpd", ObjectKey: "assets/1/manifest.mpd", Role: domainmedia.VariantRoleManifest, SortOrder: 30, State: domainmedia.VariantStateReady, ChecksumSHA256: strings.Repeat("c", 64), SizeBytes: 10, Public: true},
		{AssetID: asset.ID, VideoID: videoID, ProfileVersion: "v1", SourceType: domainmedia.SourceTypeMP4, Format: "mp4", ObjectKey: "assets/1/baseline.mp4", Role: domainmedia.VariantRoleBaseline, SortOrder: 10, State: domainmedia.VariantStateReady, ChecksumSHA256: strings.Repeat("d", 64), SizeBytes: 80, Bitrate: 1_000_000, Public: true},
	}
	if err := repo.UpsertVariants(context.Background(), variants); err != nil {
		t.Fatalf("upsert media variants: %v", err)
	}
	byVideo, err := repo.ListReadyVariantsByVideoIDs(context.Background(), []int64{videoID})
	if err != nil {
		t.Fatalf("list media variants: %v", err)
	}
	got := byVideo[videoID]
	if len(got) != 3 {
		t.Fatalf("expected three variants, got %+v", got)
	}
	if got[0].Role != domainmedia.VariantRoleBaseline || got[1].Role != domainmedia.VariantRoleRendition || got[2].Role != domainmedia.VariantRoleManifest {
		t.Fatalf("unexpected deterministic order: %+v", got)
	}
}

func TestPostgreSQLViewHistoryBackfillDoesNotResurrectDeletedProjection(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)

	if err := db.AutoMigrate(
		&infraaccount.UserModel{},
		&infravideo.VideoModel{},
		&infraexposure.ViewEventModel{},
	); err != nil {
		t.Fatalf("migrate legacy history sources: %v", err)
	}
	users := []infraaccount.UserModel{
		{ID: 1, Account: "history-delete", Password: "hash", Nickname: "delete", Status: domainaccount.StatusNormal, Role: domainaccount.RoleUser},
		{ID: 2, Account: "history-clear", Password: "hash", Nickname: "clear", Status: domainaccount.StatusNormal, Role: domainaccount.RoleUser},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create history users: %v", err)
	}
	publishedAt := time.Now().UTC()
	videos := []infravideo.VideoModel{
		{AuthorID: 1, Title: "delete target", MediaURL: "https://example.com/delete.mp4", CoverURL: "https://example.com/delete.jpg", Status: domainvideo.StatusPublished, Visibility: domainvideo.VisibilityPublic, PublishedAt: &publishedAt},
		{AuthorID: 1, Title: "keep target", MediaURL: "https://example.com/keep.mp4", CoverURL: "https://example.com/keep.jpg", Status: domainvideo.StatusPublished, Visibility: domainvideo.VisibilityPublic, PublishedAt: &publishedAt},
		{AuthorID: 2, Title: "clear target", MediaURL: "https://example.com/clear.mp4", CoverURL: "https://example.com/clear.jpg", Status: domainvideo.StatusPublished, Visibility: domainvideo.VisibilityPublic, PublishedAt: &publishedAt},
	}
	if err := db.Create(&videos).Error; err != nil {
		t.Fatalf("create history videos: %v", err)
	}
	now := time.Now().UTC()
	events := []infraexposure.ViewEventModel{
		{UserID: 1, VideoID: videos[0].ID, Scene: "timeline", EventType: domainexposure.EventTypePlay, WatchMs: 100, CreatedAt: now.Add(-3 * time.Minute)},
		{UserID: 1, VideoID: videos[1].ID, Scene: "timeline", EventType: domainexposure.EventTypeComplete, WatchMs: 200, Completed: true, CreatedAt: now.Add(-2 * time.Minute)},
		{UserID: 2, VideoID: videos[2].ID, Scene: "timeline", EventType: domainexposure.EventTypeSkip, WatchMs: 50, CreatedAt: now.Add(-time.Minute)},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("create legacy view events: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("run initial history backfill: %v", err)
	}
	var histories int64
	if err := db.Model(&infraexposure.ViewHistoryModel{}).Count(&histories).Error; err != nil {
		t.Fatalf("count backfilled history: %v", err)
	}
	if histories != 3 {
		t.Fatalf("expected three backfilled histories, got %d", histories)
	}

	repo := infraexposure.New(db)
	if err := repo.DeleteHistory(context.Background(), 1, videos[0].ID); err != nil {
		t.Fatalf("delete one history item: %v", err)
	}
	if err := repo.ClearHistory(context.Background(), 2); err != nil {
		t.Fatalf("clear user history: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("rerun startup migration: %v", err)
	}

	assertHistoryCount := func(userID, videoID int64, want int64) {
		t.Helper()
		var count int64
		query := db.Model(&infraexposure.ViewHistoryModel{}).Where("user_id = ?", userID)
		if videoID > 0 {
			query = query.Where("video_id = ?", videoID)
		}
		if err := query.Count(&count).Error; err != nil {
			t.Fatalf("count history for user=%d video=%d: %v", userID, videoID, err)
		}
		if count != want {
			t.Fatalf("history resurrected for user=%d video=%d: got %d want %d", userID, videoID, count, want)
		}
	}
	assertHistoryCount(1, videos[0].ID, 0)
	assertHistoryCount(1, videos[1].ID, 1)
	assertHistoryCount(2, 0, 0)

	var markerCount int64
	if err := db.Model(&markerModel{}).Where("key = ?", viewHistoryBackfillKey).Count(&markerCount).Error; err != nil {
		t.Fatalf("count history migration marker: %v", err)
	}
	if markerCount != 1 {
		t.Fatalf("expected one durable history migration marker, got %d", markerCount)
	}
	var rawEvents int64
	if err := db.Model(&infraexposure.ViewEventModel{}).Count(&rawEvents).Error; err != nil {
		t.Fatalf("count preserved raw events: %v", err)
	}
	if rawEvents != 3 {
		t.Fatalf("history deletion changed raw events: got %d", rawEvents)
	}
}

func TestPostgreSQLAtomicProfileUpdateRollback(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	userModel := infraaccount.UserModel{
		Account: "atomic-profile-user", Password: "hash", Nickname: "before",
		Status: domainaccount.StatusNormal, Role: domainaccount.RoleUser,
	}
	if err := db.Create(&userModel).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := db.Create(&infraaccount.ProfileSettingModel{
		UserID:             userModel.ID,
		LikedVisibility:    domainaccount.ProfileVisibilityPrivate,
		FavoriteVisibility: domainaccount.ProfileVisibilityPrivate,
	}).Error; err != nil {
		t.Fatalf("create profile setting: %v", err)
	}
	if err := db.Exec(`
		CREATE FUNCTION reject_atomic_profile_setting_update() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'forced profile setting failure';
		END;
		$$ LANGUAGE plpgsql
	`).Error; err != nil {
		t.Fatalf("create failure function: %v", err)
	}
	if err := db.Exec(`
		CREATE TRIGGER reject_atomic_profile_setting_update
		BEFORE UPDATE ON account_profile_setting
		FOR EACH ROW EXECUTE FUNCTION reject_atomic_profile_setting_update()
	`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	repo := infraaccount.New(db)
	nickname := "after"
	public := domainaccount.ProfileVisibilityPublic
	profileUpdate := domainaccount.ProfileUpdate{UserID: userModel.ID, Nickname: &nickname}
	settingUpdate := domainaccount.ProfileSettingUpdate{UserID: userModel.ID, LikedVisibility: &public}
	if err := repo.UpdateProfileAndSetting(context.Background(), &profileUpdate, &settingUpdate); err == nil {
		t.Fatal("expected atomic update failure")
	}

	var storedUser infraaccount.UserModel
	if err := db.Where("id = ?", userModel.ID).Take(&storedUser).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	var storedSetting infraaccount.ProfileSettingModel
	if err := db.Where("user_id = ?", userModel.ID).Take(&storedSetting).Error; err != nil {
		t.Fatalf("reload setting: %v", err)
	}
	if storedUser.Nickname != "before" || storedSetting.LikedVisibility != domainaccount.ProfileVisibilityPrivate {
		t.Fatalf("atomic rollback failed: user=%+v setting=%+v", storedUser, storedSetting)
	}
}

func TestPostgreSQLConcurrentMigration(t *testing.T) {
	fixture := newPostgresFixture(t)
	first := fixture.openGORM(t)
	second := fixture.openGORM(t)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, db := range []*gorm.DB{first, second} {
		wg.Add(1)
		go func(db *gorm.DB) {
			defer wg.Done()
			<-start
			errs <- AutoMigrate(db)
		}(db)
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migration: %v", err)
		}
	}
	if !first.Migrator().HasTable(&infravideo.VideoModel{}) {
		t.Fatal("video table missing after concurrent migration")
	}
	if !first.Migrator().HasIndex(&infravideo.VideoModel{}, "idx_video_timeline") {
		t.Fatal("timeline index missing after concurrent migration")
	}
}

func TestPostgreSQLContentStatReconciliationPreservesConcurrentDelta(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	concurrentDB := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&infraaccount.UserModel{
		ID: 1, Account: "reconcile-user", Password: "hash", Nickname: "reconcile user",
		Status: domainaccount.StatusNormal, Role: domainaccount.RoleUser,
	}).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := infravideo.ReconcileContentStats(db); err != nil {
		t.Fatalf("create baseline content stat: %v", err)
	}

	publishedAt := time.Now().UTC()
	firstVideo := infravideo.VideoModel{
		AuthorID: 1, Title: "existing fact", MediaURL: "/uploads/reconcile-1.mp4",
		CoverURL: "/uploads/reconcile-1.jpg", Status: domainvideo.StatusPublished,
		Visibility: domainvideo.VisibilityPublic, PublishedAt: &publishedAt,
	}
	if err := db.Create(&firstVideo).Error; err != nil {
		t.Fatalf("create existing video fact: %v", err)
	}
	if err := db.Create(&infravideo.VideoStatModel{VideoID: firstVideo.ID}).Error; err != nil {
		t.Fatalf("create existing video stat: %v", err)
	}

	tx := concurrentDB.Begin()
	if tx.Error != nil {
		t.Fatalf("begin concurrent transaction: %v", tx.Error)
	}
	secondVideo := infravideo.VideoModel{
		AuthorID: 1, Title: "concurrent fact", MediaURL: "/uploads/reconcile-2.mp4",
		CoverURL: "/uploads/reconcile-2.jpg", Status: domainvideo.StatusPublished,
		Visibility: domainvideo.VisibilityPublic, PublishedAt: &publishedAt,
	}
	if err := tx.Create(&secondVideo).Error; err != nil {
		_ = tx.Rollback()
		t.Fatalf("create concurrent video fact: %v", err)
	}
	if err := tx.Create(&infravideo.VideoStatModel{VideoID: secondVideo.ID}).Error; err != nil {
		_ = tx.Rollback()
		t.Fatalf("create concurrent video stat: %v", err)
	}
	if err := infravideo.AdjustContentStat(tx, 1, 1, 0, 0, 0); err != nil {
		_ = tx.Rollback()
		t.Fatalf("apply concurrent aggregate delta: %v", err)
	}

	reconcileErr := make(chan error, 1)
	go func() {
		reconcileErr <- infravideo.ReconcileContentStats(db)
	}()
	select {
	case err := <-reconcileErr:
		_ = tx.Rollback()
		t.Fatalf("reconciliation completed before locked delta committed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit concurrent aggregate delta: %v", err)
	}
	if err := <-reconcileErr; err != nil {
		t.Fatalf("reconcile content stats: %v", err)
	}

	var contentStat infravideo.UserContentStatModel
	if err := db.Where("user_id = ?", 1).Take(&contentStat).Error; err != nil {
		t.Fatalf("load reconciled content stat: %v", err)
	}
	if contentStat.PublicWorkCount != 2 {
		t.Fatalf("reconciliation overwrote concurrent delta: %+v", contentStat)
	}
}

func TestPostgreSQLProfileBackendTransactions(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, user := range []infraaccount.UserModel{
		{ID: 1, Account: "creator-one", Password: "hash", Nickname: "creator one", Status: 1, Role: "user"},
		{ID: 2, Account: "creator-two", Password: "hash", Nickname: "creator two", Status: 1, Role: "user"},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatalf("create account: %v", err)
		}
	}

	videoRepo := infravideo.New(db)
	createVideo := func(authorID int64, title string) *domainvideo.Video {
		video, err := domainvideo.NewPublished(authorID, title, "", "/uploads/video.mp4", "/uploads/cover.jpg", "")
		if err != nil {
			t.Fatalf("new video: %v", err)
		}
		if err := videoRepo.Save(context.Background(), video); err != nil {
			t.Fatalf("save video: %v", err)
		}
		return video
	}
	first := createVideo(1, "first")
	second := createVideo(1, "second")
	concurrent := createVideo(1, "concurrent")
	other := createVideo(2, "other")

	if _, _, err := videoRepo.ApplyBatch(context.Background(), 1, domainvideo.BatchActionMakePrivate, []int64{first.ID, other.ID}, "mixed", "mixed-fingerprint"); !errors.Is(err, domainvideo.ErrVideoPermissionDenied) {
		t.Fatalf("expected mixed ownership rollback, got %v", err)
	}
	reloaded, err := videoRepo.FindByID(context.Background(), first.ID)
	if err != nil || reloaded.Visibility != domainvideo.VisibilityPublic {
		t.Fatalf("mixed batch changed owned video: video=%+v err=%v", reloaded, err)
	}

	operation, replayed, err := videoRepo.ApplyBatch(context.Background(), 1, domainvideo.BatchActionMakePrivate, []int64{first.ID, second.ID}, "private-batch", "fingerprint-a")
	if err != nil || replayed || len(operation.VideoIDs) != 2 {
		t.Fatalf("private batch failed: operation=%+v replayed=%v err=%v", operation, replayed, err)
	}
	_, replayed, err = videoRepo.ApplyBatch(context.Background(), 1, domainvideo.BatchActionMakePrivate, []int64{first.ID, second.ID}, "private-batch", "fingerprint-a")
	if err != nil || !replayed {
		t.Fatalf("batch replay failed: replayed=%v err=%v", replayed, err)
	}
	if _, _, err := videoRepo.ApplyBatch(context.Background(), 1, domainvideo.BatchActionDelete, []int64{first.ID}, "private-batch", "fingerprint-b"); !errors.Is(err, domainvideo.ErrBatchIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}

	var creatorStat infravideo.UserContentStatModel
	if err := db.Where("user_id = ?", 1).Take(&creatorStat).Error; err != nil {
		t.Fatalf("load creator stat: %v", err)
	}
	if creatorStat.PublicWorkCount != 1 || creatorStat.PrivateWorkCount != 2 {
		t.Fatalf("unexpected visibility counts: %+v", creatorStat)
	}

	visibilityErrs := make(chan error, 2)
	var visibilityWG sync.WaitGroup
	for _, key := range []string{"visibility-a", "visibility-b"} {
		visibilityWG.Add(1)
		go func(key string) {
			defer visibilityWG.Done()
			_, _, err := videoRepo.ApplyBatch(context.Background(), 1, domainvideo.BatchActionMakePrivate, []int64{concurrent.ID}, key, key)
			visibilityErrs <- err
		}(key)
	}
	visibilityWG.Wait()
	close(visibilityErrs)
	for err := range visibilityErrs {
		if err != nil {
			t.Fatalf("concurrent visibility update: %v", err)
		}
	}
	if err := db.Where("user_id = ?", 1).Take(&creatorStat).Error; err != nil {
		t.Fatalf("reload visibility stat: %v", err)
	}
	if creatorStat.PublicWorkCount != 0 || creatorStat.PrivateWorkCount != 3 {
		t.Fatalf("concurrent visibility double-adjusted counts: %+v", creatorStat)
	}

	lifecycle := createVideo(1, "lifecycle")
	if err := db.Where("user_id = ?", 1).Take(&creatorStat).Error; err != nil {
		t.Fatalf("load lifecycle stat: %v", err)
	}
	if creatorStat.PublicWorkCount != 1 {
		t.Fatalf("published public lifecycle video not counted: %+v", creatorStat)
	}
	lifecycle.Status = domainvideo.StatusOffline
	if err := videoRepo.UpdateStatus(context.Background(), lifecycle); err != nil {
		t.Fatalf("set lifecycle video offline: %v", err)
	}
	if err := db.Where("user_id = ?", 1).Take(&creatorStat).Error; err != nil {
		t.Fatalf("reload offline stat: %v", err)
	}
	if creatorStat.PublicWorkCount != 0 {
		t.Fatalf("offline public video remained in public count: %+v", creatorStat)
	}
	lifecycle.Status = domainvideo.StatusPublished
	if err := videoRepo.UpdateStatus(context.Background(), lifecycle); err != nil {
		t.Fatalf("restore lifecycle video: %v", err)
	}
	if err := db.Where("user_id = ?", 1).Take(&creatorStat).Error; err != nil {
		t.Fatalf("reload restored stat: %v", err)
	}
	if creatorStat.PublicWorkCount != 1 {
		t.Fatalf("restored published public video not counted: %+v", creatorStat)
	}

	collection, created, err := videoRepo.CreateCollection(context.Background(), mustCollection(t, 1, "series", domainvideo.VisibilityPublic, "collection-key"))
	if err != nil || !created {
		t.Fatalf("create collection: created=%v err=%v", created, err)
	}
	replayedCollection, created, err := videoRepo.CreateCollection(context.Background(), mustCollection(t, 1, "changed", domainvideo.VisibilityPrivate, "collection-key"))
	if err != nil || created || replayedCollection.ID != collection.ID {
		t.Fatalf("collection replay failed: collection=%+v created=%v err=%v", replayedCollection, created, err)
	}
	staleTitle, err := videoRepo.GetCollection(context.Background(), collection.ID)
	if err != nil {
		t.Fatalf("load collection for title update: %v", err)
	}
	staleDescription, err := videoRepo.GetCollection(context.Background(), collection.ID)
	if err != nil {
		t.Fatalf("load collection for description update: %v", err)
	}
	title := "concurrent title"
	titleUpdate := domainvideo.CollectionUpdate{Title: &title}
	if err := staleTitle.UpdateBy(1, titleUpdate); err != nil {
		t.Fatalf("validate collection title update: %v", err)
	}
	description := "concurrent description"
	descriptionUpdate := domainvideo.CollectionUpdate{Description: &description}
	if err := staleDescription.UpdateBy(1, descriptionUpdate); err != nil {
		t.Fatalf("validate collection description update: %v", err)
	}
	startCollectionUpdates := make(chan struct{})
	collectionUpdateErrors := make(chan error, 2)
	go func() {
		<-startCollectionUpdates
		collectionUpdateErrors <- videoRepo.UpdateCollection(context.Background(), staleTitle, titleUpdate)
	}()
	go func() {
		<-startCollectionUpdates
		collectionUpdateErrors <- videoRepo.UpdateCollection(context.Background(), staleDescription, descriptionUpdate)
	}()
	close(startCollectionUpdates)
	for range 2 {
		if err := <-collectionUpdateErrors; err != nil {
			t.Fatalf("concurrent collection update: %v", err)
		}
	}
	updatedCollection, err := videoRepo.GetCollection(context.Background(), collection.ID)
	if err != nil {
		t.Fatalf("reload concurrently updated collection: %v", err)
	}
	if updatedCollection.Title != title || updatedCollection.Description != description || updatedCollection.Visibility != domainvideo.VisibilityPublic {
		t.Fatalf("concurrent partial collection update lost a field: %+v", updatedCollection)
	}
	var collectionModel infravideo.CollectionModel
	if err := db.Where("id = ?", collection.ID).Take(&collectionModel).Error; err != nil {
		t.Fatalf("load collection before membership: %v", err)
	}
	beforeMembership := collectionModel.UpdatedAt
	if err := videoRepo.SetCollectionItem(context.Background(), 1, collection.ID, first.ID, true); err != nil {
		t.Fatalf("add collection item: %v", err)
	}
	if err := db.Where("id = ?", collection.ID).Take(&collectionModel).Error; err != nil {
		t.Fatalf("load collection after membership: %v", err)
	}
	afterAdd := collectionModel.UpdatedAt
	if !afterAdd.After(beforeMembership) {
		t.Fatalf("membership add did not touch collection: before=%s after=%s", beforeMembership, afterAdd)
	}
	if err := videoRepo.SetCollectionItem(context.Background(), 1, collection.ID, first.ID, true); err != nil {
		t.Fatalf("replay collection item: %v", err)
	}
	if err := db.Where("id = ?", collection.ID).Take(&collectionModel).Error; err != nil {
		t.Fatalf("load collection after add replay: %v", err)
	}
	if !collectionModel.UpdatedAt.Equal(afterAdd) {
		t.Fatalf("no-op add changed collection updated_at: first=%s replay=%s", afterAdd, collectionModel.UpdatedAt)
	}
	if err := videoRepo.SetCollectionItem(context.Background(), 1, collection.ID, second.ID, false); err != nil {
		t.Fatalf("remove absent collection item: %v", err)
	}
	if err := db.Where("id = ?", collection.ID).Take(&collectionModel).Error; err != nil {
		t.Fatalf("load collection after remove replay: %v", err)
	}
	if !collectionModel.UpdatedAt.Equal(afterAdd) {
		t.Fatalf("no-op remove changed collection updated_at: first=%s replay=%s", afterAdd, collectionModel.UpdatedAt)
	}
	if err := videoRepo.SetCollectionItem(context.Background(), 1, collection.ID, first.ID, false); err != nil {
		t.Fatalf("remove collection item: %v", err)
	}
	if err := db.Where("id = ?", collection.ID).Take(&collectionModel).Error; err != nil {
		t.Fatalf("load collection after remove: %v", err)
	}
	afterRemove := collectionModel.UpdatedAt
	if !afterRemove.After(afterAdd) {
		t.Fatalf("membership remove did not touch collection: add=%s remove=%s", afterAdd, afterRemove)
	}
	if err := videoRepo.SetCollectionItem(context.Background(), 1, collection.ID, first.ID, true); err != nil {
		t.Fatalf("restore collection item: %v", err)
	}
	ownerItems, err := videoRepo.ListCollectionItems(context.Background(), collection.ID, false)
	if err != nil || len(ownerItems) != 1 {
		t.Fatalf("unexpected owner items: items=%+v err=%v", ownerItems, err)
	}
	publicItems, err := videoRepo.ListCollectionItems(context.Background(), collection.ID, true)
	if err != nil || len(publicItems) != 0 {
		t.Fatalf("private member leaked publicly: items=%+v err=%v", publicItems, err)
	}

	if _, _, err := videoRepo.ApplyBatch(context.Background(), 1, domainvideo.BatchActionMakePublic, []int64{first.ID}, "public-first", "fingerprint-public"); err != nil {
		t.Fatalf("make first public: %v", err)
	}
	interactionRepo := infrainteraction.New(db)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, err := interactionRepo.SetAction(context.Background(), 2, first.ID, domaininteraction.ActionTypeLike, true, "same-like")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent like: %v", err)
		}
	}
	if err := db.Where("user_id = ?", 1).Take(&creatorStat).Error; err != nil {
		t.Fatalf("reload creator stat: %v", err)
	}
	if creatorStat.ReceivedLikeCount != 1 {
		t.Fatalf("expected exactly one received like, got %+v", creatorStat)
	}

	if err := infravideo.AdjustContentStat(db, 1, -100, -100, -100, -100); err != nil {
		t.Fatalf("clamp aggregate: %v", err)
	}
	if err := db.Where("user_id = ?", 1).Take(&creatorStat).Error; err != nil {
		t.Fatalf("reload clamped stat: %v", err)
	}
	if creatorStat.PublicWorkCount < 0 || creatorStat.PrivateWorkCount < 0 || creatorStat.ReceivedLikeCount < 0 || creatorStat.CollectionCount < 0 {
		t.Fatalf("aggregate became negative: %+v", creatorStat)
	}

	exposureRepo := infraexposure.New(db)
	if _, err := exposureRepo.SaveViewEvent(context.Background(), mustViewEvent(t, 2, first.ID, domainexposure.EventTypeExposed, 0)); err != nil {
		t.Fatalf("save exposed event: %v", err)
	}
	history, err := exposureRepo.ListHistory(context.Background(), 2, nil, 20)
	if err != nil || len(history) != 0 {
		t.Fatalf("exposed event entered history: history=%+v err=%v", history, err)
	}
	if _, err := exposureRepo.SaveViewEvent(context.Background(), mustViewEvent(t, 2, first.ID, domainexposure.EventTypePlay, 900)); err != nil {
		t.Fatalf("save play event: %v", err)
	}
	if _, err := exposureRepo.SaveViewEvent(context.Background(), mustViewEvent(t, 2, first.ID, domainexposure.EventTypeComplete, 1500)); err != nil {
		t.Fatalf("save complete event: %v", err)
	}
	history, err = exposureRepo.ListHistory(context.Background(), 2, nil, 20)
	if err != nil || len(history) != 1 || !history[0].Completed || history[0].LastWatchMs != 1500 {
		t.Fatalf("unexpected history upsert: history=%+v err=%v", history, err)
	}
	if err := exposureRepo.ClearHistory(context.Background(), 2); err != nil {
		t.Fatalf("clear history: %v", err)
	}
	var rawEvents int64
	if err := db.Model(&infraexposure.ViewEventModel{}).Where("user_id = ?", 2).Count(&rawEvents).Error; err != nil {
		t.Fatalf("count raw events: %v", err)
	}
	if rawEvents != 3 {
		t.Fatalf("history clear deleted raw events: %d", rawEvents)
	}

	libraryRepo := infralibrary.New(db)
	fact, _ := domainlibrary.NewWatchLater(2, first.ID, true)
	firstFact, err := libraryRepo.SetWatchLater(context.Background(), fact)
	if err != nil {
		t.Fatalf("set watch later: %v", err)
	}
	replayedFact, err := libraryRepo.SetWatchLater(context.Background(), fact)
	if err != nil || !firstFact.UpdatedAt.Equal(replayedFact.UpdatedAt) {
		t.Fatalf("watch later replay changed state: first=%+v replay=%+v err=%v", firstFact, replayedFact, err)
	}
}

func mustCollection(t *testing.T, ownerID int64, title, visibility, key string) *domainvideo.Collection {
	t.Helper()
	collection, err := domainvideo.NewCollection(ownerID, title, "", visibility, key)
	if err != nil {
		t.Fatalf("new collection: %v", err)
	}
	return collection
}

func TestPostgreSQLPublicCollectionListUsesBoundedBatchHydration(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate collection list fixture: %v", err)
	}

	const ownerID int64 = 9001
	now := time.Now().UTC().Truncate(time.Microsecond)
	collections := make([]infravideo.CollectionModel, 0, 100)
	videos := make([]infravideo.VideoModel, 0, 600)
	memberships := make([]infravideo.CollectionItemModel, 0, 600)
	for collectionID := int64(1); collectionID <= 100; collectionID++ {
		collections = append(collections, infravideo.CollectionModel{
			ID: collectionID, OwnerID: ownerID, Title: fmt.Sprintf("collection-%03d", collectionID),
			Visibility: domainvideo.VisibilityPublic, Status: domainvideo.CollectionStatusActive,
			CreatedAt: now, UpdatedAt: now,
		})
		baseVideoID := collectionID * 10
		videoDefinitions := []struct {
			offset     int64
			position   int
			status     int
			visibility string
		}{
			{offset: 1, position: 1, status: domainvideo.StatusPublished, visibility: domainvideo.VisibilityPrivate},
			{offset: 3, position: 2, status: domainvideo.StatusPublished, visibility: domainvideo.VisibilityPublic},
			{offset: 2, position: 2, status: domainvideo.StatusPublished, visibility: domainvideo.VisibilityPublic},
			{offset: 4, position: 3, status: domainvideo.StatusOffline, visibility: domainvideo.VisibilityPublic},
			{offset: 5, position: 4, status: domainvideo.StatusPublished, visibility: domainvideo.VisibilityPublic},
			{offset: 6, position: 5, status: domainvideo.StatusPublished, visibility: domainvideo.VisibilityPublic},
		}
		for _, definition := range videoDefinitions {
			videoID := baseVideoID + definition.offset
			publishedAt := now
			videos = append(videos, infravideo.VideoModel{
				ID: videoID, AuthorID: ownerID, Title: fmt.Sprintf("video-%d", videoID),
				MediaURL: fmt.Sprintf("https://media.example/%d.mp4", videoID),
				CoverURL: fmt.Sprintf("https://media.example/%d.jpg", videoID),
				Status:   definition.status, Visibility: definition.visibility,
				PublishedAt: &publishedAt, CreatedAt: now, UpdatedAt: now,
			})
			memberships = append(memberships, infravideo.CollectionItemModel{
				CollectionID: collectionID, VideoID: videoID, Position: definition.position, CreatedAt: now,
			})
		}
	}
	if err := db.CreateInBatches(&collections, 100).Error; err != nil {
		t.Fatalf("seed public collections: %v", err)
	}
	if err := db.CreateInBatches(&videos, 100).Error; err != nil {
		t.Fatalf("seed collection videos: %v", err)
	}
	if err := db.CreateInBatches(&memberships, 100).Error; err != nil {
		t.Fatalf("seed collection memberships: %v", err)
	}

	counter := &queryCounterLogger{Interface: logger.Default.LogMode(logger.Silent)}
	repo := infravideo.New(db.Session(&gorm.Session{Logger: counter}))
	listed, err := repo.ListCollections(context.Background(), ownerID, true, nil, 100)
	if err != nil {
		t.Fatalf("list public collections: %v", err)
	}
	if queryCount := counter.count.Load(); queryCount != 3 {
		t.Fatalf("public collection list used %d queries, want 3 batched queries", queryCount)
	}
	if len(listed) != 100 {
		t.Fatalf("listed %d collections, want 100", len(listed))
	}
	for index, collection := range listed {
		expectedCollectionID := int64(100 - index)
		if collection.ID != expectedCollectionID {
			t.Fatalf("collection order at %d: got %d want %d", index, collection.ID, expectedCollectionID)
		}
		if collection.MemberCount != 4 {
			t.Fatalf("collection %d readable member count = %d, want 4", collection.ID, collection.MemberCount)
		}
		if len(collection.Items) != domainvideo.MaxPublicCollectionPreviewItems {
			t.Fatalf("collection %d preview length = %d, want %d", collection.ID, len(collection.Items), domainvideo.MaxPublicCollectionPreviewItems)
		}
		baseVideoID := collection.ID * 10
		expectedVideoIDs := []int64{baseVideoID + 2, baseVideoID + 3, baseVideoID + 5}
		for itemIndex, item := range collection.Items {
			if item.VideoID != expectedVideoIDs[itemIndex] || item.Video == nil || item.Video.ID != expectedVideoIDs[itemIndex] {
				t.Fatalf("collection %d item %d = %+v, want video %d", collection.ID, itemIndex, item, expectedVideoIDs[itemIndex])
			}
			if !item.Video.IsPubliclyReadable() {
				t.Fatalf("collection %d returned unreadable video %+v", collection.ID, item.Video)
			}
		}
	}

	ownerItems, err := infravideo.New(db).ListCollectionItems(context.Background(), 100, false)
	if err != nil {
		t.Fatalf("list owner collection members: %v", err)
	}
	if len(ownerItems) != 6 {
		t.Fatalf("owner collection members = %d, want all 6 for editing", len(ownerItems))
	}
	expectedOwnerVideoIDs := []int64{1001, 1002, 1003, 1004, 1005, 1006}
	for index, item := range ownerItems {
		if item.VideoID != expectedOwnerVideoIDs[index] {
			t.Fatalf("owner item %d = %d, want %d", index, item.VideoID, expectedOwnerVideoIDs[index])
		}
	}
}

func mustViewEvent(t *testing.T, userID, videoID int64, eventType string, watchMs int) *domainexposure.ViewEvent {
	t.Helper()
	event, err := domainexposure.NewViewEvent(domainexposure.NewViewEventInput{
		UserID: userID, VideoID: videoID, Scene: "timeline", EventType: eventType,
		WatchMs: watchMs,
	})
	if err != nil {
		t.Fatalf("new view event: %v", err)
	}
	event.OccurredAt = time.Now().UTC()
	event.PositionMs = watchMs
	return event
}

func TestPostgreSQLRepositorySemantics(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate repository test schema: %v", err)
	}

	ctx := context.Background()
	accountRepo := infraaccount.New(db)
	alice, err := domainaccount.New(" Alice ", "CaseSensitivePassword", "Alice")
	if err != nil {
		t.Fatalf("new Alice: %v", err)
	}
	if err := accountRepo.Save(ctx, alice); err != nil {
		t.Fatalf("save Alice: %v", err)
	}
	loadedAlice, err := accountRepo.FindByAccount(ctx, "ALICE")
	if err != nil {
		t.Fatalf("find Alice with case variant: %v", err)
	}
	if loadedAlice.Account != "alice" {
		t.Fatalf("expected canonical account, got %q", loadedAlice.Account)
	}

	duplicate, err := domainaccount.New("ALICE", "AnotherPassword", "Duplicate")
	if err != nil {
		t.Fatalf("new duplicate Alice: %v", err)
	}
	if err := accountRepo.Save(ctx, duplicate); !errors.Is(err, domainaccount.ErrAccountAlreadyExists) {
		t.Fatalf("expected account conflict, got %v", err)
	}

	bob, err := domainaccount.New("Bob", "CaseSensitivePassword", "Bob")
	if err != nil {
		t.Fatalf("new Bob: %v", err)
	}
	if err := accountRepo.Save(ctx, bob); err != nil {
		t.Fatalf("save Bob: %v", err)
	}

	relationRepo := infrarelation.New(db)
	if _, userStat, targetStat, err := relationRepo.SetFollow(ctx, alice.ID, bob.ID, true, "Follow-Key"); err != nil {
		t.Fatalf("create follow: %v", err)
	} else if userStat.FollowingCount != 1 || targetStat.FollowerCount != 1 {
		t.Fatalf("unexpected relation stats: user=%+v target=%+v", userStat, targetStat)
	}
	if _, userStat, targetStat, err := relationRepo.SetFollow(ctx, alice.ID, bob.ID, true, "Follow-Key"); err != nil {
		t.Fatalf("repeat follow: %v", err)
	} else if userStat.FollowingCount != 1 || targetStat.FollowerCount != 1 {
		t.Fatalf("repeated follow changed stats: user=%+v target=%+v", userStat, targetStat)
	}

	videoRepo := infravideo.New(db)
	video, err := domainvideo.NewPublished(alice.ID, "PostgreSQL", "", "/uploads/postgres.mp4", "/uploads/postgres.jpg", "")
	if err != nil {
		t.Fatalf("new video: %v", err)
	}
	if err := videoRepo.Save(ctx, video); err != nil {
		t.Fatalf("save video: %v", err)
	}
	secondVideo, err := domainvideo.NewPublished(alice.ID, "PostgreSQL second", "", "/uploads/postgres-2.mp4", "/uploads/postgres-2.jpg", "")
	if err != nil {
		t.Fatalf("new second video: %v", err)
	}
	if err := videoRepo.Save(ctx, secondVideo); err != nil {
		t.Fatalf("save second video without idempotency key: %v", err)
	}
	if video.ID == 0 || secondVideo.ID == 0 || video.ID == secondVideo.ID {
		t.Fatalf("expected distinct generated IDs, got %d and %d", video.ID, secondVideo.ID)
	}

	messageRepo := inframessage.New(db)
	firstMessage, err := domainmessage.New(alice.ID, domainmessage.TypeSystem, "Title", "Content", "Event-Key")
	if err != nil {
		t.Fatalf("new first message: %v", err)
	}
	savedFirst, created, err := messageRepo.Create(ctx, firstMessage, "Request-Key")
	if err != nil || !created {
		t.Fatalf("create first message: created=%v err=%v", created, err)
	}
	repeatedMessage, _ := domainmessage.New(alice.ID, domainmessage.TypeSystem, "Title", "Content", "Event-Key")
	repeatedMessage.WithTargets(video.ID, 3002, 3001)
	savedRepeated, created, err := messageRepo.Create(ctx, repeatedMessage, "Request-Key")
	if err != nil || created || savedRepeated.ID != savedFirst.ID {
		t.Fatalf("repeat exact message: created=%v saved=%+v err=%v", created, savedRepeated, err)
	}
	if savedRepeated.VideoID != video.ID || savedRepeated.CommentID != 3002 || savedRepeated.RootCommentID != 3001 {
		t.Fatalf("idempotent replay did not persist structured targets: %+v", savedRepeated)
	}
	caseVariantMessage, _ := domainmessage.New(alice.ID, domainmessage.TypeSystem, "Title", "Content", "event-key")
	savedVariant, created, err := messageRepo.Create(ctx, caseVariantMessage, "request-key")
	if err != nil || !created || savedVariant.ID == savedFirst.ID {
		t.Fatalf("create case-variant opaque keys: created=%v saved=%+v err=%v", created, savedVariant, err)
	}

	exposureRepo := infraexposure.New(db)
	firstEvent, err := domainexposure.NewViewEvent(domainexposure.NewViewEventInput{
		UserID: alice.ID, VideoID: video.ID, Scene: "timeline", RequestID: "Exposure-Key",
		EventType: domainexposure.EventTypeExposed,
	})
	if err != nil {
		t.Fatalf("new first exposure: %v", err)
	}
	firstEvent.OccurredAt = time.Now().UTC()
	firstResult, err := exposureRepo.SaveViewEvent(ctx, firstEvent)
	if err != nil {
		t.Fatalf("save first exposure: %v", err)
	}
	secondEvent, err := domainexposure.NewViewEvent(domainexposure.NewViewEventInput{
		UserID: alice.ID, VideoID: video.ID, Scene: "hot", RequestID: "exposure-key",
		EventType: domainexposure.EventTypeExposed,
	})
	if err != nil {
		t.Fatalf("new second exposure: %v", err)
	}
	secondEvent.OccurredAt = time.Now().UTC()
	secondResult, err := exposureRepo.SaveViewEvent(ctx, secondEvent)
	if err != nil {
		t.Fatalf("save second exposure: %v", err)
	}
	if firstResult.Exposure.ExposureCount != 1 || secondResult.Exposure.ExposureCount != 2 || secondResult.Exposure.LastScene != "hot" {
		t.Fatalf("unexpected exposure aggregation: first=%+v aggregated=%+v", firstResult.Exposure, secondResult.Exposure)
	}
	if firstResult.Event.ID == 0 || secondResult.Event.ID == 0 || firstResult.Event.ID == secondResult.Event.ID {
		t.Fatalf("expected distinct exposure event IDs: %d and %d", firstResult.Event.ID, secondResult.Event.ID)
	}
	if delta := secondResult.Exposure.LastExposedAt.Sub(secondResult.Event.CreatedAt); delta < -time.Millisecond || delta > time.Millisecond {
		t.Fatalf("last exposure time does not match incoming event: delta=%v", delta)
	}

	embeddingRepo := infraembedding.New(db)
	embedding := domainembedding.NewVideoEmbedding(video.ID, "hash-ngram", []float64{0.1}, "hash-1", `[0.1]`)
	if err := embeddingRepo.SaveVideoEmbedding(ctx, embedding); err != nil {
		t.Fatalf("save embedding: %v", err)
	}
	updatedEmbedding := domainembedding.NewVideoEmbedding(video.ID, "hash-ngram", []float64{0.2, 0.3}, "hash-2", `[0.2,0.3]`)
	if err := embeddingRepo.SaveVideoEmbedding(ctx, updatedEmbedding); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}
	loadedEmbedding, err := embeddingRepo.FindVideoEmbedding(ctx, video.ID, "hash-ngram")
	if err != nil {
		t.Fatalf("find embedding: %v", err)
	}
	if loadedEmbedding.Dimension != 2 || loadedEmbedding.TextHash != "hash-2" {
		t.Fatalf("unexpected upserted embedding metadata: %+v", loadedEmbedding)
	}
	var vector []float64
	if err := json.Unmarshal([]byte(loadedEmbedding.EmbeddingJSON), &vector); err != nil {
		t.Fatalf("decode upserted embedding: %v", err)
	}
	if len(vector) != 2 || vector[0] != 0.2 || vector[1] != 0.3 {
		t.Fatalf("unexpected upserted embedding: %+v", loadedEmbedding)
	}
	if _, offset := loadedEmbedding.UpdatedAt.Zone(); offset != 0 {
		t.Fatalf("expected UTC embedding timestamp, got %v", loadedEmbedding.UpdatedAt)
	}

	assertConcurrentCounters(t, db, video.ID)
	assertStableTimelineCursor(t, db, alice.ID)
}

func TestPostgreSQLCommentListingRequiresPublicPublishedVideo(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate comment visibility test schema: %v", err)
	}

	ctx := context.Background()
	accountRepo := infraaccount.New(db)
	author, err := domainaccount.New("comment-author", "CaseSensitivePassword", "Author")
	if err != nil {
		t.Fatalf("new comment author: %v", err)
	}
	if err := accountRepo.Save(ctx, author); err != nil {
		t.Fatalf("save comment author: %v", err)
	}
	commenter, err := domainaccount.New("commenter", "CaseSensitivePassword", "Commenter")
	if err != nil {
		t.Fatalf("new commenter: %v", err)
	}
	if err := accountRepo.Save(ctx, commenter); err != nil {
		t.Fatalf("save commenter: %v", err)
	}

	videoRepo := infravideo.New(db)
	video, err := domainvideo.NewPublished(
		author.ID,
		"Comment visibility",
		"",
		"/uploads/comment-visibility.mp4",
		"/uploads/comment-visibility.jpg",
		"",
	)
	if err != nil {
		t.Fatalf("new comment video: %v", err)
	}
	if err := videoRepo.Save(ctx, video); err != nil {
		t.Fatalf("save comment video: %v", err)
	}

	comment, err := domaininteraction.NewComment(video.ID, commenter.ID, "visible comment", "comment-visibility")
	if err != nil {
		t.Fatalf("new comment: %v", err)
	}
	interactionRepo := infrainteraction.New(db)
	if _, _, _, err := interactionRepo.CreateComment(ctx, comment); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	assertList := func(wantVisible bool) {
		t.Helper()
		comments, err := interactionRepo.ListComments(ctx, video.ID, nil, 10)
		if wantVisible {
			if err != nil || len(comments) != 1 || comments[0].Content != "visible comment" {
				t.Fatalf("expected visible comment, comments=%+v err=%v", comments, err)
			}
			return
		}
		if !errors.Is(err, domaininteraction.ErrVideoNotFound) || len(comments) != 0 {
			t.Fatalf("expected hidden video to return not found without comments, comments=%+v err=%v", comments, err)
		}
	}

	assertList(true)
	if err := db.Model(&infravideo.VideoModel{}).
		Where("id = ?", video.ID).
		Update("visibility", domainvideo.VisibilityPrivate).Error; err != nil {
		t.Fatalf("make video private: %v", err)
	}
	assertList(false)
	if err := db.Model(&infravideo.VideoModel{}).
		Where("id = ?", video.ID).
		Update("visibility", domainvideo.VisibilityPublic).Error; err != nil {
		t.Fatalf("make video public: %v", err)
	}
	assertList(true)
	if err := db.Model(&infravideo.VideoModel{}).
		Where("id = ?", video.ID).
		Update("status", domainvideo.StatusOffline).Error; err != nil {
		t.Fatalf("make video offline: %v", err)
	}
	assertList(false)
	if err := db.Model(&infravideo.VideoModel{}).
		Where("id = ?", video.ID).
		Update("status", domainvideo.StatusPublished).Error; err != nil {
		t.Fatalf("republish video: %v", err)
	}
	assertList(true)
	if err := db.Model(&infravideo.VideoModel{}).
		Where("id = ?", video.ID).
		Update("status", domainvideo.StatusDeleted).Error; err != nil {
		t.Fatalf("delete video: %v", err)
	}
	assertList(false)
}

func TestPostgreSQLThreadedCommentPersistence(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate threaded comment schema: %v", err)
	}

	ctx := context.Background()
	accountRepo := infraaccount.New(db)
	users := make([]*domainaccount.User, 0, 3)
	for _, definition := range []struct {
		account  string
		nickname string
	}{
		{account: "thread-author", nickname: "Author"},
		{account: "thread-root-user", nickname: "Root User"},
		{account: "thread-reply-user", nickname: "Reply User"},
	} {
		user, err := domainaccount.New(definition.account, "CaseSensitivePassword", definition.nickname)
		if err != nil {
			t.Fatalf("new threaded comment account: %v", err)
		}
		if err := accountRepo.Save(ctx, user); err != nil {
			t.Fatalf("save threaded comment account: %v", err)
		}
		users = append(users, user)
	}

	videoRepo := infravideo.New(db)
	video, err := domainvideo.NewPublished(
		users[0].ID,
		"Threaded comments",
		"",
		"/uploads/threaded-comments.mp4",
		"/uploads/threaded-comments.jpg",
		"",
	)
	if err != nil {
		t.Fatalf("new threaded comment video: %v", err)
	}
	if err := videoRepo.Save(ctx, video); err != nil {
		t.Fatalf("save threaded comment video: %v", err)
	}

	repo := infrainteraction.New(db)
	rootInput, err := domaininteraction.NewRootComment(video.ID, users[1].ID, "root", "thread-root-key")
	if err != nil {
		t.Fatalf("new root comment: %v", err)
	}
	rootMutation, err := repo.CreateThreadedComment(ctx, rootInput)
	if err != nil {
		t.Fatalf("create root comment: %v", err)
	}
	replayed, err := repo.CreateThreadedComment(ctx, rootInput)
	if err != nil || replayed.Comment.ID != rootMutation.Comment.ID || replayed.VideoDelta != 0 {
		t.Fatalf("root replay changed facts: replay=%+v err=%v", replayed, err)
	}
	conflictingRoot, err := domaininteraction.NewRootComment(video.ID, users[1].ID, "other payload", "thread-root-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateThreadedComment(ctx, conflictingRoot); !errors.Is(err, domaininteraction.ErrCommentIdempotencyConflict) {
		t.Fatalf("expected payload-aware root conflict, got %v", err)
	}
	conflictingTarget, err := domaininteraction.NewReplyComment(
		video.ID, users[1].ID, 999999, "other payload", "thread-root-key",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateThreadedComment(ctx, conflictingTarget); !errors.Is(err, domaininteraction.ErrCommentIdempotencyConflict) {
		t.Fatalf("changed missing target should preserve idempotency conflict, got %v", err)
	}

	firstReplyInput, err := domaininteraction.NewReplyComment(
		video.ID, users[2].ID, rootMutation.Comment.ID, "first reply", "thread-reply-key-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	firstReply, err := repo.CreateThreadedComment(ctx, firstReplyInput)
	if err != nil {
		t.Fatalf("create first reply: %v", err)
	}
	secondReplyInput, err := domaininteraction.NewReplyComment(
		video.ID, users[0].ID, firstReply.Comment.ID, "reply to reply", "thread-reply-key-2",
	)
	if err != nil {
		t.Fatal(err)
	}
	secondReply, err := repo.CreateThreadedComment(ctx, secondReplyInput)
	if err != nil {
		t.Fatalf("create reply to reply: %v", err)
	}
	if secondReply.Comment.RootCommentID != rootMutation.Comment.ID ||
		secondReply.Comment.ReplyToCommentID != firstReply.Comment.ID {
		t.Fatalf("reply-to-reply nesting was not flattened: %+v", secondReply.Comment)
	}

	like, err := repo.SetCommentLike(ctx, rootMutation.Comment.ID, users[0].ID, true, "thread-like-key")
	if err != nil {
		t.Fatalf("like root comment: %v", err)
	}
	if !like.Liked || like.LikeCount != 1 {
		t.Fatalf("unexpected root like result: %+v", like)
	}
	if _, err := repo.SetCommentLike(ctx, firstReply.Comment.ID, users[0].ID, false, "thread-like-key"); !errors.Is(err, domaininteraction.ErrCommentLikeIdempotencyConflict) {
		t.Fatalf("expected comment-like receipt conflict, got %v", err)
	}

	page, err := repo.ListCommentRoots(ctx, domaininteraction.CommentRootQuery{
		VideoID:      video.ID,
		Viewer:       domaininteraction.CommentViewer{UserID: users[0].ID, Role: domainaccount.RoleUser},
		Sort:         domaininteraction.CommentSortHot,
		Limit:        10,
		PreviewLimit: 3,
	})
	if err != nil {
		t.Fatalf("list hot roots: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ReplyCount != 2 || page.Items[0].HotScore != 13 ||
		len(page.Items[0].ReplyPreviews) != 2 || !page.Items[0].Liked || !page.Items[0].CanDelete {
		t.Fatalf("unexpected hydrated root page: %+v", page)
	}

	thread, err := repo.GetCommentThreadContext(
		ctx, secondReply.Comment.ID,
		domaininteraction.CommentViewer{UserID: users[1].ID, Role: domainaccount.RoleUser},
		10,
	)
	if err != nil {
		t.Fatalf("load direct thread context: %v", err)
	}
	if thread.Root.ID != rootMutation.Comment.ID || thread.Target.ID != secondReply.Comment.ID || len(thread.Replies) != 2 {
		t.Fatalf("unexpected direct thread context: %+v", thread)
	}

	if err := db.Model(&infrainteraction.CommentModel{}).Where("id = ?", rootMutation.Comment.ID).Updates(map[string]any{
		"reply_count": 99, "like_count": 99, "hot_score": 999,
	}).Error; err != nil {
		t.Fatalf("corrupt root counters: %v", err)
	}
	if err := db.Model(&infravideo.VideoStatModel{}).Where("video_id = ?", video.ID).Update("comment_count", 99).Error; err != nil {
		t.Fatalf("corrupt video comment count: %v", err)
	}
	if err := repo.ReconcileCommentCounters(ctx); err != nil {
		t.Fatalf("reconcile threaded counters: %v", err)
	}
	var reconciledRoot infrainteraction.CommentModel
	if err := db.Where("id = ?", rootMutation.Comment.ID).Take(&reconciledRoot).Error; err != nil {
		t.Fatalf("load reconciled root: %v", err)
	}
	var reconciledStat infravideo.VideoStatModel
	if err := db.Where("video_id = ?", video.ID).Take(&reconciledStat).Error; err != nil {
		t.Fatalf("load reconciled video stat: %v", err)
	}
	if reconciledRoot.ReplyCount != 2 || reconciledRoot.LikeCount != 1 || reconciledRoot.HotScore != 13 ||
		reconciledStat.CommentCount != 3 {
		t.Fatalf("unexpected reconciled counters: root=%+v stat=%+v", reconciledRoot, reconciledStat)
	}

	rootDeleted, err := repo.DeleteThreadedComment(
		ctx, rootMutation.Comment.ID, users[1].ID, domainaccount.RoleUser,
	)
	if err != nil {
		t.Fatalf("self-delete root with replies: %v", err)
	}
	if !rootDeleted.Tombstone || rootDeleted.CommentCount != 2 {
		t.Fatalf("unexpected root tombstone result: %+v", rootDeleted)
	}
	tombstoneThread, err := repo.GetCommentThreadContext(
		ctx, firstReply.Comment.ID,
		domaininteraction.CommentViewer{UserID: users[2].ID, Role: domainaccount.RoleUser},
		10,
	)
	if err != nil {
		t.Fatalf("load tombstone thread: %v", err)
	}
	if tombstoneThread.Root.UserID != 0 || tombstoneThread.Root.Content != "" ||
		tombstoneThread.Target.ReplyToUserID != 0 ||
		tombstoneThread.Target.ReplyToUserNickname != "" ||
		tombstoneThread.Target.ReplyToUserAvatarURL != "" {
		t.Fatalf("tombstone identity leaked through thread metadata: %+v", tombstoneThread)
	}

	if err := db.Model(&infravideo.VideoModel{}).Where("id = ?", video.ID).
		Update("visibility", domainvideo.VisibilityPrivate).Error; err != nil {
		t.Fatalf("make threaded comment video private: %v", err)
	}
	if _, err := repo.ListCommentRoots(ctx, domaininteraction.CommentRootQuery{
		VideoID: video.ID, Sort: domaininteraction.CommentSortLatest, Limit: 10,
	}); !errors.Is(err, domaininteraction.ErrVideoNotFound) {
		t.Fatalf("private threaded comments remained readable: %v", err)
	}
	if _, err := repo.CreateThreadedComment(ctx, rootInput); !errors.Is(err, domaininteraction.ErrVideoNotFound) {
		t.Fatalf("comment replay exposed a private video: %v", err)
	}
	deleted, err := repo.DeleteThreadedComment(
		ctx, firstReply.Comment.ID, users[2].ID, domainaccount.RoleUser,
	)
	if err != nil {
		t.Fatalf("delete reply after privacy change: %v", err)
	}
	if deleted.Comment.Status != domaininteraction.CommentStatusSelfDeleted ||
		deleted.RootReplyCount != 1 || deleted.CommentCount != 1 {
		t.Fatalf("unexpected private-video deletion result: %+v", deleted)
	}

	notificationVideo, err := domainvideo.NewPublished(
		users[0].ID,
		"Notification outbox",
		"",
		"/uploads/comment-notification.mp4",
		"/uploads/comment-notification.jpg",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := videoRepo.Save(ctx, notificationVideo); err != nil {
		t.Fatalf("save notification video: %v", err)
	}
	if _, err := repo.CreateThreadedComment(ctx, mustRootComment(t, notificationVideo.ID, users[0].ID, "self root", "notification-self-root")); err != nil {
		t.Fatalf("create self root: %v", err)
	}
	notificationRoot, err := repo.CreateThreadedComment(ctx, mustRootComment(t, notificationVideo.ID, users[1].ID, "root notification", "notification-root"))
	if err != nil {
		t.Fatalf("create notification root: %v", err)
	}
	if replay, err := repo.CreateThreadedComment(ctx, mustRootComment(t, notificationVideo.ID, users[1].ID, "root notification", "notification-root")); err != nil || replay.VideoDelta != 0 {
		t.Fatalf("replay notification root: replay=%+v err=%v", replay, err)
	}
	if _, err := repo.CreateThreadedComment(ctx, mustReplyComment(t, notificationVideo.ID, users[1].ID, notificationRoot.Comment.ID, "self reply", "notification-self-reply")); err != nil {
		t.Fatalf("create self reply: %v", err)
	}
	notificationReply, err := repo.CreateThreadedComment(ctx, mustReplyComment(t, notificationVideo.ID, users[2].ID, notificationRoot.Comment.ID, "reply notification", "notification-reply"))
	if err != nil {
		t.Fatalf("create notification reply: %v", err)
	}
	if _, err := repo.SetCommentLike(ctx, notificationReply.Comment.ID, users[2].ID, true, "notification-self-like"); err != nil {
		t.Fatalf("self like reply: %v", err)
	}
	if _, err := repo.SetCommentLike(ctx, notificationReply.Comment.ID, users[0].ID, true, "notification-like"); err != nil {
		t.Fatalf("like reply: %v", err)
	}
	if _, err := repo.SetCommentLike(ctx, notificationReply.Comment.ID, users[0].ID, false, "notification-unlike"); err != nil {
		t.Fatalf("unlike reply: %v", err)
	}
	if _, err := repo.SetCommentLike(ctx, notificationReply.Comment.ID, users[0].ID, true, "notification-relike"); err != nil {
		t.Fatalf("relike reply: %v", err)
	}

	var notifications []infrainteraction.CommentNotificationOutboxModel
	if err := db.Where("video_id = ?", notificationVideo.ID).Order("created_at ASC").Find(&notifications).Error; err != nil {
		t.Fatalf("load comment notifications: %v", err)
	}
	if len(notifications) != 3 {
		t.Fatalf("expected root, reply, and like notifications only, got %+v", notifications)
	}
	byType := make(map[string]infrainteraction.CommentNotificationOutboxModel, len(notifications))
	for _, notification := range notifications {
		byType[notification.MessageType] = notification
	}
	rootNotification := byType[domaininteraction.CommentNotificationTypeRoot]
	if rootNotification.RecipientID != users[0].ID || rootNotification.ActorID != users[1].ID ||
		rootNotification.CommentID != notificationRoot.Comment.ID || rootNotification.RootCommentID != notificationRoot.Comment.ID {
		t.Fatalf("unexpected root notification: %+v", rootNotification)
	}
	replyNotification := byType[domaininteraction.CommentNotificationTypeReply]
	if replyNotification.RecipientID != users[1].ID || replyNotification.ActorID != users[2].ID ||
		replyNotification.CommentID != notificationReply.Comment.ID || replyNotification.RootCommentID != notificationRoot.Comment.ID {
		t.Fatalf("unexpected reply notification: %+v", replyNotification)
	}
	likeNotification := byType[domaininteraction.CommentNotificationTypeLike]
	if likeNotification.RecipientID != users[2].ID || likeNotification.ActorID != users[0].ID ||
		likeNotification.CommentID != notificationReply.Comment.ID || likeNotification.RootCommentID != notificationRoot.Comment.ID {
		t.Fatalf("unexpected like notification: %+v", likeNotification)
	}
}

func mustRootComment(t *testing.T, videoID int64, userID int64, content string, key string) *domaininteraction.Comment {
	t.Helper()
	comment, err := domaininteraction.NewRootComment(videoID, userID, content, key)
	if err != nil {
		t.Fatal(err)
	}
	return comment
}

func mustReplyComment(t *testing.T, videoID int64, userID int64, targetID int64, content string, key string) *domaininteraction.Comment {
	t.Helper()
	comment, err := domaininteraction.NewReplyComment(videoID, userID, targetID, content, key)
	if err != nil {
		t.Fatal(err)
	}
	return comment
}

func TestPostgreSQLThreadedCommentLegacyBackfill(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := db.AutoMigrate(
		&infraaccount.UserModel{},
		&infravideo.VideoModel{},
		&infravideo.VideoStatModel{},
	); err != nil {
		t.Fatalf("migrate legacy comment dependencies: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE interaction_comment (
			id bigserial PRIMARY KEY,
			video_id bigint NOT NULL,
			user_id bigint NOT NULL,
			content varchar(1000) NOT NULL,
			status smallint NOT NULL DEFAULT 1,
			idempotency_key varchar(128),
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL
		)
	`).Error; err != nil {
		t.Fatalf("create legacy interaction_comment: %v", err)
	}

	user := infraaccount.UserModel{
		ID: 1, Account: "legacy-thread-user", Password: "hash", Nickname: "legacy",
		Status: domainaccount.StatusNormal, Role: domainaccount.RoleUser,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create legacy comment user: %v", err)
	}
	publishedAt := time.Now().UTC()
	video := infravideo.VideoModel{
		ID: 1, AuthorID: user.ID, Title: "legacy thread",
		MediaURL: "/uploads/legacy-thread.mp4", CoverURL: "/uploads/legacy-thread.jpg",
		Status: domainvideo.StatusPublished, Visibility: domainvideo.VisibilityPublic,
		MediaStatus: domainmedia.MediaStatusLegacyReady, PublishedAt: &publishedAt,
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatalf("create legacy comment video: %v", err)
	}
	if err := db.Create(&infravideo.VideoStatModel{VideoID: video.ID, CommentCount: 1}).Error; err != nil {
		t.Fatalf("create legacy comment stat: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO interaction_comment
			(video_id, user_id, content, status, idempotency_key, created_at, updated_at)
		VALUES
			(?, ?, 'legacy visible', 1, 'legacy-visible-key', NOW(), NOW()),
			(?, ?, 'legacy deleted', 2, 'legacy-deleted-key', NOW(), NOW())
	`, video.ID, user.ID, video.ID, user.ID).Error; err != nil {
		t.Fatalf("insert legacy comments: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("upgrade legacy comments: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("repeat legacy comment migration: %v", err)
	}

	var comments []infrainteraction.CommentModel
	if err := db.Order("id ASC").Find(&comments).Error; err != nil {
		t.Fatalf("load migrated legacy comments: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("unexpected migrated legacy comments: %+v", comments)
	}
	for _, comment := range comments {
		if comment.RootCommentID != nil || comment.ReplyToCommentID != nil ||
			comment.ReplyCount != 0 || comment.LikeCount != 0 || comment.HotScore != 0 ||
			comment.RequestFingerprint != "" {
			t.Fatalf("legacy comment was not safely initialized as a root: %+v", comment)
		}
	}
	if comments[0].Status != domaininteraction.CommentStatusNormal ||
		comments[1].Status != domaininteraction.CommentStatusSelfDeleted {
		t.Fatalf("legacy statuses were not preserved safely: %+v", comments)
	}
	var stat infravideo.VideoStatModel
	if err := db.Where("video_id = ?", video.ID).Take(&stat).Error; err != nil {
		t.Fatalf("load migrated legacy stat: %v", err)
	}
	if stat.CommentCount != 1 {
		t.Fatalf("legacy visible comment total changed: %+v", stat)
	}
	var markerCount int64
	if err := db.Model(&markerModel{}).Where("key = ?", threadedCommentBackfillKey).Count(&markerCount).Error; err != nil {
		t.Fatalf("load legacy threaded marker: %v", err)
	}
	if markerCount != 1 {
		t.Fatalf("threaded migration marker was not idempotent: %d", markerCount)
	}
	for _, indexName := range []string{
		"idx_interaction_comment_root_latest",
		"idx_interaction_comment_root_hot",
		"idx_interaction_comment_replies",
		"idx_interaction_comment_direct_target",
	} {
		if !db.Migrator().HasIndex(&infrainteraction.CommentModel{}, indexName) {
			t.Fatalf("repeated legacy migration is missing index %s", indexName)
		}
	}
	for _, index := range []struct {
		model any
		name  string
	}{
		{&infrainteraction.CommentLikeModel{}, "uk_interaction_comment_like_user_comment"},
		{&infrainteraction.CommentLikeModel{}, "idx_interaction_comment_like_comment_status"},
		{&infrainteraction.CommentNotificationOutboxModel{}, "idx_interaction_comment_notification_outbox_pending"},
	} {
		if !db.Migrator().HasIndex(index.model, index.name) {
			t.Fatalf("repeated legacy migration is missing index %s", index.name)
		}
	}
}

func TestPostgreSQLThreadedCommentOrderingHydrationAndCascadeModeration(t *testing.T) {
	_, db, repo, users, video := newPostgresThreadedCommentFixture(t, "ordering")
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	roots := []infrainteraction.CommentModel{
		{
			VideoID: video.ID, UserID: users[1].ID, Content: "old hottest",
			Status: domaininteraction.CommentStatusNormal, ReplyCount: 4, HotScore: 50,
			CreatedAt: base, UpdatedAt: base,
		},
		{
			VideoID: video.ID, UserID: users[2].ID, Content: "middle cold",
			Status: domaininteraction.CommentStatusNormal, ReplyCount: 4, HotScore: 10,
			CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute),
		},
		{
			VideoID: video.ID, UserID: users[3].ID, Content: "new warm",
			Status: domaininteraction.CommentStatusNormal, ReplyCount: 4, HotScore: 30,
			CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base.Add(2 * time.Minute),
		},
	}
	if err := db.Create(&roots).Error; err != nil {
		t.Fatalf("seed ordered roots: %v", err)
	}
	replies := make([]infrainteraction.CommentModel, 0, len(roots)*4)
	for rootIndex := range roots {
		for replyIndex := 0; replyIndex < 4; replyIndex++ {
			rootID := roots[rootIndex].ID
			targetID := rootID
			if replyIndex > 0 {
				targetID = replies[len(replies)-1].ID
				if targetID == 0 {
					targetID = rootID
				}
			}
			replies = append(replies, infrainteraction.CommentModel{
				VideoID: video.ID, UserID: users[(rootIndex+replyIndex)%3+1].ID,
				RootCommentID: &rootID, ReplyToCommentID: &targetID,
				Content:   fmt.Sprintf("reply-%d-%d", rootIndex, replyIndex),
				Status:    domaininteraction.CommentStatusNormal,
				CreatedAt: base.Add(time.Duration(rootIndex*10+replyIndex) * time.Second),
				UpdatedAt: base.Add(time.Duration(rootIndex*10+replyIndex) * time.Second),
			})
		}
	}
	if err := db.Create(&replies).Error; err != nil {
		t.Fatalf("seed ordered replies: %v", err)
	}
	if err := db.Model(&infravideo.VideoStatModel{}).Where("video_id = ?", video.ID).
		Update("comment_count", len(roots)+len(replies)).Error; err != nil {
		t.Fatalf("seed ordered comment count: %v", err)
	}
	if err := db.Create(&infrainteraction.CommentLikeModel{
		UserID: users[0].ID, CommentID: roots[0].ID, Status: domaininteraction.ActionStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed ordered viewer like: %v", err)
	}

	latest, err := repo.ListCommentRoots(ctx, domaininteraction.CommentRootQuery{
		VideoID: video.ID, Sort: domaininteraction.CommentSortLatest, Limit: 2,
		PreviewLimit: domaininteraction.ReplyPreviewLimit,
	})
	if err != nil {
		t.Fatalf("list latest roots: %v", err)
	}
	if len(latest.Items) != 2 || latest.Items[0].ID != roots[2].ID || latest.Items[1].ID != roots[1].ID {
		t.Fatalf("latest root order is unstable: %+v", latest.Items)
	}
	latestNext, err := repo.ListCommentRoots(ctx, domaininteraction.CommentRootQuery{
		VideoID: video.ID, Sort: domaininteraction.CommentSortLatest, Limit: 2,
		Cursor: &domaininteraction.CommentCursor{
			Version: domaininteraction.CommentCursorVersion, Sort: domaininteraction.CommentSortLatest,
			CreatedAt: latest.Items[1].CreatedAt, CommentID: latest.Items[1].ID,
		},
	})
	if err != nil || len(latestNext.Items) != 1 || latestNext.Items[0].ID != roots[0].ID {
		t.Fatalf("latest cursor did not advance stably: page=%+v err=%v", latestNext, err)
	}

	hot, err := repo.ListCommentRoots(ctx, domaininteraction.CommentRootQuery{
		VideoID: video.ID, Sort: domaininteraction.CommentSortHot, Limit: 2,
		PreviewLimit: domaininteraction.ReplyPreviewLimit,
	})
	if err != nil {
		t.Fatalf("list hot roots: %v", err)
	}
	if len(hot.Items) != 2 || hot.Items[0].ID != roots[0].ID || hot.Items[1].ID != roots[2].ID {
		t.Fatalf("hot root order is unstable: %+v", hot.Items)
	}
	if len(hot.Items[0].ReplyPreviews) != domaininteraction.ReplyPreviewLimit ||
		hot.Items[0].ReplyPreviews[0].Content != "reply-0-0" ||
		hot.Items[0].ReplyPreviews[2].Content != "reply-0-2" {
		t.Fatalf("reply previews were not capped and ordered: %+v", hot.Items[0].ReplyPreviews)
	}
	hotNext, err := repo.ListCommentRoots(ctx, domaininteraction.CommentRootQuery{
		VideoID: video.ID, Sort: domaininteraction.CommentSortHot, Limit: 2,
		Cursor: &domaininteraction.CommentCursor{
			Version: domaininteraction.CommentCursorVersion, Sort: domaininteraction.CommentSortHot,
			HotScore: hot.Items[1].HotScore, CreatedAt: hot.Items[1].CreatedAt, CommentID: hot.Items[1].ID,
		},
	})
	if err != nil || len(hotNext.Items) != 1 || hotNext.Items[0].ID != roots[1].ID {
		t.Fatalf("hot cursor did not advance stably: page=%+v err=%v", hotNext, err)
	}

	bulkRoots := make([]infrainteraction.CommentModel, 0, 100)
	for index := 0; index < 100; index++ {
		createdAt := base.Add(-time.Duration(index+1) * time.Minute)
		bulkRoots = append(bulkRoots, infrainteraction.CommentModel{
			VideoID: video.ID, UserID: users[index%3+1].ID,
			Content: fmt.Sprintf("bulk-root-%03d", index), Status: domaininteraction.CommentStatusNormal,
			ReplyCount: 4, HotScore: int64(index % 17), CreatedAt: createdAt, UpdatedAt: createdAt,
		})
	}
	if err := db.CreateInBatches(&bulkRoots, 100).Error; err != nil {
		t.Fatalf("seed bounded-hydration roots: %v", err)
	}
	bulkReplies := make([]infrainteraction.CommentModel, 0, 400)
	for rootIndex := range bulkRoots {
		for replyIndex := 0; replyIndex < 4; replyIndex++ {
			rootID := bulkRoots[rootIndex].ID
			targetID := rootID
			bulkReplies = append(bulkReplies, infrainteraction.CommentModel{
				VideoID: video.ID, UserID: users[(rootIndex+replyIndex)%3+1].ID,
				RootCommentID: &rootID, ReplyToCommentID: &targetID,
				Content:   fmt.Sprintf("bulk-reply-%03d-%d", rootIndex, replyIndex),
				Status:    domaininteraction.CommentStatusNormal,
				CreatedAt: base.Add(-time.Duration(rootIndex+1)*time.Minute + time.Duration(replyIndex)*time.Second),
				UpdatedAt: base,
			})
		}
	}
	if err := db.CreateInBatches(&bulkReplies, 100).Error; err != nil {
		t.Fatalf("seed bounded-hydration replies: %v", err)
	}
	if err := db.Model(&infravideo.VideoStatModel{}).Where("video_id = ?", video.ID).
		Update("comment_count", len(roots)+len(replies)+len(bulkRoots)+len(bulkReplies)).Error; err != nil {
		t.Fatalf("update bounded-hydration comment count: %v", err)
	}
	smallCounter := &queryCounterLogger{Interface: logger.Default.LogMode(logger.Silent)}
	smallPage, err := infrainteraction.New(db.Session(&gorm.Session{Logger: smallCounter})).ListCommentRoots(
		ctx,
		domaininteraction.CommentRootQuery{
			VideoID: video.ID,
			Viewer:  domaininteraction.CommentViewer{UserID: users[0].ID, Role: domainaccount.RoleUser},
			Sort:    domaininteraction.CommentSortLatest, Limit: 1,
			PreviewLimit: domaininteraction.ReplyPreviewLimit,
		},
	)
	if err != nil {
		t.Fatalf("list small hydrated root page: %v", err)
	}
	if len(smallPage.Items) != 1 {
		t.Fatalf("small hydration returned %d roots, want 1", len(smallPage.Items))
	}

	largeCounter := &queryCounterLogger{Interface: logger.Default.LogMode(logger.Silent)}
	bounded, err := infrainteraction.New(db.Session(&gorm.Session{Logger: largeCounter})).ListCommentRoots(ctx, domaininteraction.CommentRootQuery{
		VideoID: video.ID,
		Viewer:  domaininteraction.CommentViewer{UserID: users[0].ID, Role: domainaccount.RoleUser},
		Sort:    domaininteraction.CommentSortLatest, Limit: 100,
		PreviewLimit: domaininteraction.ReplyPreviewLimit,
	})
	if err != nil {
		t.Fatalf("list bounded hydrated roots: %v", err)
	}
	const maxHydrationQueries = 8
	smallQueries := smallCounter.count.Load()
	largeQueries := largeCounter.count.Load()
	if smallQueries > maxHydrationQueries || largeQueries > maxHydrationQueries {
		t.Fatalf(
			"root hydration exceeded %d queries: small=%d large=%d",
			maxHydrationQueries, smallQueries, largeQueries,
		)
	}
	if largeQueries != smallQueries {
		t.Fatalf("root hydration query count grew with page size: small=%d large=%d", smallQueries, largeQueries)
	}
	if len(bounded.Items) != 100 {
		t.Fatalf("bounded hydration returned %d roots, want 100", len(bounded.Items))
	}
	for _, root := range bounded.Items {
		if len(root.ReplyPreviews) > domaininteraction.ReplyPreviewLimit {
			t.Fatalf("root %d returned %d previews", root.ID, len(root.ReplyPreviews))
		}
	}

	moderationVideo, err := domainvideo.NewPublished(
		users[0].ID, "cascade moderation", "", "/uploads/cascade.mp4", "/uploads/cascade.jpg", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := infravideo.New(db).Save(ctx, moderationVideo); err != nil {
		t.Fatalf("save cascade moderation video: %v", err)
	}
	moderatedRoot, err := repo.CreateThreadedComment(
		ctx, mustRootComment(t, moderationVideo.ID, users[1].ID, "moderated root", "cascade-root"),
	)
	if err != nil {
		t.Fatalf("create moderated root: %v", err)
	}
	for index := 0; index < 2; index++ {
		if _, err := repo.CreateThreadedComment(
			ctx,
			mustReplyComment(
				t, moderationVideo.ID, users[index+2].ID, moderatedRoot.Comment.ID,
				fmt.Sprintf("cascade reply %d", index), fmt.Sprintf("cascade-reply-%d", index),
			),
		); err != nil {
			t.Fatalf("create cascade reply %d: %v", index, err)
		}
	}
	cascade, err := repo.DeleteThreadedComment(
		ctx, moderatedRoot.Comment.ID, users[0].ID, domainaccount.RoleUser,
	)
	if err != nil {
		t.Fatalf("cascade moderate root: %v", err)
	}
	if !cascade.ThreadHidden || cascade.DeletedCount != 3 || cascade.CommentCount != 0 {
		t.Fatalf("unexpected cascade moderation result: %+v", cascade)
	}
	var visibleThreadRows int64
	if err := db.Model(&infrainteraction.CommentModel{}).
		Where("(id = ? OR root_comment_id = ?) AND status <> ?",
			moderatedRoot.Comment.ID, moderatedRoot.Comment.ID, domaininteraction.CommentStatusModerated).
		Count(&visibleThreadRows).Error; err != nil {
		t.Fatalf("count cascade moderation rows: %v", err)
	}
	if visibleThreadRows != 0 {
		t.Fatalf("cascade moderation left %d non-moderated rows", visibleThreadRows)
	}
}

func TestPostgreSQLThreadedCommentReconciliationPreservesConcurrentDelta(t *testing.T) {
	fixture, db, repo, users, video := newPostgresThreadedCommentFixture(t, "reconcile")
	concurrentDB := db.Session(&gorm.Session{NewDB: true})
	ctx := context.Background()

	rootMutation, err := repo.CreateThreadedComment(
		ctx, mustRootComment(t, video.ID, users[1].ID, "root", "reconcile-root"),
	)
	if err != nil {
		t.Fatalf("create reconciliation root: %v", err)
	}
	if _, err := repo.CreateThreadedComment(
		ctx, mustReplyComment(t, video.ID, users[2].ID, rootMutation.Comment.ID, "first reply", "reconcile-reply-1"),
	); err != nil {
		t.Fatalf("create reconciliation reply: %v", err)
	}
	if err := db.Model(&infrainteraction.CommentModel{}).Where("id = ?", rootMutation.Comment.ID).
		Updates(map[string]any{"reply_count": 0, "like_count": 0, "hot_score": 0}).Error; err != nil {
		t.Fatalf("corrupt reconciliation root counters: %v", err)
	}
	if err := db.Model(&infravideo.VideoStatModel{}).Where("video_id = ?", video.ID).
		Update("comment_count", 1).Error; err != nil {
		t.Fatalf("corrupt reconciliation video count: %v", err)
	}

	tx := concurrentDB.Begin()
	if tx.Error != nil {
		t.Fatalf("begin concurrent threaded transaction: %v", tx.Error)
	}
	var lockedRoot infrainteraction.CommentModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", rootMutation.Comment.ID).Take(&lockedRoot).Error; err != nil {
		_ = tx.Rollback()
		t.Fatalf("lock reconciliation root: %v", err)
	}
	var lockedStat infravideo.VideoStatModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("video_id = ?", video.ID).Take(&lockedStat).Error; err != nil {
		_ = tx.Rollback()
		t.Fatalf("lock reconciliation video stat: %v", err)
	}
	rootID := rootMutation.Comment.ID
	if err := tx.Create(&infrainteraction.CommentModel{
		VideoID: video.ID, UserID: users[3].ID, RootCommentID: &rootID, ReplyToCommentID: &rootID,
		Content: "concurrent reply", Status: domaininteraction.CommentStatusNormal,
	}).Error; err != nil {
		_ = tx.Rollback()
		t.Fatalf("create concurrent reply fact: %v", err)
	}
	if err := tx.Create(&infrainteraction.CommentLikeModel{
		UserID: users[0].ID, CommentID: rootID, Status: domaininteraction.ActionStatusActive,
	}).Error; err != nil {
		_ = tx.Rollback()
		t.Fatalf("create concurrent like fact: %v", err)
	}
	if err := tx.Model(&infrainteraction.CommentModel{}).Where("id = ?", rootID).Updates(map[string]any{
		"reply_count": gorm.Expr("reply_count + 1"),
		"like_count":  gorm.Expr("like_count + 1"),
		"hot_score":   gorm.Expr("(like_count + 1) * 3 + (reply_count + 1) * 5"),
	}).Error; err != nil {
		_ = tx.Rollback()
		t.Fatalf("apply concurrent root counter delta: %v", err)
	}
	if err := tx.Model(&infravideo.VideoStatModel{}).Where("video_id = ?", video.ID).
		Update("comment_count", gorm.Expr("comment_count + 1")).Error; err != nil {
		_ = tx.Rollback()
		t.Fatalf("apply concurrent video counter delta: %v", err)
	}

	reconcileDB := fixture.openGORMWithMaxOpenConns(t, 1)
	var reconcileBackendPID int
	if err := reconcileDB.Raw("SELECT pg_backend_pid()").Scan(&reconcileBackendPID).Error; err != nil {
		_ = tx.Rollback()
		t.Fatalf("read reconciliation backend PID: %v", err)
	}
	reconcileErr := make(chan error, 1)
	go func() {
		reconcileErr <- infrainteraction.New(reconcileDB).ReconcileCommentCounters(context.Background())
	}()
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWait()
	if err := waitForPostgreSQLBlockedBackend(waitCtx, fixture.admin, reconcileBackendPID); err != nil {
		_ = tx.Rollback()
		select {
		case reconcileResult := <-reconcileErr:
			t.Fatalf("threaded reconciliation completed before reaching the held lock: %v", reconcileResult)
		default:
			t.Fatalf("threaded reconciliation was not observed blocked by the held lock: %v", err)
		}
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit concurrent threaded delta: %v", err)
	}
	if err := <-reconcileErr; err != nil {
		t.Fatalf("reconcile threaded counters: %v", err)
	}

	var reconciledRoot infrainteraction.CommentModel
	if err := db.Where("id = ?", rootID).Take(&reconciledRoot).Error; err != nil {
		t.Fatalf("load concurrently reconciled root: %v", err)
	}
	var reconciledStat infravideo.VideoStatModel
	if err := db.Where("video_id = ?", video.ID).Take(&reconciledStat).Error; err != nil {
		t.Fatalf("load concurrently reconciled video stat: %v", err)
	}
	if reconciledRoot.ReplyCount != 2 || reconciledRoot.LikeCount != 1 ||
		reconciledRoot.HotScore != 13 || reconciledStat.CommentCount != 3 {
		t.Fatalf("reconciliation overwrote a concurrent delta: root=%+v stat=%+v", reconciledRoot, reconciledStat)
	}
}

func newPostgresThreadedCommentFixture(
	t *testing.T,
	suffix string,
) (*postgresFixture, *gorm.DB, *infrainteraction.Repository, []*domainaccount.User, *domainvideo.Video) {
	t.Helper()
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate threaded comment %s fixture: %v", suffix, err)
	}
	ctx := context.Background()
	accountRepo := infraaccount.New(db)
	users := make([]*domainaccount.User, 0, 4)
	for index := 0; index < 4; index++ {
		user, err := domainaccount.New(
			fmt.Sprintf("thread-%s-%d", suffix, index),
			"CaseSensitivePassword",
			fmt.Sprintf("Thread %s %d", suffix, index),
		)
		if err != nil {
			t.Fatalf("new threaded fixture account %d: %v", index, err)
		}
		if err := accountRepo.Save(ctx, user); err != nil {
			t.Fatalf("save threaded fixture account %d: %v", index, err)
		}
		users = append(users, user)
	}
	video, err := domainvideo.NewPublished(
		users[0].ID,
		"Threaded "+suffix,
		"",
		"/uploads/threaded-"+suffix+".mp4",
		"/uploads/threaded-"+suffix+".jpg",
		"",
	)
	if err != nil {
		t.Fatalf("new threaded fixture video: %v", err)
	}
	if err := infravideo.New(db).Save(ctx, video); err != nil {
		t.Fatalf("save threaded fixture video: %v", err)
	}
	return fixture, db, infrainteraction.New(db), users, video
}

func waitForPostgreSQLBlockedBackend(ctx context.Context, db *sql.DB, backendPID int) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		var state string
		var waitEventType string
		var blockingCount int
		err := db.QueryRowContext(ctx, `
			SELECT state, COALESCE(wait_event_type, ''), cardinality(pg_blocking_pids(pid))
			FROM pg_stat_activity
			WHERE pid = $1
		`, backendPID).Scan(&state, &waitEventType, &blockingCount)
		if err != nil {
			return fmt.Errorf("inspect PostgreSQL backend %d: %w", backendPID, err)
		}
		if state == "active" && waitEventType == "Lock" && blockingCount > 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"backend %d did not block on a PostgreSQL lock (state=%q wait_event_type=%q blockers=%d): %w",
				backendPID, state, waitEventType, blockingCount, ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func TestPostgreSQLAcceptedInteractionEventPersistsAfterPrivacyChange(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate accepted interaction event test schema: %v", err)
	}

	ctx := context.Background()
	accountRepo := infraaccount.New(db)
	author, err := domainaccount.New("event-author", "CaseSensitivePassword", "Author")
	if err != nil {
		t.Fatalf("new event author: %v", err)
	}
	if err := accountRepo.Save(ctx, author); err != nil {
		t.Fatalf("save event author: %v", err)
	}
	actor, err := domainaccount.New("event-actor", "CaseSensitivePassword", "Actor")
	if err != nil {
		t.Fatalf("new event actor: %v", err)
	}
	if err := accountRepo.Save(ctx, actor); err != nil {
		t.Fatalf("save event actor: %v", err)
	}

	videoRepo := infravideo.New(db)
	video, err := domainvideo.NewPublished(
		author.ID,
		"Accepted interaction",
		"",
		"/uploads/accepted-interaction.mp4",
		"/uploads/accepted-interaction.jpg",
		"",
	)
	if err != nil {
		t.Fatalf("new accepted interaction video: %v", err)
	}
	if err := videoRepo.Save(ctx, video); err != nil {
		t.Fatalf("save accepted interaction video: %v", err)
	}

	interactionRepo := infrainteraction.New(db)
	if _, err := interactionRepo.GetVideoStat(ctx, video.ID); err != nil {
		t.Fatalf("public video should accept interaction before enqueueing: %v", err)
	}
	accepted, err := domaininteraction.NewAcceptedActionEvent(
		"accepted-like-event",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeLike,
		true,
		"accepted-like-request",
		1,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("new accepted action event: %v", err)
	}
	if _, _, err := videoRepo.ApplyBatch(
		ctx,
		author.ID,
		domainvideo.BatchActionMakePrivate,
		[]int64{video.ID},
		"private-before-consume",
		"private-before-consume-fingerprint",
	); err != nil {
		t.Fatalf("make accepted interaction video private: %v", err)
	}
	if _, _, _, err := interactionRepo.SetAction(
		ctx,
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeFavorite,
		true,
		"new-private-request",
	); !errors.Is(err, domaininteraction.ErrVideoNotFound) {
		t.Fatalf("new synchronous interaction should reject private video, got %v", err)
	}

	if err := interactionRepo.PersistAcceptedActionEvent(ctx, accepted); err != nil {
		t.Fatalf("persist accepted event after privacy change: %v", err)
	}
	if err := interactionRepo.PersistAcceptedActionEvent(ctx, accepted); err != nil {
		t.Fatalf("replay accepted event: %v", err)
	}

	var action infrainteraction.ActionModel
	if err := db.Where(
		"user_id = ? AND video_id = ? AND action_type = ?",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeLike,
	).Take(&action).Error; err != nil {
		t.Fatalf("load persisted accepted action: %v", err)
	}
	if action.Status != domaininteraction.ActionStatusActive {
		t.Fatalf("accepted action was not active: %+v", action)
	}
	var stat infravideo.VideoStatModel
	if err := db.Where("video_id = ?", video.ID).Take(&stat).Error; err != nil {
		t.Fatalf("load accepted action stat: %v", err)
	}
	if stat.LikeCount != 1 {
		t.Fatalf("duplicate delivery corrupted like count: %+v", stat)
	}
	var authorStat infravideo.UserContentStatModel
	if err := db.Where("user_id = ?", author.ID).Take(&authorStat).Error; err != nil {
		t.Fatalf("load author content stat: %v", err)
	}
	if authorStat.ReceivedLikeCount != 1 {
		t.Fatalf("duplicate delivery corrupted received likes: %+v", authorStat)
	}
	var receiptCount int64
	if err := db.Model(&infrainteraction.ActionEventModel{}).Count(&receiptCount).Error; err != nil {
		t.Fatalf("count action event receipts: %v", err)
	}
	if receiptCount != 1 {
		t.Fatalf("expected one action event receipt, got %d", receiptCount)
	}

	conflict := *accepted
	conflict.Active = false
	if err := interactionRepo.PersistAcceptedActionEvent(ctx, &conflict); !errors.Is(err, domaininteraction.ErrActionEventConflict) {
		t.Fatalf("expected event payload conflict, got %v", err)
	}
	acceptedUnlike, err := domaininteraction.NewAcceptedActionEvent(
		"accepted-unlike-event",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeLike,
		false,
		"accepted-unlike-request",
		2,
		accepted.OccurredAt.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("new accepted unlike event: %v", err)
	}
	if err := interactionRepo.PersistAcceptedActionEvent(ctx, acceptedUnlike); err != nil {
		t.Fatalf("persist accepted unlike event: %v", err)
	}
	if err := interactionRepo.PersistAcceptedActionEvent(ctx, accepted); err != nil {
		t.Fatalf("replay older accepted like after unlike: %v", err)
	}
	if err := db.Where("id = ?", action.ID).Take(&action).Error; err != nil {
		t.Fatalf("reload action after unlike and replay: %v", err)
	}
	if action.Status != domaininteraction.ActionStatusCanceled {
		t.Fatalf("older duplicate reverted newer action state: %+v", action)
	}
	if err := db.Where("video_id = ?", video.ID).Take(&stat).Error; err != nil {
		t.Fatalf("reload stat after unlike and replay: %v", err)
	}
	if stat.LikeCount != 0 {
		t.Fatalf("older duplicate corrupted like count after unlike: %+v", stat)
	}
	if err := db.Where("user_id = ?", author.ID).Take(&authorStat).Error; err != nil {
		t.Fatalf("reload author stat after unlike and replay: %v", err)
	}
	if authorStat.ReceivedLikeCount != 0 {
		t.Fatalf("older duplicate corrupted received likes after unlike: %+v", authorStat)
	}
	missing, err := domaininteraction.NewAcceptedActionEvent(
		"missing-video-event",
		actor.ID,
		video.ID+9999,
		domaininteraction.ActionTypeLike,
		true,
		"",
		1,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("new missing video event: %v", err)
	}
	if err := interactionRepo.PersistAcceptedActionEvent(ctx, missing); !errors.Is(err, domaininteraction.ErrVideoNotFound) {
		t.Fatalf("missing video should be terminal not found, got %v", err)
	}

	if _, _, err := videoRepo.ApplyBatch(
		ctx,
		author.ID,
		domainvideo.BatchActionDelete,
		[]int64{video.ID},
		"delete-before-event",
		"delete-before-event-fingerprint",
	); err != nil {
		t.Fatalf("delete accepted interaction video: %v", err)
	}
	deleted, err := domaininteraction.NewAcceptedActionEvent(
		"deleted-video-event",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeFavorite,
		true,
		"",
		1,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("new deleted video event: %v", err)
	}
	if err := interactionRepo.PersistAcceptedActionEvent(ctx, deleted); !errors.Is(err, domaininteraction.ErrVideoNotFound) {
		t.Fatalf("deleted video should be terminal not found, got %v", err)
	}
	if err := db.Where("user_id = ?", author.ID).Take(&authorStat).Error; err != nil {
		t.Fatalf("reload author content stat after delete: %v", err)
	}
	if authorStat.ReceivedLikeCount != 0 {
		t.Fatalf("deleted video retained received likes: %+v", authorStat)
	}
	if err := db.Model(&infrainteraction.ActionEventModel{}).Count(&receiptCount).Error; err != nil {
		t.Fatalf("recount action event receipts: %v", err)
	}
	if receiptCount != 2 {
		t.Fatalf("terminal missing/deleted events should roll back receipts, got %d", receiptCount)
	}
}

func TestPostgreSQLSynchronousInteractionCreatesRecommendationHandoffs(t *testing.T) {
	db, repository, _, actor, video := newPostgresInteractionEventFixture(t)
	occurredAt := time.Now().UTC().Truncate(time.Microsecond)
	event, err := domaininteraction.NewAcceptedActionEventWithRecommendation(
		"sync-recommendation-like",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeLike,
		true,
		"sync-recommendation-key",
		"sync-recommendation-request",
		0,
		occurredAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	action, count, delta, err := repository.SetActionWithAcceptedEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("synchronously accept recommendation action: %v", err)
	}
	if action == nil || !action.Active() || count != 1 || delta != 1 {
		t.Fatalf("unexpected synchronous action result: action=%#v count=%d delta=%d", action, count, delta)
	}

	var receipt infrainteraction.ActionEventModel
	if err := db.Where("event_id = ?", event.EventID).Take(&receipt).Error; err != nil {
		t.Fatalf("load synchronous action receipt: %v", err)
	}
	if receipt.RecommendationRequestID != "sync-recommendation-request" || receipt.Version <= 0 ||
		receipt.ProfileProjectionAvailableAt.IsZero() || receipt.RecommendationOutcomeAvailableAt.IsZero() ||
		receipt.ProfileProjectionDispatchedAt != nil || receipt.RecommendationOutcomeDispatchedAt != nil {
		t.Fatalf("synchronous receipt did not retain pending profile/outcome handoffs: %#v", receipt)
	}

	retry := *event
	retry.EventID = "sync-recommendation-retry"
	if action, count, delta, err = repository.SetActionWithAcceptedEvent(context.Background(), &retry); err != nil || action == nil || !action.Active() || count != 1 || delta != 0 {
		t.Fatalf("synchronous idempotent retry = action=%#v count=%d delta=%d err=%v", action, count, delta, err)
	}
	var receipts int64
	if err := db.Model(&infrainteraction.ActionEventModel{}).Where("user_id = ? AND video_id = ?", actor.ID, video.ID).Count(&receipts).Error; err != nil {
		t.Fatalf("count synchronous receipts: %v", err)
	}
	if receipts != 1 {
		t.Fatalf("idempotent synchronous retry created a second handoff: %d", receipts)
	}
}

func TestPostgreSQLSynchronousInteractionStoresNoOpIdempotencyReceipts(t *testing.T) {
	db, repository, _, actor, video := newPostgresInteractionEventFixture(t)
	ctx := context.Background()
	occurredAt := time.Now().UTC().Truncate(time.Microsecond)
	newEvent := func(eventID string, active bool, key string) *domaininteraction.AcceptedActionEvent {
		t.Helper()
		event, err := domaininteraction.NewAcceptedActionEvent(
			eventID, actor.ID, video.ID, domaininteraction.ActionTypeLike, active, key, 0, occurredAt,
		)
		if err != nil {
			t.Fatalf("new action event %q: %v", eventID, err)
		}
		return event
	}

	first := newEvent("sync-like-original", true, "sync-like-original-key")
	action, count, delta, err := repository.SetActionWithAcceptedEvent(ctx, first)
	if err != nil || action == nil || !action.Active() || count != 1 || delta != 1 {
		t.Fatalf("initial like = action=%#v count=%d delta=%d err=%v", action, count, delta, err)
	}
	unlike := newEvent("sync-unlike-new-key", false, "sync-unlike-new-key")
	if _, count, delta, err = repository.SetActionWithAcceptedEvent(ctx, unlike); err != nil || count != 0 || delta != -1 {
		t.Fatalf("new-key unlike = count=%d delta=%d err=%v", count, delta, err)
	}
	replay := newEvent("sync-like-replay", true, "sync-like-original-key")
	action, count, delta, err = repository.SetActionWithAcceptedEvent(ctx, replay)
	if err != nil || action == nil || !action.Active() || count != 1 || delta != 0 {
		t.Fatalf("same-key replay did not return its original result: action=%#v count=%d delta=%d err=%v", action, count, delta, err)
	}
	conflict := newEvent("sync-like-key-conflict", false, "sync-like-original-key")
	if _, _, _, err := repository.SetActionWithAcceptedEvent(ctx, conflict); !errors.Is(err, domaininteraction.ErrActionIdempotencyConflict) {
		t.Fatalf("opposite desired action with the same key = %v, want conflict", err)
	}

	absentUnlike := &domaininteraction.AcceptedActionEvent{
		EventID: "sync-absent-favorite-unlike", UserID: actor.ID, VideoID: video.ID,
		ActionType: domaininteraction.ActionTypeFavorite, Active: false, IdempotencyKey: "sync-absent-favorite-key",
		OccurredAt: occurredAt,
	}
	action, count, delta, err = repository.SetActionWithAcceptedEvent(ctx, absentUnlike)
	if err != nil || action == nil || action.Active() || count != 0 || delta != 0 {
		t.Fatalf("absent unlike = action=%#v count=%d delta=%d err=%v", action, count, delta, err)
	}

	var handoffs int64
	if err := db.Model(&infrainteraction.ActionEventModel{}).Where("user_id = ? AND video_id = ?", actor.ID, video.ID).Count(&handoffs).Error; err != nil {
		t.Fatalf("count state-change handoffs: %v", err)
	}
	if handoffs != 2 {
		t.Fatalf("same-state and absent-unlike requests created handoffs: %d", handoffs)
	}
	var receipts int64
	if err := db.Model(&infrainteraction.ActionIdempotencyReceiptModel{}).Where("user_id = ? AND video_id = ?", actor.ID, video.ID).Count(&receipts).Error; err != nil {
		t.Fatalf("count idempotency receipts: %v", err)
	}
	if receipts != 3 {
		t.Fatalf("expected transition and no-op idempotency receipts, got %d", receipts)
	}
}

func TestPostgreSQLInteractionActionOrderBackfill(t *testing.T) {
	db, _, _, actor, video := newPostgresInteractionEventFixture(t)

	if err := db.Exec(`
		ALTER TABLE interaction_action
		DROP COLUMN latest_event_version,
		DROP COLUMN latest_event_id,
		DROP COLUMN latest_event_occurred_at
	`).Error; err != nil {
		t.Fatalf("restore legacy interaction_action shape: %v", err)
	}
	updatedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	if err := db.Exec(`
		INSERT INTO interaction_action (
			user_id, video_id, action_type, status, idempotency_key, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeFavorite,
		domaininteraction.ActionStatusCanceled,
		"legacy-favorite",
		updatedAt.Add(-time.Minute),
		updatedAt,
	).Error; err != nil {
		t.Fatalf("insert legacy action row: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate legacy action event order: %v", err)
	}
	var action infrainteraction.ActionModel
	if err := db.Where(
		"user_id = ? AND video_id = ? AND action_type = ?",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeFavorite,
	).Take(&action).Error; err != nil {
		t.Fatalf("load backfilled action: %v", err)
	}
	if action.LatestEventOccurredAt == nil || !action.LatestEventOccurredAt.Equal(updatedAt) {
		t.Fatalf("expected latest event time backfilled from updated_at, got %+v", action)
	}
	if action.LatestEventID == nil || *action.LatestEventID != "" {
		t.Fatalf("expected deterministic empty legacy event id, got %+v", action.LatestEventID)
	}
	if action.LatestEventVersion != 0 {
		t.Fatalf("expected legacy action version zero, got %d", action.LatestEventVersion)
	}
}

func TestPostgreSQLInteractionEventsApplyDeterministicLatestOrder(t *testing.T) {
	db, repo, author, actor, video := newPostgresInteractionEventFixture(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)

	newerUnlike := mustAcceptedActionEventWithRecommendation(
		t,
		"newer-unlike",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeLike,
		false,
		"newer-unlike-request",
		"newer-unlike-recommendation",
		2,
		base,
	)
	olderLike := mustAcceptedActionEventWithRecommendation(
		t,
		"older-like",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeLike,
		true,
		"older-like-request",
		"older-like-recommendation",
		1,
		base.Add(time.Second),
	)
	if err := repo.PersistAcceptedActionEvent(ctx, newerUnlike); err != nil {
		t.Fatalf("persist newer unlike: %v", err)
	}
	if err := repo.PersistAcceptedActionEvent(ctx, olderLike); err != nil {
		t.Fatalf("acknowledge stale older like: %v", err)
	}
	if err := repo.PersistAcceptedActionEvent(ctx, olderLike); err != nil {
		t.Fatalf("acknowledge duplicate older like: %v", err)
	}
	assertPostgreSQLLikeState(t, db, author.ID, actor.ID, video.ID, false, 0, 0)

	tieTime := base.Add(2 * time.Second)
	tieLike := mustAcceptedActionEventWithRecommendation(
		t,
		"tie-a-like",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeLike,
		true,
		"tie-like-request",
		"tie-like-recommendation",
		3,
		tieTime,
	)
	tieUnlike := mustAcceptedActionEventWithRecommendation(
		t,
		"tie-z-unlike",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeLike,
		false,
		"tie-unlike-request",
		"tie-unlike-recommendation",
		3,
		tieTime,
	)
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, event := range []*domaininteraction.AcceptedActionEvent{tieLike, tieUnlike} {
		event := event
		go func() {
			<-start
			errs <- repo.PersistAcceptedActionEvent(context.Background(), event)
		}()
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("persist equal-time action event: %v", err)
		}
	}
	if err := repo.PersistAcceptedActionEvent(ctx, tieUnlike); err != nil {
		t.Fatalf("acknowledge duplicate tie winner: %v", err)
	}
	if err := repo.PersistAcceptedActionEvent(ctx, tieLike); err != nil {
		t.Fatalf("acknowledge stale tie loser: %v", err)
	}
	assertPostgreSQLLikeState(t, db, author.ID, actor.ID, video.ID, false, 0, 0)

	var action infrainteraction.ActionModel
	if err := db.Where(
		"user_id = ? AND video_id = ? AND action_type = ?",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeLike,
	).Take(&action).Error; err != nil {
		t.Fatalf("load ordered action row: %v", err)
	}
	if action.LatestEventOccurredAt == nil || !action.LatestEventOccurredAt.Equal(tieTime) {
		t.Fatalf("unexpected latest event time: %+v", action)
	}
	if action.LatestEventID == nil || *action.LatestEventID != tieUnlike.EventID {
		t.Fatalf("equal-time tie-break did not keep greatest event id: %+v", action)
	}
	if action.LatestEventVersion != 3 {
		t.Fatalf("expected latest action version 3, got %+v", action)
	}

	var receipts []infrainteraction.ActionEventModel
	if err := db.Order("event_id ASC").Find(&receipts).Error; err != nil {
		t.Fatalf("list ordered action receipts: %v", err)
	}
	if len(receipts) != 4 {
		t.Fatalf("expected receipts for four distinct events, got %d", len(receipts))
	}
	expectedReceiptTimes := map[string]time.Time{
		newerUnlike.EventID: newerUnlike.OccurredAt,
		olderLike.EventID:   olderLike.OccurredAt,
		tieLike.EventID:     tieLike.OccurredAt,
		tieUnlike.EventID:   tieUnlike.OccurredAt,
	}
	for _, receipt := range receipts {
		occurredAt, ok := expectedReceiptTimes[receipt.EventID]
		if !ok {
			t.Fatalf("unexpected action receipt: %+v", receipt)
		}
		delete(expectedReceiptTimes, receipt.EventID)
		if receipt.ProfileProjectionDispatchedAt != nil || !receipt.ProfileProjectionAvailableAt.Equal(occurredAt) {
			t.Fatalf("receipt missing profile-projection handoff: %+v", receipt)
		}
		if receipt.RecommendationOutcomeDispatchedAt != nil || !receipt.RecommendationOutcomeAvailableAt.Equal(occurredAt) {
			t.Fatalf("receipt missing recommendation-outcome handoff: %+v", receipt)
		}
	}
	if len(expectedReceiptTimes) != 0 {
		t.Fatalf("missing action receipts: %#v", expectedReceiptTimes)
	}

	leaseNow := base.Add(time.Minute)
	profiles, err := repo.ClaimActionProfileProjections(ctx, 10, leaseNow, leaseNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("claim profile projection handoffs: %v", err)
	}
	if len(profiles) != 4 {
		t.Fatalf("expected four profile handoffs, got %+v", profiles)
	}
	profileIDs := map[string]bool{}
	for _, profile := range profiles {
		profileIDs[profile.EventID] = true
	}
	for _, eventID := range []string{
		newerUnlike.EventID,
		olderLike.EventID,
		tieLike.EventID,
		tieUnlike.EventID,
	} {
		if !profileIDs[eventID] {
			t.Fatalf("missing profile handoff for %q: %+v", eventID, profiles)
		}
	}

	outcomes, err := repo.ClaimRecommendationActionOutcomes(ctx, 10, leaseNow, leaseNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("claim recommendation outcome handoffs: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected stale and current active outcome handoffs, got %+v", outcomes)
	}
	outcomeIDs := map[string]bool{}
	for _, outcome := range outcomes {
		outcomeIDs[outcome.EventID] = true
	}
	if !outcomeIDs[olderLike.EventID] || !outcomeIDs[tieLike.EventID] {
		t.Fatalf("missing immutable active outcome handoff: %+v", outcomes)
	}

	sameStateBase := base.Add(3 * time.Second)
	activeV1 := mustAcceptedActionEventWithRecommendation(
		t, "favorite-active-v1", actor.ID, video.ID, domaininteraction.ActionTypeFavorite, true,
		"favorite-v1", "favorite-request-v1", 1, sameStateBase,
	)
	activeV3 := mustAcceptedActionEventWithRecommendation(
		t, "favorite-active-v3", actor.ID, video.ID, domaininteraction.ActionTypeFavorite, true,
		"favorite-v3", "favorite-request-v3", 3, sameStateBase.Add(2*time.Second),
	)
	inactiveV2 := mustAcceptedActionEventWithRecommendation(
		t, "favorite-inactive-v2", actor.ID, video.ID, domaininteraction.ActionTypeFavorite, false,
		"favorite-v2", "favorite-request-v2", 2, sameStateBase.Add(time.Second),
	)
	for _, event := range []*domaininteraction.AcceptedActionEvent{activeV1, activeV3, inactiveV2} {
		if err := repo.PersistAcceptedActionEvent(ctx, event); err != nil {
			t.Fatalf("persist same-state ordering event %q: %v", event.EventID, err)
		}
	}
	var favorite infrainteraction.ActionModel
	if err := db.Where(
		"user_id = ? AND video_id = ? AND action_type = ?",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeFavorite,
	).Take(&favorite).Error; err != nil {
		t.Fatalf("load same-state ordered favorite: %v", err)
	}
	if favorite.Status != domaininteraction.ActionStatusActive || favorite.LatestEventVersion != activeV3.Version ||
		favorite.LatestEventID == nil || *favorite.LatestEventID != activeV3.EventID ||
		favorite.RecommendationRequestID != activeV3.RecommendationRequestID {
		t.Fatalf("newer same-state event did not protect against stale reversal: %+v", favorite)
	}
	var favoriteReceipts []infrainteraction.ActionEventModel
	if err := db.Where("action_type = ?", domaininteraction.ActionTypeFavorite).Order("event_id ASC").Find(&favoriteReceipts).Error; err != nil {
		t.Fatalf("load same-state action receipts: %v", err)
	}
	if len(favoriteReceipts) != 3 {
		t.Fatalf("expected every same-state/stale action receipt and handoff, got %+v", favoriteReceipts)
	}
	for _, receipt := range favoriteReceipts {
		if receipt.ProfileProjectionDispatchedAt != nil || receipt.RecommendationOutcomeDispatchedAt != nil {
			t.Fatalf("same-state event lost its durable handoff: %+v", receipt)
		}
	}
}

func TestPostgreSQLConcurrentInteractionWorkersOrderByVersion(t *testing.T) {
	db, repo, author, actor, video := newPostgresInteractionEventFixture(t)
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	like := &applicationinteraction.ActionChangedEvent{
		EventID:        "worker-like-v1",
		UserID:         actor.ID,
		VideoID:        video.ID,
		ActionType:     domaininteraction.ActionTypeLike,
		Active:         true,
		IdempotencyKey: "worker-like-v1",
		Version:        1,
		OccurredAt:     base.Add(time.Second),
	}
	unlike := &applicationinteraction.ActionChangedEvent{
		EventID:        "worker-unlike-v2",
		UserID:         actor.ID,
		VideoID:        video.ID,
		ActionType:     domaininteraction.ActionTypeLike,
		Active:         false,
		IdempotencyKey: "worker-unlike-v2",
		Version:        2,
		OccurredAt:     base,
	}
	workers := []*applicationinteraction.ActionWorker{
		applicationinteraction.NewActionWorker(repo, nil),
		applicationinteraction.NewActionWorker(repo, nil),
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for index, event := range []*applicationinteraction.ActionChangedEvent{like, unlike} {
		index, event := index, event
		go func() {
			<-start
			errs <- workers[index].HandleActionChanged(context.Background(), event)
		}()
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent worker persistence: %v", err)
		}
	}
	assertPostgreSQLLikeState(t, db, author.ID, actor.ID, video.ID, false, 0, 0)
	var action infrainteraction.ActionModel
	if err := db.Where(
		"user_id = ? AND video_id = ? AND action_type = ?",
		actor.ID,
		video.ID,
		domaininteraction.ActionTypeLike,
	).Take(&action).Error; err != nil {
		t.Fatalf("load worker-ordered action: %v", err)
	}
	if action.LatestEventVersion != 2 || action.LatestEventID == nil || *action.LatestEventID != unlike.EventID {
		t.Fatalf("concurrent workers did not keep version winner: %+v", action)
	}
}

func TestPostgreSQLDirectFollowState(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate direct follow state fixture: %v", err)
	}
	ctx := context.Background()
	accountRepo := infraaccount.New(db)
	viewer, err := domainaccount.New("direct-follow-viewer", "CaseSensitivePassword", "Viewer")
	if err != nil {
		t.Fatalf("new direct follow viewer: %v", err)
	}
	target, err := domainaccount.New("direct-follow-target", "CaseSensitivePassword", "Target")
	if err != nil {
		t.Fatalf("new direct follow target: %v", err)
	}
	if err := accountRepo.Save(ctx, viewer); err != nil {
		t.Fatalf("save direct follow viewer: %v", err)
	}
	if err := accountRepo.Save(ctx, target); err != nil {
		t.Fatalf("save direct follow target: %v", err)
	}
	repo := infrarelation.New(db)
	following, err := repo.IsFollowing(ctx, viewer.ID, target.ID)
	if err != nil || following {
		t.Fatalf("unexpected initial direct follow state: following=%v err=%v", following, err)
	}
	if _, _, _, err := repo.SetFollow(ctx, viewer.ID, target.ID, true, "direct-follow"); err != nil {
		t.Fatalf("persist direct follow: %v", err)
	}
	following, err = repo.IsFollowing(ctx, viewer.ID, target.ID)
	if err != nil || !following {
		t.Fatalf("direct follow state did not become active: following=%v err=%v", following, err)
	}
	if _, _, _, err := repo.SetFollow(ctx, viewer.ID, target.ID, false, "direct-unfollow"); err != nil {
		t.Fatalf("persist direct unfollow: %v", err)
	}
	following, err = repo.IsFollowing(ctx, viewer.ID, target.ID)
	if err != nil || following {
		t.Fatalf("direct follow state did not become inactive: following=%v err=%v", following, err)
	}
	if _, err := repo.IsFollowing(ctx, viewer.ID, target.ID+9999); !errors.Is(err, domainrelation.ErrTargetUserNotFound) {
		t.Fatalf("missing direct follow target should be not found, got %v", err)
	}
}

func TestPostgreSQLFollowProfileOutboxBackfillIsSetBasedAndIdempotent(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := db.AutoMigrate(&infrarelation.FollowModel{}, &infrarelation.FollowProfileOutboxModel{}); err != nil {
		t.Fatalf("migrate relation backfill tables: %v", err)
	}
	updatedAt := time.Date(2026, 7, 27, 7, 0, 0, 123000000, time.UTC)
	follows := []infrarelation.FollowModel{
		{UserID: 1, TargetUserID: 2, Status: domainrelation.FollowStatusActive, UpdatedAt: updatedAt},
		{UserID: 1, TargetUserID: 3, Status: domainrelation.FollowStatusCanceled, UpdatedAt: updatedAt.Add(time.Microsecond)},
	}
	if err := db.Create(&follows).Error; err != nil {
		t.Fatalf("seed follows: %v", err)
	}
	if err := db.Order("id ASC").Find(&follows).Error; err != nil {
		t.Fatalf("reload seeded follows: %v", err)
	}

	counter := &queryCounterLogger{Interface: logger.Default.LogMode(logger.Silent)}
	backfillDB := db.Session(&gorm.Session{Logger: counter})
	if err := infrarelation.EnsureFollowProfileOutbox(backfillDB); err != nil {
		t.Fatalf("set-based follow outbox backfill: %v", err)
	}
	if got := counter.count.Load(); got != 1 {
		t.Fatalf("follow outbox backfill issued %d queries, want one INSERT ... SELECT", got)
	}
	var outboxes []infrarelation.FollowProfileOutboxModel
	if err := db.Order("follow_id ASC").Find(&outboxes).Error; err != nil {
		t.Fatalf("load backfilled follow outboxes: %v", err)
	}
	if len(outboxes) != len(follows) {
		t.Fatalf("backfill rows = %d, want %d", len(outboxes), len(follows))
	}
	for index, follow := range follows {
		outbox := outboxes[index]
		expectedEventID := fmt.Sprintf(
			"relation:follow:%d:%d:%d",
			follow.ID,
			follow.Status,
			follow.UpdatedAt.UTC().UnixNano(),
		)
		if outbox.EventID != expectedEventID || outbox.FollowID != follow.ID ||
			outbox.UserID != follow.UserID || outbox.TargetUserID != follow.TargetUserID ||
			outbox.Active != (follow.Status == domainrelation.FollowStatusActive) ||
			!outbox.OccurredAt.Equal(follow.UpdatedAt) || !outbox.AvailableAt.Equal(follow.UpdatedAt) ||
			outbox.RecommendationRequestID != "" || outbox.RecommendationVideoID != 0 {
			t.Fatalf("backfill changed stable follow signal: follow=%+v outbox=%+v", follow, outbox)
		}
	}

	if err := infrarelation.EnsureFollowProfileOutbox(db); err != nil {
		t.Fatalf("repeat follow outbox backfill: %v", err)
	}
	var count int64
	if err := db.Model(&infrarelation.FollowProfileOutboxModel{}).Count(&count).Error; err != nil {
		t.Fatalf("count repeated follow outbox backfill: %v", err)
	}
	if count != int64(len(follows)) {
		t.Fatalf("repeat backfill was not idempotent: %d rows", count)
	}
}

func newPostgresInteractionEventFixture(t *testing.T) (*gorm.DB, *infrainteraction.Repository, *domainaccount.User, *domainaccount.User, *domainvideo.Video) {
	t.Helper()
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate interaction event fixture: %v", err)
	}

	ctx := context.Background()
	accountRepo := infraaccount.New(db)
	author, err := domainaccount.New("ordered-event-author", "CaseSensitivePassword", "Author")
	if err != nil {
		t.Fatalf("new ordered event author: %v", err)
	}
	if err := accountRepo.Save(ctx, author); err != nil {
		t.Fatalf("save ordered event author: %v", err)
	}
	actor, err := domainaccount.New("ordered-event-actor", "CaseSensitivePassword", "Actor")
	if err != nil {
		t.Fatalf("new ordered event actor: %v", err)
	}
	if err := accountRepo.Save(ctx, actor); err != nil {
		t.Fatalf("save ordered event actor: %v", err)
	}

	video, err := domainvideo.NewPublished(
		author.ID,
		"Ordered interaction",
		"",
		"/uploads/ordered-interaction.mp4",
		"/uploads/ordered-interaction.jpg",
		"",
	)
	if err != nil {
		t.Fatalf("new ordered interaction video: %v", err)
	}
	if err := infravideo.New(db).Save(ctx, video); err != nil {
		t.Fatalf("save ordered interaction video: %v", err)
	}
	return db, infrainteraction.New(db), author, actor, video
}

func mustAcceptedActionEvent(t *testing.T, eventID string, userID int64, videoID int64, actionType string, active bool, idempotencyKey string, version int64, occurredAt time.Time) *domaininteraction.AcceptedActionEvent {
	t.Helper()
	return mustAcceptedActionEventWithRecommendation(t, eventID, userID, videoID, actionType, active, idempotencyKey, "", version, occurredAt)
}

func mustAcceptedActionEventWithRecommendation(t *testing.T, eventID string, userID int64, videoID int64, actionType string, active bool, idempotencyKey string, recommendationRequestID string, version int64, occurredAt time.Time) *domaininteraction.AcceptedActionEvent {
	t.Helper()
	event, err := domaininteraction.NewAcceptedActionEventWithRecommendation(
		eventID,
		userID,
		videoID,
		actionType,
		active,
		idempotencyKey,
		recommendationRequestID,
		version,
		occurredAt,
	)
	if err != nil {
		t.Fatalf("new accepted action event: %v", err)
	}
	return event
}

func assertPostgreSQLLikeState(t *testing.T, db *gorm.DB, authorID int64, actorID int64, videoID int64, active bool, likeCount int, receivedLikeCount int) {
	t.Helper()
	var action infrainteraction.ActionModel
	if err := db.Where(
		"user_id = ? AND video_id = ? AND action_type = ?",
		actorID,
		videoID,
		domaininteraction.ActionTypeLike,
	).Take(&action).Error; err != nil {
		t.Fatalf("load durable like action: %v", err)
	}
	if (action.Status == domaininteraction.ActionStatusActive) != active {
		t.Fatalf("unexpected durable like state: %+v", action)
	}
	var stat infravideo.VideoStatModel
	if err := db.Where("video_id = ?", videoID).Take(&stat).Error; err != nil {
		t.Fatalf("load durable video stat: %v", err)
	}
	if stat.LikeCount != likeCount {
		t.Fatalf("unexpected durable like count: %+v", stat)
	}
	var authorStat infravideo.UserContentStatModel
	if err := db.Where("user_id = ?", authorID).Take(&authorStat).Error; err != nil {
		t.Fatalf("load durable author stat: %v", err)
	}
	if authorStat.ReceivedLikeCount != receivedLikeCount {
		t.Fatalf("unexpected durable received-like count: %+v", authorStat)
	}
}

func assertColumnType(t *testing.T, db *gorm.DB, table string, column string, expected string) {
	t.Helper()

	var dataType string
	err := db.Raw(`
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = ?
		  AND column_name = ?
	`, table, column).Scan(&dataType).Error
	if err != nil {
		t.Fatalf("read %s.%s type: %v", table, column, err)
	}
	if dataType != expected {
		t.Fatalf("expected %s.%s type %s, got %s", table, column, expected, dataType)
	}
}

func assertConcurrentCounters(t *testing.T, db *gorm.DB, videoID int64) {
	t.Helper()

	const writers = 8
	ctx := context.Background()
	repo := infrainteraction.New(db)
	run := func(active bool, keyPrefix string) {
		t.Helper()
		errs := make(chan error, writers)
		var wg sync.WaitGroup
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, _, _, err := repo.SetAction(
					ctx,
					int64(i+100),
					videoID,
					domaininteraction.ActionTypeLike,
					active,
					fmt.Sprintf("%s-%d", keyPrefix, i),
				)
				errs <- err
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Errorf("concurrent counter update: %v", err)
			}
		}
	}

	run(true, "Like")
	var stat infravideo.VideoStatModel
	if err := db.Where("video_id = ?", videoID).Take(&stat).Error; err != nil {
		t.Fatalf("load incremented video stat: %v", err)
	}
	if stat.LikeCount != writers {
		t.Fatalf("expected %d likes, got %d", writers, stat.LikeCount)
	}

	run(false, "Unlike")
	if err := db.Where("video_id = ?", videoID).Take(&stat).Error; err != nil {
		t.Fatalf("load decremented video stat: %v", err)
	}
	if stat.LikeCount != 0 {
		t.Fatalf("expected non-negative zero likes, got %d", stat.LikeCount)
	}
}

func assertStableTimelineCursor(t *testing.T, db *gorm.DB, authorID int64) {
	t.Helper()

	publishedAt := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)
	first := infravideo.VideoModel{
		AuthorID:    authorID,
		Title:       "cursor first",
		MediaURL:    "/uploads/cursor-first.mp4",
		CoverURL:    "/uploads/cursor-first.jpg",
		Status:      domainvideo.StatusPublished,
		PublishedAt: &publishedAt,
	}
	second := infravideo.VideoModel{
		AuthorID:    authorID,
		Title:       "cursor second",
		MediaURL:    "/uploads/cursor-second.mp4",
		CoverURL:    "/uploads/cursor-second.jpg",
		Status:      domainvideo.StatusPublished,
		PublishedAt: &publishedAt,
	}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create first cursor video: %v", err)
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatalf("create second cursor video: %v", err)
	}
	if err := db.Create(&[]infravideo.VideoStatModel{{VideoID: first.ID}, {VideoID: second.ID}}).Error; err != nil {
		t.Fatalf("create cursor video stats: %v", err)
	}

	repo := infrafeed.New(db)
	page, err := repo.ListTimelinePage(context.Background(), nil, 2)
	if err != nil {
		t.Fatalf("list first timeline page: %v", err)
	}
	if len(page) != 2 || page[0].VideoID != second.ID || page[1].VideoID != first.ID {
		t.Fatalf("unexpected stable timeline order: %+v", page)
	}

	cursor := &domainfeed.TimelineCursor{PublishedAt: page[1].PublishedAt, VideoID: page[1].VideoID}
	next, err := repo.ListTimelinePage(context.Background(), cursor, 2)
	if err != nil {
		t.Fatalf("list next timeline page: %v", err)
	}
	for _, item := range next {
		if item.VideoID == first.ID || item.VideoID == second.ID {
			t.Fatalf("timeline cursor repeated item: %+v", item)
		}
	}
}
