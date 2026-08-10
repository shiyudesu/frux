package infraembedding_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"slices"
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

func TestPostgresSemanticEmbeddingStorageContract(t *testing.T) {
	db := openEmbeddingPostgres(t)
	repository := infraembedding.New(db)
	if len(domainembedding.SemanticModelKey) > 64 {
		t.Fatalf("semantic model key length = %d", len(domainembedding.SemanticModelKey))
	}
	hashVector := make([]float64, 128)
	hashVector[0] = 1
	hashJSON, _ := json.Marshal(hashVector)
	semanticVector := make([]float64, domainembedding.SemanticDimension)
	value := 1 / math.Sqrt(float64(domainembedding.SemanticDimension))
	for index := range semanticVector {
		semanticVector[index] = value
	}
	semanticJSON, err := json.Marshal(semanticVector)
	if err != nil {
		t.Fatal(err)
	}
	hash := domainembedding.NewVideoEmbedding(
		1, domainembedding.HashNgramModel, hashVector, "hash-text", string(hashJSON),
	)
	semantic := domainembedding.NewVideoEmbedding(
		1, domainembedding.SemanticModelKey, semanticVector, "semantic-text",
		string(semanticJSON),
	)
	if err := repository.SaveVideoEmbedding(context.Background(), hash); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveVideoEmbedding(context.Background(), semantic); err != nil {
		t.Fatal(err)
	}
	var rows []infraembedding.VideoEmbeddingModel
	if err := db.Order("model ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("coexisting model rows = %d", len(rows))
	}
	before := rows[1].UpdatedAt
	if rows[0].Model == domainembedding.SemanticModelKey {
		before = rows[0].UpdatedAt
	}
	if err := repository.SaveVideoEmbedding(context.Background(), semantic); err != nil {
		t.Fatal(err)
	}
	var stored infraembedding.VideoEmbeddingModel
	if err := db.Where(
		"video_id = ? AND model = ?",
		1,
		domainembedding.SemanticModelKey,
	).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.UpdatedAt.Equal(before) {
		t.Fatalf("identical semantic fact churned updated_at: %v -> %v", before, stored.UpdatedAt)
	}
	var roundTrip []float64
	if err := json.Unmarshal([]byte(stored.EmbeddingJSON), &roundTrip); err != nil {
		t.Fatal(err)
	}
	norm := 0.0
	for _, component := range roundTrip {
		if math.IsNaN(component) || math.IsInf(component, 0) {
			t.Fatal("non-finite component round-tripped")
		}
		norm += component * component
	}
	if len(roundTrip) != domainembedding.SemanticDimension ||
		math.Abs(math.Sqrt(norm)-1) > 1e-12 {
		t.Fatalf("semantic JSON dimension=%d norm=%v", len(roundTrip), math.Sqrt(norm))
	}

	var column struct {
		DataType string
		Length   int `gorm:"column:character_maximum_length"`
	}
	if err := db.Raw(`
		SELECT data_type, character_maximum_length
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'video_embedding'
		  AND column_name = 'model'
	`).Scan(&column).Error; err != nil {
		t.Fatal(err)
	}
	if column.DataType != "character varying" || column.Length != 64 {
		t.Fatalf("model column = %+v", column)
	}
	var jsonType string
	if err := db.Raw(`
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'video_embedding'
		  AND column_name = 'embedding_json'
	`).Scan(&jsonType).Error; err != nil {
		t.Fatal(err)
	}
	if jsonType != "jsonb" {
		t.Fatalf("embedding storage type = %q", jsonType)
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
	if !slices.Equal(tables, []string{"semantic_embedding_job", "video_embedding"}) {
		t.Fatalf("unexpected embedding schema tables = %v", tables)
	}
	var indexDefinitions []string
	if err := db.Raw(`
		SELECT indexdef
		FROM pg_indexes
		WHERE schemaname = current_schema()
	`).Scan(&indexDefinitions).Error; err != nil {
		t.Fatal(err)
	}
	for _, definition := range indexDefinitions {
		lower := strings.ToLower(definition)
		if strings.Contains(lower, "vector") ||
			strings.Contains(lower, "ivfflat") ||
			strings.Contains(lower, "hnsw") {
			t.Fatalf("unexpected ANN/vector schema artifact: %s", definition)
		}
	}
}

func TestPostgresSemanticJobLifecycleAndBacklogOrdering(t *testing.T) {
	db := openEmbeddingPostgres(t)
	repository := infraembedding.New(db)
	now := time.Now().UTC()
	jobs := []infraembedding.SemanticJobModel{
		{
			VideoID: 1, Model: domainembedding.SemanticModelKey, TextHash: "one",
			Title: "one", State: domainembedding.SemanticJobPending,
			AvailableAt: now.Add(-2 * time.Minute), CreatedAt: now.Add(-time.Hour),
			UpdatedAt: now,
		},
		{
			VideoID: 2, Model: domainembedding.SemanticModelKey, TextHash: "two",
			Title: "two", State: domainembedding.SemanticJobRetry,
			AvailableAt: now.Add(-time.Minute), CreatedAt: now.Add(-30 * time.Minute),
			UpdatedAt: now,
		},
		{
			VideoID: 3, Model: domainembedding.SemanticModelKey, TextHash: "three",
			Title: "three", State: domainembedding.SemanticJobSuspended,
			AvailableAt: now, CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now,
		},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.ClaimSemanticJobs(
		context.Background(), "owner", now, now.Add(time.Minute), 3,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 3 || claimed[0].VideoID != 1 ||
		claimed[1].VideoID != 2 || claimed[2].VideoID != 3 {
		t.Fatalf("stable claims = %+v", claimed)
	}
	if err := repository.RetrySemanticJob(
		context.Background(), claimed[0], now.Add(5*time.Second), "raw-secret-class", false,
	); err != nil {
		t.Fatal(err)
	}
	var retried infraembedding.SemanticJobModel
	if err := db.Where(
		"video_id = ? AND model = ?",
		claimed[0].VideoID,
		claimed[0].Model,
	).Take(&retried).Error; err != nil {
		t.Fatal(err)
	}
	if retried.State != domainembedding.SemanticJobRetry ||
		len(retried.LastErrorClass) > 32 ||
		retried.LeaseOwner != "" ||
		retried.LeaseUntil != nil {
		t.Fatalf("retried job = %+v", retried)
	}
	completedAt := now.Add(-31 * 24 * time.Hour)
	completed := infraembedding.SemanticJobModel{
		VideoID: 4, Model: domainembedding.SemanticModelKey, TextHash: "four",
		Title: "four", State: domainembedding.SemanticJobCompleted,
		AvailableAt: completedAt, CompletedAt: &completedAt,
		CreatedAt: completedAt, UpdatedAt: completedAt,
	}
	if err := db.Create(&completed).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := repository.SemanticBacklog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index < len(rows); index++ {
		if rows[index-1].State > rows[index].State {
			t.Fatalf("backlog order = %+v", rows)
		}
	}
	deleted, err := repository.CleanupSemanticJobs(
		context.Background(),
		now.Add(-30*24*time.Hour),
		100,
	)
	if err != nil || deleted != 1 {
		t.Fatalf("cleanup deleted=%d err=%v", deleted, err)
	}
	reset := &domainembedding.SemanticJob{
		VideoID: 3, Model: domainembedding.SemanticModelKey,
		TextHash: "changed", Title: "changed",
		State: domainembedding.SemanticJobPending, AvailableAt: now,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.UpsertSemanticJob(context.Background(), reset); err != nil {
		t.Fatal(err)
	}
	var changed infraembedding.SemanticJobModel
	if err := db.Where("video_id = ?", 3).Take(&changed).Error; err != nil {
		t.Fatal(err)
	}
	if changed.TextHash != "changed" ||
		changed.State != domainembedding.SemanticJobPending ||
		changed.Attempts != 0 ||
		changed.CompletedAt != nil {
		t.Fatalf("changed-text reset = %+v", changed)
	}
}

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
	if err := db.AutoMigrate(
		&infraembedding.VideoEmbeddingModel{},
		&infraembedding.SemanticJobModel{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}
