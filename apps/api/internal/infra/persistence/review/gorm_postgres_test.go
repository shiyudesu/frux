package infrareview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
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
		assertReviewCounts(t, db, 1, 1, 1)
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
		&CaseModel{}, &ResultModel{}, &SignalModel{}, &DecisionModel{}, &PolicyModel{},
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
