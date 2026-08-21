package infraacceptance

import (
	"context"
	"errors"
	"testing"
	"time"

	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

type sessionRunnerRuntimeStub struct {
	apiMetrics []MetricSnapshot
	apiCalls   int
}

func (*sessionRunnerRuntimeStub) CheckHealth(context.Context, string) error { return nil }
func (s *sessionRunnerRuntimeStub) CollectMetrics(_ context.Context, endpoint string) (MetricSnapshot, error) {
	if endpoint == "http://127.0.0.1:8099/metrics" {
		return MetricSnapshot{"frux_tongyi_provider_operations_total{operation=startup,result=success}": 1}, nil
	}
	index := s.apiCalls
	s.apiCalls++
	if index >= len(s.apiMetrics) {
		index = len(s.apiMetrics) - 1
	}
	return s.apiMetrics[index], nil
}

type sessionRunnerAPIStub struct {
	loginCalls    int
	viewCalls     int
	favoriteCalls int
	feedCalls     int
	failViewCall  int
	missingCursor bool
}

func (s *sessionRunnerAPIStub) Login(context.Context, bool, string, string) (string, error) {
	s.loginCalls++
	return "token", nil
}
func (*sessionRunnerAPIStub) Me(context.Context, string) (int64, error) { return 7, nil }
func (s *sessionRunnerAPIStub) CreateSessionViewEvent(context.Context, string, int64, string, string, string, string, int64, int, int, int, bool, time.Time) error {
	s.viewCalls++
	if s.failViewCall == s.viewCalls {
		return errors.New("view failure")
	}
	return nil
}
func (s *sessionRunnerAPIStub) SetSessionFavorite(context.Context, string, int64, string, string, bool) error {
	s.favoriteCalls++
	return nil
}
func (s *sessionRunnerAPIStub) SessionFeed(context.Context, string, string, string, string, int64, int64, int) (SessionFeedPage, error) {
	s.feedCalls++
	if s.feedCalls == 1 {
		cursor := "signed-cursor"
		if s.missingCursor {
			cursor = ""
		}
		return SessionFeedPage{RequestID: "request", VideoIDs: []int64{13}, NextCursor: cursor, HasMore: cursor != ""}, nil
	}
	return SessionFeedPage{RequestID: "request", VideoIDs: []int64{14}}, nil
}

type sessionRunnerStoreStub struct {
	disableCalls int
	deleteCalls  int
	failDisable  bool
}

func (*sessionRunnerStoreStub) Ping(context.Context) error { return nil }
func (*sessionRunnerStoreStub) VerifyFixtures(context.Context, applicationacceptance.SessionSemanticConfig) (applicationacceptance.ContractEvidence, applicationacceptance.SessionFixtureEvidence, error) {
	return applicationacceptance.ContractEvidence{Dimension: 768}, applicationacceptance.SessionFixtureEvidence{
		PositiveSeedVideoID: 11, NegativeSeedVideoID: 12, ExpectedTargetVideoID: 13, TargetSimilarity: 0.9,
	}, nil
}
func (*sessionRunnerStoreStub) FavoriteActive(context.Context, int64, int64) (bool, error) {
	return false, nil
}
func (*sessionRunnerStoreStub) InstallPolicy(context.Context, string, int64, string) (applicationacceptance.SessionPolicyEvidence, string, error) {
	return applicationacceptance.SessionPolicyEvidence{ID: 9, Version: 3, RolloutPercent: 1}, "request", nil
}
func (s *sessionRunnerStoreStub) DisablePolicy(context.Context, int64, int) error {
	s.disableCalls++
	if s.failDisable {
		return errors.New("disable failure")
	}
	return nil
}
func (s *sessionRunnerStoreStub) DeleteDisabledPolicy(context.Context, int64, int) error {
	s.deleteCalls++
	return nil
}
func (*sessionRunnerStoreStub) RequestLog(context.Context, int64, string, int64) (SessionRequestLogEvidence, error) {
	semantic, _ := domainrecommendation.NewSessionSemanticEvidence(domainrecommendation.SessionSemanticEvidence{
		BuilderVersion: domainrecommendation.SessionSemanticBuilderV1,
		ContractKey:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Result:         domainrecommendation.SessionSemanticResultSuccess,
		Confidence:     0.8, ConfidenceBand: domainrecommendation.SessionSemanticConfidenceHigh,
		EligibleCount: 2, PositiveCount: 3, NegativeCount: 1, CompatibleCount: 2,
		ExcludedCount: 2, InputDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	return SessionRequestLogEvidence{
		PolicyVersion: 3, Semantic: semantic, ExpectedTargetSeen: true,
		SemanticSimilarity: 0.7, SemanticUnderfill: 9,
	}, nil
}

func TestSessionRunnerCompletesRealWorkflowAndCleanup(t *testing.T) {
	runtime := sessionRunnerTestRuntime()
	api := &sessionRunnerAPIStub{}
	store := &sessionRunnerStoreStub{}
	runner, err := NewSessionRunner(sessionRunnerTestConfig(), runtime, api, store)
	if err != nil {
		t.Fatal(err)
	}
	report, err := runner.Run(context.Background(), applicationacceptance.NewSessionSemanticReport(
		"session-run", applicationacceptance.ModeExecution, time.Now(), true,
	))
	if err != nil || report.Result != applicationacceptance.ResultSuccess || report.ExternalModelCalls != 0 ||
		report.Policy == nil || !report.Policy.Disabled || !report.Policy.Deleted ||
		report.Request == nil || !report.Request.ExpectedTargetSeen || report.Request.SemanticSimilarity <= 0 ||
		report.Metrics == nil || report.Metrics.BuilderFirstPageDelta != 1 || report.Metrics.BuilderSnapshotDelta != 0 ||
		report.Cleanup == nil || report.Cleanup.Result != applicationacceptance.ResultSuccess ||
		api.loginCalls != 1 || api.viewCalls != 2 || api.favoriteCalls != 2 || api.feedCalls != 2 ||
		store.disableCalls != 1 || store.deleteCalls != 1 {
		t.Fatalf("report=%#v api=%#v store=%#v err=%v", report, api, store, err)
	}
	for _, stage := range report.Stages {
		if stage.Result != applicationacceptance.ResultSuccess {
			t.Fatalf("stage=%#v", stage)
		}
	}
}

func TestSessionRunnerValidationDoesNotMutate(t *testing.T) {
	runtime := sessionRunnerTestRuntime()
	api := &sessionRunnerAPIStub{}
	store := &sessionRunnerStoreStub{}
	runner, _ := NewSessionRunner(sessionRunnerTestConfig(), runtime, api, store)
	report, err := runner.Run(context.Background(), applicationacceptance.NewSessionSemanticReport(
		"session-run", applicationacceptance.ModeValidation, time.Now(), false,
	))
	if err != nil || report.Result != applicationacceptance.ResultSuccess || api.loginCalls != 0 ||
		api.viewCalls != 0 || api.favoriteCalls != 0 || api.feedCalls != 0 || store.disableCalls != 0 || store.deleteCalls != 0 {
		t.Fatalf("report=%#v api=%#v store=%#v err=%v", report, api, store, err)
	}
	for _, stage := range report.Stages {
		if stage.Name == applicationacceptance.SessionStagePreflight {
			if stage.Result != applicationacceptance.ResultSuccess {
				t.Fatalf("preflight=%#v", stage)
			}
		} else if stage.Result != applicationacceptance.ResultSkipped {
			t.Fatalf("stage=%#v", stage)
		}
	}
}

func TestSessionRunnerFailureAfterPolicyAlwaysDisablesIt(t *testing.T) {
	runtime := sessionRunnerTestRuntime()
	api := &sessionRunnerAPIStub{failViewCall: 2}
	store := &sessionRunnerStoreStub{}
	runner, _ := NewSessionRunner(sessionRunnerTestConfig(), runtime, api, store)
	report, err := runner.Run(context.Background(), applicationacceptance.NewSessionSemanticReport(
		"session-run", applicationacceptance.ModeExecution, time.Now(), false,
	))
	if err == nil || report.Failure != applicationacceptance.SessionFailureBehavior ||
		report.Policy == nil || !report.Policy.Disabled || store.disableCalls != 1 {
		t.Fatalf("report=%#v store=%#v err=%v", report, store, err)
	}
}

func TestSessionRunnerRejectsMissingCursorAndReportsDisableFailure(t *testing.T) {
	runtime := sessionRunnerTestRuntime()
	api := &sessionRunnerAPIStub{missingCursor: true}
	store := &sessionRunnerStoreStub{failDisable: true}
	runner, _ := NewSessionRunner(sessionRunnerTestConfig(), runtime, api, store)
	report, err := runner.Run(context.Background(), applicationacceptance.NewSessionSemanticReport(
		"session-run", applicationacceptance.ModeExecution, time.Now(), true,
	))
	if err == nil || report.Failure != applicationacceptance.SessionFailureFeed ||
		report.Cleanup == nil || report.Cleanup.Result != applicationacceptance.ResultFailed || store.disableCalls != 1 {
		t.Fatalf("report=%#v store=%#v err=%v", report, store, err)
	}
}

func sessionRunnerTestRuntime() *sessionRunnerRuntimeStub {
	baseline := MetricSnapshot{
		"frux_recommendation_session_semantic_operations_total{confidence_band=high,result=success,stage=builder}":  5,
		"frux_recommendation_session_semantic_operations_total{confidence_band=high,result=success,stage=provider}": 5,
		"frux_recommendation_snapshot_operations_total{result=write_success}":                                       2,
		"frux_recommendation_snapshot_operations_total{result=hit}":                                                 3,
	}
	afterFirst := MetricSnapshot{
		"frux_recommendation_session_semantic_operations_total{confidence_band=high,result=success,stage=builder}":  6,
		"frux_recommendation_session_semantic_operations_total{confidence_band=high,result=success,stage=provider}": 6,
		"frux_recommendation_snapshot_operations_total{result=write_success}":                                       3,
		"frux_recommendation_snapshot_operations_total{result=hit}":                                                 3,
	}
	afterSecond := MetricSnapshot{
		"frux_recommendation_session_semantic_operations_total{confidence_band=high,result=success,stage=builder}":  6,
		"frux_recommendation_session_semantic_operations_total{confidence_band=high,result=success,stage=provider}": 6,
		"frux_recommendation_snapshot_operations_total{result=write_success}":                                       3,
		"frux_recommendation_snapshot_operations_total{result=hit}":                                                 4,
	}
	return &sessionRunnerRuntimeStub{apiMetrics: []MetricSnapshot{baseline, afterFirst, afterSecond}}
}

func sessionRunnerTestConfig() applicationacceptance.SessionSemanticConfig {
	return applicationacceptance.SessionSemanticConfig{
		APIEndpoint: "http://127.0.0.1:8080", APIMetricsEndpoint: "http://127.0.0.1:8080/metrics",
		AdapterMetricsEndpoint: "http://127.0.0.1:8099/metrics",
		PostgresDSN:            "postgres://frux:secret@127.0.0.1:5432/frux?sslmode=disable",
		UserAccount:            "user", UserPassword: "secret",
		ExpectedProfile:     "tongyi-embedding-vision-flash-2026-03-06",
		PositiveSeedVideoID: 11, NegativeSeedVideoID: 12, ExpectedTargetVideoID: 13,
		PollInterval: time.Millisecond, StageTimeout: time.Second,
		HTTPTimeout: time.Second, MaxResponseBytes: 1 << 20,
	}
}
