package infraembedding_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	infraembedding "github.com/shiyudesu/frux/internal/infra/persistence/embedding"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresSemanticClaimsAreConcurrentAndExpiryFenced(t *testing.T) {
	db := openEmbeddingPostgres(t)
	repository := infraembedding.New(db)
	now := time.Now().UTC()
	for index := range 8 {
		job := infraembedding.SemanticJobModel{
			VideoID: int64(index + 1), Model: domainembedding.SemanticModelKey,
			TextHash: fmt.Sprintf("hash-%d", index), Title: "title",
			State: domainembedding.SemanticJobPending, AvailableAt: now,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&job).Error; err != nil {
			t.Fatal(err)
		}
	}

	var wait sync.WaitGroup
	claimed := make(chan *domainembedding.SemanticJob, 4)
	errs := make(chan error, 4)
	for index := range 4 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			jobs, err := repository.ClaimSemanticJobs(
				context.Background(), fmt.Sprintf("claim-%d", index),
				now, now.Add(time.Minute), 1,
			)
			if err != nil {
				errs <- err
				return
			}
			if len(jobs) != 1 {
				errs <- fmt.Errorf("claimed %d jobs", len(jobs))
				return
			}
			claimed <- jobs[0]
		}()
	}
	wait.Wait()
	close(claimed)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := map[int64]struct{}{}
	for job := range claimed {
		seen[job.VideoID] = struct{}{}
	}
	if len(seen) != 4 {
		t.Fatalf("unique claims=%d", len(seen))
	}

	first, err := repository.ClaimSemanticJobs(
		context.Background(), "expired-attempt", now, now.Add(time.Second), 1,
	)
	if err != nil || len(first) != 1 {
		t.Fatalf("first expiry claim=%v err=%v", first, err)
	}
	reclaimedAt := now.Add(2 * time.Second)
	second, err := repository.ClaimSemanticJobs(
		context.Background(), "replacement-attempt",
		reclaimedAt, reclaimedAt.Add(time.Minute), 1,
	)
	if err != nil || len(second) != 1 || second[0].VideoID != first[0].VideoID {
		t.Fatalf("reclaimed=%v err=%v", second, err)
	}
	vector := make([]float64, domainembedding.SemanticDimension)
	vector[0] = 1
	embedding := domainembedding.NewVideoEmbedding(
		first[0].VideoID, domainembedding.SemanticModelKey,
		vector, first[0].TextHash, "[1]",
	)
	if err := repository.CompleteSemanticJob(
		context.Background(), first[0], embedding, reclaimedAt,
	); !errors.Is(err, domainembedding.ErrSemanticJobLeaseLost) {
		t.Fatalf("stale completion error=%v", err)
	}
	if err := repository.RetrySemanticJob(
		context.Background(), first[0], reclaimedAt, "stale", false,
	); !errors.Is(err, domainembedding.ErrSemanticJobLeaseLost) {
		t.Fatalf("stale retry error=%v", err)
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
	defer admin.Close()
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
	if err := db.AutoMigrate(
		&infraembedding.VideoEmbeddingModel{},
		&infraembedding.SemanticJobModel{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}
