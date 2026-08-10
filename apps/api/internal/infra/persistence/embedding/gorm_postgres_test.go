package infraembedding_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	infraembedding "github.com/shiyudesu/frux/internal/infra/persistence/embedding"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresHashEmbeddingConditionalPersistenceAndParity(t *testing.T) {
	db := openEmbeddingPostgres(t)
	repository := infraembedding.New(db)
	ctx := context.Background()

	first := domainembedding.NewVideoEmbedding(
		7,
		domainembedding.HashNgramModel,
		[]float64{0.25, 0.75},
		domainembedding.TextHash("title\ndescription"),
		`[0.25,0.75]`,
	)
	if err := repository.SaveVideoEmbedding(ctx, first); err != nil {
		t.Fatal(err)
	}
	var initial infraembedding.VideoEmbeddingModel
	if err := db.Where(
		"video_id = ? AND model = ?", first.VideoID, first.Model,
	).Take(&initial).Error; err != nil {
		t.Fatal(err)
	}

	if err := repository.SaveVideoEmbedding(ctx, first); err != nil {
		t.Fatal(err)
	}
	var duplicate infraembedding.VideoEmbeddingModel
	if err := db.Where(
		"video_id = ? AND model = ?", first.VideoID, first.Model,
	).Take(&duplicate).Error; err != nil {
		t.Fatal(err)
	}
	if !duplicate.UpdatedAt.Equal(initial.UpdatedAt) {
		t.Fatalf(
			"identical hash fact churned updated_at: initial=%s duplicate=%s",
			initial.UpdatedAt, duplicate.UpdatedAt,
		)
	}

	present, matches, err := repository.PublicationIntakeParity(
		ctx, first.VideoID, " title ", " description ",
	)
	if err != nil || !present || !matches {
		t.Fatalf("matching parity present=%v matches=%v err=%v", present, matches, err)
	}

	changed := domainembedding.NewVideoEmbedding(
		first.VideoID,
		domainembedding.HashNgramModel,
		[]float64{0.5, 0.5},
		domainembedding.TextHash("changed"),
		`[0.5,0.5]`,
	)
	if err := repository.SaveVideoEmbedding(ctx, changed); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.FindVideoEmbedding(
		ctx, first.VideoID, domainembedding.HashNgramModel,
	)
	if err != nil || stored.TextHash != changed.TextHash {
		t.Fatalf("changed hash fact=%+v err=%v", stored, err)
	}
	present, matches, err = repository.PublicationIntakeParity(
		ctx, first.VideoID, "title", "description",
	)
	if err != nil || !present || matches {
		t.Fatalf("mismatched parity present=%v matches=%v err=%v", present, matches, err)
	}
	present, matches, err = repository.PublicationIntakeParity(
		ctx, first.VideoID+1, "title\x00", "description",
	)
	if err != nil || !present || !matches {
		t.Fatalf("terminal parity present=%v matches=%v err=%v", present, matches, err)
	}

	var tables []string
	if err := db.Raw(`
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = current_schema()
		ORDER BY table_name
	`).Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0] != "video_embedding" {
		t.Fatalf("embedding tables=%v", tables)
	}
}

func openEmbeddingPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	schema := fmt.Sprintf("embedding_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`)
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
	if err := db.AutoMigrate(&infraembedding.VideoEmbeddingModel{}); err != nil {
		t.Fatal(err)
	}
	return db
}
