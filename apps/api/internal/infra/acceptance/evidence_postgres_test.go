package infraacceptance

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"testing"
	"time"

	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestEvidenceStoreAgainstIsolatedPostgresSchema(t *testing.T) {
	dsn := os.Getenv("FRUX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping acceptance PostgreSQL integration test")
	}
	base, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "acceptance_" + hex.EncodeToString(random[:])
	if _, err := base.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = base.Exec(`DROP SCHEMA "` + schema + `" CASCADE`) }()
	for _, statement := range []string{
		`CREATE TABLE review_case (id bigint, video_id bigint, version integer, review_version integer, status text)`,
		`CREATE TABLE multimodal_embedding_job (id bigint, video_id bigint, contract_key text, state text, attempts integer, failure_code text)`,
		`CREATE TABLE multimodal_vector_fact (video_id bigint, contract_key text, provider_alias text, model_alias text, revision_alias text, dimension integer, text_canonicalizer text, frame_sampling_policy text, image_preprocessing_policy text, fusion_policy text, embedding_json jsonb, vector_digest text, source_hash text)`,
		`CREATE TABLE multimodal_projection (video_id bigint, contract_key text, vector_digest text, source_hash text)`,
	} {
		if _, err := base.Exec(`SET search_path TO "` + schema + `"; ` + statement); err != nil {
			t.Fatal(err)
		}
	}
	profile, _ := multimodalprofile.Resolve(multimodalprofile.TongyiFlashSnapshotProfile)
	vector := make([]float64, profile.Dimension)
	vector[0] = 1
	encoded, _ := json.Marshal(vector)
	if _, err := base.Exec(`SET search_path TO "`+schema+`";
		INSERT INTO review_case VALUES (7,13,1,1,'pending_human');
		INSERT INTO multimodal_embedding_job VALUES (9,13,$1,'succeeded',1,'');
		INSERT INTO multimodal_vector_fact VALUES (13,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'digest','source');
		INSERT INTO multimodal_projection VALUES (13,$1,'digest','source')`,
		profile.Contract.Key(), profile.Contract.ProviderAlias, profile.Contract.ModelAlias,
		profile.Contract.RevisionAlias, profile.Contract.Dimension, profile.Contract.TextCanonicalizer,
		profile.Contract.FrameSamplingPolicy, profile.Contract.ImagePreprocessingPolicy,
		profile.Contract.FusionPolicy, encoded,
	); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	store, err := NewEvidenceStore(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	review, err := store.ReviewCase(ctx, 13)
	if err != nil || review.ID != 7 {
		t.Fatalf("review=%#v err=%v", review, err)
	}
	evidence, err := store.Multimodal(ctx, 13, profile.ID)
	if err != nil || evidence.JobID != 9 || evidence.VectorLength != profile.Dimension {
		t.Fatalf("evidence=%#v err=%v", evidence, err)
	}
}
