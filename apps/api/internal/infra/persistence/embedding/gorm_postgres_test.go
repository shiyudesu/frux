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

func TestPostgresMultimodalConcurrentHandoffLeaseFencingAndSourceChange(t *testing.T) {
	db := openEmbeddingPostgres(t)
	migrateMultimodalEmbeddingTables(t, db)
	repository := infraembedding.New(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	contract := multimodalTestContract(t, "revision-1")
	sourceHash := domainembedding.MultimodalSourceHash([]byte("source-v1"))
	job, err := domainembedding.NewMultimodalEmbeddingJob(71, contract, sourceHash, 5, now)
	if err != nil {
		t.Fatal(err)
	}

	results := make([]*domainembedding.MultimodalEmbeddingJob, 2)
	createdFlags := make([]bool, 2)
	errs := make([]error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for index := range results {
		go func(index int) {
			defer wait.Done()
			results[index], createdFlags[index], _, errs[index] = repository.HandoffMultimodalJob(ctx, job)
		}(index)
	}
	wait.Wait()
	if errs[0] != nil || errs[1] != nil || results[0] == nil || results[1] == nil ||
		results[0].ID != results[1].ID || createdFlags[0] == createdFlags[1] {
		t.Fatalf("concurrent handoff results=%#v created=%v errors=%v", results, createdFlags, errs)
	}
	var jobCount int64
	if err := db.Model(&infraembedding.MultimodalEmbeddingJobModel{}).Count(&jobCount).Error; err != nil || jobCount != 1 {
		t.Fatalf("job count=%d err=%v", jobCount, err)
	}

	claimed, err := repository.ClaimMultimodalJobs(ctx, "worker-a", time.Minute, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("first claim=%#v err=%v", claimed, err)
	}
	oldToken := claimed[0].ClaimToken
	if alive, err := repository.HeartbeatMultimodalJob(ctx, claimed[0].ID, "wrong-token", time.Minute); err != nil || alive {
		t.Fatalf("wrong-token heartbeat alive=%v err=%v", alive, err)
	}
	if alive, err := repository.HeartbeatMultimodalJob(ctx, claimed[0].ID, oldToken, time.Minute); err != nil || !alive {
		t.Fatalf("owned heartbeat alive=%v err=%v", alive, err)
	}
	if err := db.Model(&infraembedding.MultimodalEmbeddingJobModel{}).Where("id = ?", claimed[0].ID).
		Update("lease_until", now.Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	reclaimed, err := repository.ClaimMultimodalJobs(ctx, "worker-b", time.Minute, 1)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].ClaimToken == oldToken {
		t.Fatalf("reclaim=%#v err=%v", reclaimed, err)
	}
	fact := multimodalTestFact(t, 71, contract, sourceHash, now)
	if completed, err := repository.CompleteMultimodalJob(ctx, reclaimed[0].ID, oldToken, fact); err != nil || completed {
		t.Fatalf("stale token completed=%v err=%v", completed, err)
	}
	if completed, err := repository.CompleteMultimodalJob(ctx, reclaimed[0].ID, reclaimed[0].ClaimToken, fact); err != nil || !completed {
		t.Fatalf("current token completed=%v err=%v", completed, err)
	}
	if completed, err := repository.CompleteMultimodalJob(ctx, reclaimed[0].ID, reclaimed[0].ClaimToken, fact); err != nil || completed {
		t.Fatalf("duplicate success completed=%v err=%v", completed, err)
	}

	sourceHashV2 := domainembedding.MultimodalSourceHash([]byte("source-v2"))
	changed, err := domainembedding.NewMultimodalEmbeddingJob(71, contract, sourceHashV2, 5, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	refreshed, created, sourceChanged, err := repository.HandoffMultimodalJob(ctx, changed)
	if err != nil || created || !sourceChanged || refreshed.SourceHash != sourceHashV2 {
		t.Fatalf("source refresh=%#v created=%v changed=%v err=%v", refreshed, created, sourceChanged, err)
	}
	claimed, err = repository.ClaimMultimodalJobs(ctx, "worker-c", time.Minute, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("changed claim=%#v err=%v", claimed, err)
	}
	if completed, err := repository.CompleteMultimodalJob(ctx, claimed[0].ID, claimed[0].ClaimToken, fact); !errors.Is(err, domainembedding.ErrMultimodalOperationConflict) || completed {
		t.Fatalf("stale source completed=%v err=%v", completed, err)
	}
	factV2 := multimodalTestFact(t, 71, contract, sourceHashV2, now.Add(time.Second))
	if completed, err := repository.CompleteMultimodalJob(ctx, claimed[0].ID, claimed[0].ClaimToken, factV2); err != nil || !completed {
		t.Fatalf("new source completed=%v err=%v", completed, err)
	}
}

func TestPostgresMultimodalTerminalRequeueContractIsolationAndProjection(t *testing.T) {
	db := openEmbeddingPostgres(t)
	migrateMultimodalEmbeddingTables(t, db)
	repository := infraembedding.New(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	contractV1 := multimodalTestContract(t, "revision-1")
	contractV2 := multimodalTestContract(t, "revision-2")
	sourceHash := domainembedding.MultimodalSourceHash([]byte("source"))

	for _, contract := range []domainembedding.MultimodalContractIdentity{contractV1, contractV2} {
		job, err := domainembedding.NewMultimodalEmbeddingJob(81, contract, sourceHash, 5, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, created, _, err := repository.HandoffMultimodalJob(ctx, job); err != nil || !created {
			t.Fatalf("handoff contract=%s created=%v err=%v", contract.Key(), created, err)
		}
	}
	claimed, err := repository.ClaimMultimodalJobs(ctx, "worker", time.Minute, 2)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claims=%#v err=%v", claimed, err)
	}
	for _, job := range claimed {
		fact := multimodalTestFact(t, job.VideoID, job.Contract, job.SourceHash, now)
		if completed, err := repository.CompleteMultimodalJob(ctx, job.ID, job.ClaimToken, fact); err != nil || !completed {
			t.Fatalf("complete contract=%s completed=%v err=%v", job.Contract.Key(), completed, err)
		}
	}
	for _, contract := range []domainembedding.MultimodalContractIdentity{contractV1, contractV2} {
		if fact, err := repository.FindMultimodalVectorFact(ctx, 81, contract); err != nil || fact.Identity.Contract.Key() != contract.Key() {
			t.Fatalf("isolated fact=%#v err=%v", fact, err)
		}
	}

	fact, err := repository.FindMultimodalVectorFact(ctx, 81, contractV1)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := domainembedding.NewMultimodalProjection(fact, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := repository.UpsertMultimodalProjection(ctx, projection); err != nil || !changed {
		t.Fatalf("projection insert changed=%v err=%v", changed, err)
	}
	if changed, err := repository.UpsertMultimodalProjection(ctx, projection); err != nil || changed {
		t.Fatalf("identical projection changed=%v err=%v", changed, err)
	}
	if deleted, err := repository.DeleteMultimodalProjectionIfStale(ctx, 81, contractV1.Key(), fact.Identity.SourceHash, fact.Identity.VectorDigest); err != nil || deleted {
		t.Fatalf("current projection deleted=%v err=%v", deleted, err)
	}
	if deleted, err := repository.DeleteMultimodalProjectionIfStale(ctx, 81, contractV1.Key(), domainembedding.MultimodalSourceHash([]byte("new")), fact.Identity.VectorDigest); err != nil || !deleted {
		t.Fatalf("stale projection deleted=%v err=%v", deleted, err)
	}

	terminalJob, err := domainembedding.NewMultimodalEmbeddingJob(82, contractV1, sourceHash, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	stored, _, _, err := repository.HandoffMultimodalJob(ctx, terminalJob)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err = repository.ClaimMultimodalJobs(ctx, "worker", time.Minute, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != stored.ID {
		t.Fatalf("terminal claim=%#v err=%v", claimed, err)
	}
	if terminal, err := repository.TerminalMultimodalJob(ctx, stored.ID, claimed[0].ClaimToken, domainembedding.MultimodalFailureInvalidInput); err != nil || !terminal {
		t.Fatalf("terminal=%v err=%v", terminal, err)
	}
	if replayed, err := repository.RequeueMultimodalJob(ctx, stored.ID, "operator-requeue-1"); err != nil || replayed {
		t.Fatalf("requeue replayed=%v err=%v", replayed, err)
	}
	if replayed, err := repository.RequeueMultimodalJob(ctx, stored.ID, "operator-requeue-1"); err != nil || !replayed {
		t.Fatalf("requeue replay replayed=%v err=%v", replayed, err)
	}
	var receiptCount int64
	if err := db.Model(&infraembedding.MultimodalJobOperationModel{}).Where("job_id = ?", stored.ID).Count(&receiptCount).Error; err != nil || receiptCount != 1 {
		t.Fatalf("receipt count=%d err=%v", receiptCount, err)
	}
	claimed, err = repository.ClaimMultimodalJobs(ctx, "worker-retry", time.Minute, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != stored.ID {
		t.Fatalf("requeued claim=%#v err=%v", claimed, err)
	}
	if retried, err := repository.RetryMultimodalJob(ctx, stored.ID, claimed[0].ClaimToken, domainembedding.MultimodalFailureAdmission, time.Second); err != nil || !retried {
		t.Fatalf("retry transition=%v err=%v", retried, err)
	}
	if err := db.Model(&infraembedding.MultimodalEmbeddingJobModel{}).Where("id = ?", stored.ID).
		Update("next_attempt_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err = repository.ClaimMultimodalJobs(ctx, "worker-terminal", time.Minute, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != stored.ID {
		t.Fatalf("retry claim=%#v err=%v", claimed, err)
	}
	if terminal, err := repository.TerminalMultimodalJob(ctx, stored.ID, claimed[0].ClaimToken, domainembedding.MultimodalFailureProviderTerminal); err != nil || !terminal {
		t.Fatalf("second terminal=%v err=%v", terminal, err)
	}
	if err := db.Model(&infraembedding.MultimodalEmbeddingJobModel{}).Where("id = ?", stored.ID).
		Update("completed_at", now.Add(-2*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if deleted, err := repository.DeleteCompletedMultimodalJobsBefore(ctx, now.Add(-time.Hour), 10); err != nil || deleted != 1 {
		t.Fatalf("cleanup deleted=%d err=%v", deleted, err)
	}
	if err := db.Model(&infraembedding.MultimodalJobOperationModel{}).Where("job_id = ?", stored.ID).Count(&receiptCount).Error; err != nil || receiptCount != 0 {
		t.Fatalf("cleanup receipt count=%d err=%v", receiptCount, err)
	}
}

func TestPostgresMultimodalMigrationCreatesNoJobsOrANNIndexes(t *testing.T) {
	db := openEmbeddingPostgres(t)
	migrateMultimodalEmbeddingTables(t, db)
	var jobs int64
	if err := db.Model(&infraembedding.MultimodalEmbeddingJobModel{}).Count(&jobs).Error; err != nil || jobs != 0 {
		t.Fatalf("migration created historical jobs=%d err=%v", jobs, err)
	}
	var annIndexes int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND (indexdef ILIKE '%hnsw%' OR indexdef ILIKE '%ivfflat%')
	`).Scan(&annIndexes).Error; err != nil || annIndexes != 0 {
		t.Fatalf("migration created ANN indexes=%d err=%v", annIndexes, err)
	}
	var boundedIndexes int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND indexname IN (
		    'uk_multimodal_job_video_contract',
		    'idx_multimodal_job_claim',
		    'idx_multimodal_job_lease',
		    'idx_multimodal_job_terminal',
		    'idx_multimodal_job_source',
		    'uk_multimodal_fact_video_contract',
		    'idx_multimodal_fact_contract_updated',
		    'idx_multimodal_fact_contract_source',
		    'idx_multimodal_projection_contract_published',
		    'idx_multimodal_projection_contract_source'
		  )
	`).Scan(&boundedIndexes).Error; err != nil || boundedIndexes != 10 {
		t.Fatalf("bounded index count=%d err=%v", boundedIndexes, err)
	}
}

func migrateMultimodalEmbeddingTables(t testing.TB, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(
		&infraembedding.MultimodalEmbeddingJobModel{},
		&infraembedding.MultimodalJobOperationModel{},
		&infraembedding.MultimodalVectorFactModel{},
		&infraembedding.MultimodalProjectionModel{},
	); err != nil {
		t.Fatal(err)
	}
}

func multimodalTestContract(t testing.TB, revision string) domainembedding.MultimodalContractIdentity {
	t.Helper()
	contract, err := domainembedding.NewMultimodalContractIdentity(
		"provider", "model", revision, domainembedding.MinMultimodalDimension,
		domainembedding.MultimodalTextCanonicalizerV1,
		domainembedding.MultimodalFrameSamplingPolicyV1,
		domainembedding.MultimodalImagePreprocessingV1,
		domainembedding.MultimodalFusionPolicyV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func multimodalTestFact(
	t testing.TB,
	videoID int64,
	contract domainembedding.MultimodalContractIdentity,
	sourceHash string,
	now time.Time,
) *domainembedding.MultimodalVectorFact {
	t.Helper()
	values := make([]float64, contract.Dimension)
	values[0] = 1
	fact, err := domainembedding.NewMultimodalVectorFact(videoID, &domainembedding.MultimodalVector{
		Identity: domainembedding.MultimodalVectorIdentity{
			Contract: contract, SourceHash: sourceHash,
			VectorDigest: domainembedding.MultimodalVectorDigest(values),
		},
		Values: values,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return fact
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
