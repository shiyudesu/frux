package infraacceptance

import (
	"context"
	"errors"
	"testing"
	"time"

	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
)

type runnerRuntimeStub struct{ metrics int }

func (*runnerRuntimeStub) CheckHealth(context.Context, string) error { return nil }
func (s *runnerRuntimeStub) CollectMetrics(context.Context, string) (MetricSnapshot, error) {
	s.metrics++
	value := float64(1)
	if s.metrics > 3 {
		value = 2
	}
	return MetricSnapshot{
		"frux_tongyi_provider_operations_total{operation=video,result=success}":        value,
		"frux_tongyi_provider_operations_total{operation=startup,result=success}":      1,
		"frux_multimodal_provider_transport_total{operation=readiness,result=success}": 1,
	}, nil
}

type runnerAPIStub struct {
	deleted   []int64
	failLogin bool
	nextAsset int64
	nextVideo int64
}

func (s *runnerAPIStub) Login(context.Context, bool, string, string) (string, error) {
	if s.failLogin {
		return "", errors.New("login")
	}
	return "token", nil
}
func (s *runnerAPIStub) UploadFixture(context.Context, string, string, string, string) (CreatedAsset, error) {
	s.nextAsset++
	return CreatedAsset{ID: 20 + s.nextAsset}, nil
}
func (s *runnerAPIStub) CreateVideo(_ context.Context, _ string, media, cover int64, _, _, _ string) (CreatedVideo, error) {
	s.nextVideo++
	return CreatedVideo{ID: 10 + s.nextVideo, MediaAssetID: media, CoverAssetID: cover}, nil
}
func (*runnerAPIStub) ClaimReview(context.Context, string, int64, int) (ReviewLease, error) {
	return ReviewLease{LeaseToken: "lease", Version: 2}, nil
}
func (*runnerAPIStub) ApproveReview(context.Context, string, int64, int, ReviewLease, string) error {
	return nil
}
func (*runnerAPIStub) Similar(context.Context, int64) (SimilarResult, error) {
	return SimilarResult{Available: true, VideoIDs: []int64{12}}, nil
}
func (*runnerAPIStub) Hybrid(context.Context, string) ([]int64, error) { return []int64{12, 11}, nil }
func (s *runnerAPIStub) DeleteVideo(_ context.Context, _ string, id int64) error {
	s.deleted = append(s.deleted, id)
	return nil
}

type runnerEvidenceStub struct{ contract multimodalprofile.Profile }

func (*runnerEvidenceStub) Ping(context.Context) error { return nil }
func (*runnerEvidenceStub) ReviewCase(context.Context, int64) (ReviewCaseEvidence, error) {
	return ReviewCaseEvidence{ID: 7, Version: 1, ReviewVersion: 1, Status: "pending_human"}, nil
}
func (s *runnerEvidenceStub) Multimodal(_ context.Context, videoID int64, _ string) (DatabaseEvidence, error) {
	return DatabaseEvidence{JobID: videoID, JobState: "succeeded", Attempts: 1, FactPresent: true, ProjectionPresent: true,
		Contract: s.contract.Contract, VectorLength: s.contract.Dimension, VectorNorm: 1,
		FactDigest: "digest", ProjectionDigest: "digest", FactSourceHash: "source", ProjectionSourceHash: "source"}, nil
}

func TestRunnerCompletesAllStagesAndCleanup(t *testing.T) {
	profile, _ := multimodalprofile.Resolve(multimodalprofile.TongyiFlashSnapshotProfile)
	api := &runnerAPIStub{}
	runner, err := NewRunner(runnerTestConfig(profile.ID), &runnerRuntimeStub{}, api, &runnerEvidenceStub{contract: profile})
	if err != nil {
		t.Fatal(err)
	}
	report := applicationacceptance.NewReport("run", applicationacceptance.ModeExecution, time.Now(), true)
	report, err = runner.Run(context.Background(), report)
	if err != nil || report.Result != applicationacceptance.ResultSuccess || len(report.Fixtures) != 2 || len(report.Vectors) != 2 ||
		report.Retrieval == nil || len(api.deleted) != 2 || report.Cleanup.Result != applicationacceptance.ResultSuccess {
		t.Fatalf("report=%#v deleted=%v err=%v", report, api.deleted, err)
	}
	for _, stage := range report.Stages {
		if stage.Result != applicationacceptance.ResultSuccess {
			t.Fatalf("stage=%#v", stage)
		}
	}
}

func TestRunnerStopsAfterFailureAndSkipsLaterStages(t *testing.T) {
	profile, _ := multimodalprofile.Resolve(multimodalprofile.TongyiFlashSnapshotProfile)
	runner, _ := NewRunner(runnerTestConfig(profile.ID), &runnerRuntimeStub{}, &runnerAPIStub{failLogin: true}, &runnerEvidenceStub{contract: profile})
	report, err := runner.Run(context.Background(), applicationacceptance.NewReport("run", applicationacceptance.ModeExecution, time.Now(), false))
	if err == nil || report.Result != applicationacceptance.ResultFailed || report.Failure != applicationacceptance.FailureAuthentication {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	foundSkipped := false
	for _, stage := range report.Stages {
		if stage.Name == applicationacceptance.StageUploadFixtureA && stage.Result == applicationacceptance.ResultSkipped {
			foundSkipped = true
		}
	}
	if !foundSkipped {
		t.Fatalf("stages=%#v", report.Stages)
	}
}

type runnerPendingEvidenceStub struct{ runnerEvidenceStub }

func (s *runnerPendingEvidenceStub) Multimodal(context.Context, int64, string) (DatabaseEvidence, error) {
	return DatabaseEvidence{JobState: "pending"}, &EvidenceError{Code: EvidenceJobIncomplete}
}

func TestRunnerBoundsPendingEmbeddingStage(t *testing.T) {
	profile, _ := multimodalprofile.Resolve(multimodalprofile.TongyiFlashSnapshotProfile)
	config := runnerTestConfig(profile.ID)
	config.StageTimeout = 20 * time.Millisecond
	runner, _ := NewRunner(config, &runnerRuntimeStub{}, &runnerAPIStub{}, &runnerPendingEvidenceStub{runnerEvidenceStub{contract: profile}})
	report, err := runner.Run(context.Background(), applicationacceptance.NewReport("run", applicationacceptance.ModeExecution, time.Now(), false))
	if !errors.Is(err, context.DeadlineExceeded) || report.Failure != applicationacceptance.FailureTimeout {
		t.Fatalf("report=%#v err=%v", report, err)
	}
}

func runnerTestConfig(profile string) applicationacceptance.Config {
	return applicationacceptance.Config{APIEndpoint: "http://127.0.0.1:8080", AdapterEndpoint: "http://127.0.0.1:8099",
		APIMetricsEndpoint: "http://127.0.0.1:8080/metrics", WorkerMetricsEndpoint: "http://127.0.0.1:9091/metrics",
		UserAccount: "user", UserPassword: "secret", AdminAccount: "admin", AdminPassword: "secret",
		VideoFixturePath: "video.mp4", CoverFixturePath: "cover.jpg", ExpectedProfile: profile, Query: "雨夜城市",
		PollInterval: time.Millisecond, StageTimeout: time.Second}
}
