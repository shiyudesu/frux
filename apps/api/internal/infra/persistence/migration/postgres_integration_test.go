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
	"testing"
	"time"

	domainaccount "GCFeed/internal/domain/account"
	domainembedding "GCFeed/internal/domain/embedding"
	domainexposure "GCFeed/internal/domain/exposure"
	domainfeed "GCFeed/internal/domain/feed"
	domaininteraction "GCFeed/internal/domain/interaction"
	domainmessage "GCFeed/internal/domain/message"
	domainvideo "GCFeed/internal/domain/video"
	infraaccount "GCFeed/internal/infra/persistence/account"
	infraembedding "GCFeed/internal/infra/persistence/embedding"
	infraexposure "GCFeed/internal/infra/persistence/exposure"
	infrafeed "GCFeed/internal/infra/persistence/feed"
	infrainteraction "GCFeed/internal/infra/persistence/interaction"
	inframessage "GCFeed/internal/infra/persistence/message"
	infrarelation "GCFeed/internal/infra/persistence/relation"
	infravideo "GCFeed/internal/infra/persistence/video"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const postgresTestDSNEnv = "GCFEED_POSTGRES_TEST_DSN"

type postgresFixture struct {
	admin  *sql.DB
	dsn    string
	schema string
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

	schema := fmt.Sprintf("gcfeed_test_%d", time.Now().UnixNano())
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
	t.Helper()

	connConfig, err := pgx.ParseConfig(postgresDSNWithSchema(f.dsn, f.schema))
	if err != nil {
		t.Fatalf("parse schema PostgreSQL connection: %v", err)
	}
	sqlDB := stdlib.OpenDB(*connConfig, stdlib.OptionAfterConnect(configureTestUTCCodecs))
	sqlDB.SetMaxIdleConns(4)
	sqlDB.SetMaxOpenConns(20)
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
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}

	requiredTables := []string{
		"account",
		"video_embedding",
		"video",
		"video_stat",
		"feed_inbox",
		"video_view_events",
		"exposures",
		"interaction_action",
		"interaction_comment",
		"user_message",
		"playback_config",
		"playback_qos_log",
		"user_follow",
		"user_relation_stat",
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
		{&infrafeed.InboxModel{}, "uk_feed_inbox_user_video"},
		{&infraexposure.ExposureModel{}, "uk_exposures_user_video"},
		{&inframessage.MessageModel{}, "uk_user_message_user_event"},
	}
	for _, index := range requiredIndexes {
		if !db.Migrator().HasIndex(index.model, index.name) {
			t.Errorf("missing index %s", index.name)
		}
	}

	assertColumnType(t, db, "account", "status", "smallint")
	assertColumnType(t, db, "video", "status", "smallint")
	assertColumnType(t, db, "video_embedding", "embedding_json", "jsonb")

	var timeZone string
	if err := db.Raw("SHOW TIME ZONE").Scan(&timeZone).Error; err != nil {
		t.Fatalf("show time zone: %v", err)
	}
	if !strings.EqualFold(timeZone, "UTC") {
		t.Fatalf("expected UTC time zone, got %q", timeZone)
	}

	publishedAt := time.Now().UTC()
	video := infravideo.VideoModel{
		AuthorID:    1,
		Title:       "migration backfill",
		MediaURL:    "/uploads/migration.mp4",
		CoverURL:    "/uploads/migration.jpg",
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
	savedRepeated, created, err := messageRepo.Create(ctx, repeatedMessage, "Request-Key")
	if err != nil || created || savedRepeated.ID != savedFirst.ID {
		t.Fatalf("repeat exact message: created=%v saved=%+v err=%v", created, savedRepeated, err)
	}
	caseVariantMessage, _ := domainmessage.New(alice.ID, domainmessage.TypeSystem, "Title", "Content", "event-key")
	savedVariant, created, err := messageRepo.Create(ctx, caseVariantMessage, "request-key")
	if err != nil || !created || savedVariant.ID == savedFirst.ID {
		t.Fatalf("create case-variant opaque keys: created=%v saved=%+v err=%v", created, savedVariant, err)
	}

	exposureRepo := infraexposure.New(db)
	firstEvent, err := domainexposure.NewViewEvent(alice.ID, video.ID, "timeline", "Exposure-Key", domainexposure.EventTypeExposed, 0, false)
	if err != nil {
		t.Fatalf("new first exposure: %v", err)
	}
	savedEvent, firstExposure, err := exposureRepo.SaveViewEvent(ctx, firstEvent)
	if err != nil {
		t.Fatalf("save first exposure: %v", err)
	}
	secondEvent, err := domainexposure.NewViewEvent(alice.ID, video.ID, "hot", "exposure-key", domainexposure.EventTypeExposed, 0, false)
	if err != nil {
		t.Fatalf("new second exposure: %v", err)
	}
	savedSecondEvent, aggregated, err := exposureRepo.SaveViewEvent(ctx, secondEvent)
	if err != nil {
		t.Fatalf("save second exposure: %v", err)
	}
	if firstExposure.ExposureCount != 1 || aggregated.ExposureCount != 2 || aggregated.LastScene != "hot" {
		t.Fatalf("unexpected exposure aggregation: first=%+v aggregated=%+v", firstExposure, aggregated)
	}
	if savedEvent.ID == 0 || savedSecondEvent.ID == 0 || savedEvent.ID == savedSecondEvent.ID {
		t.Fatalf("expected distinct exposure event IDs: %d and %d", savedEvent.ID, savedSecondEvent.ID)
	}
	if delta := aggregated.LastExposedAt.Sub(savedSecondEvent.CreatedAt); delta < -time.Millisecond || delta > time.Millisecond {
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
