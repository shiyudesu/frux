package infraacceptance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
)

type sessionAcceptanceAPI interface {
	Login(context.Context, bool, string, string) (string, error)
	Me(context.Context, string) (int64, error)
	CreateSessionViewEvent(context.Context, string, int64, string, string, string, string, int64, int, int, int, bool, time.Time) error
	SetSessionFavorite(context.Context, string, int64, string, string, bool) error
	SessionFeed(context.Context, string, string, string, string, int64, int64, int) (SessionFeedPage, error)
}

type sessionAcceptanceStore interface {
	Ping(context.Context) error
	VerifyFixtures(context.Context, applicationacceptance.SessionSemanticConfig) (applicationacceptance.ContractEvidence, applicationacceptance.SessionFixtureEvidence, error)
	FavoriteActive(context.Context, int64, int64) (bool, error)
	InstallPolicy(context.Context, string, int64, string) (applicationacceptance.SessionPolicyEvidence, string, error)
	DisablePolicy(context.Context, int64, int) error
	DeleteDisabledPolicy(context.Context, int64, int) error
	RequestLog(context.Context, int64, string, int64) (SessionRequestLogEvidence, error)
}

type sessionAcceptanceRuntime interface {
	CheckHealth(context.Context, string) error
	CollectMetrics(context.Context, string) (MetricSnapshot, error)
}

type SessionRunner struct {
	config  applicationacceptance.SessionSemanticConfig
	runtime sessionAcceptanceRuntime
	api     sessionAcceptanceAPI
	store   sessionAcceptanceStore
	now     func() time.Time
}

type sessionRunState struct {
	token           string
	userID          int64
	requestID       string
	sessionID       string
	policy          applicationacceptance.SessionPolicyEvidence
	policyCreated   bool
	policyDisabled  bool
	favoriteCreated bool
	firstPage       SessionFeedPage
	requestLog      SessionRequestLogEvidence
	baselineAPI     MetricSnapshot
	afterFirstAPI   MetricSnapshot
	afterSecondAPI  MetricSnapshot
	baselineAdapter MetricSnapshot
	afterAdapter    MetricSnapshot
}

func NewSessionRunner(
	config applicationacceptance.SessionSemanticConfig,
	runtime sessionAcceptanceRuntime,
	api sessionAcceptanceAPI,
	store sessionAcceptanceStore,
) (*SessionRunner, error) {
	if runtime == nil || api == nil || store == nil {
		return nil, ErrInvalidAcceptanceConfig
	}
	return &SessionRunner{
		config: config, runtime: runtime, api: api, store: store,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (r *SessionRunner) Run(
	ctx context.Context,
	report applicationacceptance.SessionSemanticReport,
) (applicationacceptance.SessionSemanticReport, error) {
	state := &sessionRunState{sessionID: report.RunID + "-session"}
	steps := []struct {
		name applicationacceptance.SessionSemanticStageName
		code applicationacceptance.SessionSemanticFailureCode
		fn   func(context.Context) error
	}{
		{applicationacceptance.SessionStagePreflight, applicationacceptance.SessionFailurePrerequisite, func(stage context.Context) error {
			return r.preflight(stage, state, &report)
		}},
	}
	if report.Mode == applicationacceptance.ModeExecution {
		steps = append(steps,
			struct {
				name applicationacceptance.SessionSemanticStageName
				code applicationacceptance.SessionSemanticFailureCode
				fn   func(context.Context) error
			}{applicationacceptance.SessionStageLogin, applicationacceptance.SessionFailureAuthentication, func(stage context.Context) error { return r.login(stage, state, &report) }},
			struct {
				name applicationacceptance.SessionSemanticStageName
				code applicationacceptance.SessionSemanticFailureCode
				fn   func(context.Context) error
			}{applicationacceptance.SessionStagePolicy, applicationacceptance.SessionFailurePolicy, func(stage context.Context) error { return r.installPolicy(stage, state, &report) }},
			struct {
				name applicationacceptance.SessionSemanticStageName
				code applicationacceptance.SessionSemanticFailureCode
				fn   func(context.Context) error
			}{applicationacceptance.SessionStagePositiveFacts, applicationacceptance.SessionFailureBehavior, func(stage context.Context) error { return r.positiveFacts(stage, state) }},
			struct {
				name applicationacceptance.SessionSemanticStageName
				code applicationacceptance.SessionSemanticFailureCode
				fn   func(context.Context) error
			}{applicationacceptance.SessionStageNegativeFacts, applicationacceptance.SessionFailureBehavior, func(stage context.Context) error { return r.negativeFacts(stage, state) }},
			struct {
				name applicationacceptance.SessionSemanticStageName
				code applicationacceptance.SessionSemanticFailureCode
				fn   func(context.Context) error
			}{applicationacceptance.SessionStageFirstPage, applicationacceptance.SessionFailureFeed, func(stage context.Context) error { return r.firstPage(stage, state, &report) }},
			struct {
				name applicationacceptance.SessionSemanticStageName
				code applicationacceptance.SessionSemanticFailureCode
				fn   func(context.Context) error
			}{applicationacceptance.SessionStageRequestEvidence, applicationacceptance.SessionFailureEvidence, func(stage context.Context) error { return r.requestEvidence(stage, state, &report) }},
			struct {
				name applicationacceptance.SessionSemanticStageName
				code applicationacceptance.SessionSemanticFailureCode
				fn   func(context.Context) error
			}{applicationacceptance.SessionStageSnapshotPage, applicationacceptance.SessionFailureSnapshot, func(stage context.Context) error { return r.snapshotPage(stage, state, &report) }},
			struct {
				name applicationacceptance.SessionSemanticStageName
				code applicationacceptance.SessionSemanticFailureCode
				fn   func(context.Context) error
			}{applicationacceptance.SessionStageMetrics, applicationacceptance.SessionFailureMetrics, func(stage context.Context) error { return r.metrics(stage, state, &report) }},
			struct {
				name applicationacceptance.SessionSemanticStageName
				code applicationacceptance.SessionSemanticFailureCode
				fn   func(context.Context) error
			}{applicationacceptance.SessionStageDisablePolicy, applicationacceptance.SessionFailureCleanup, func(stage context.Context) error { return r.disablePolicy(stage, state, &report) }},
		)
	}
	for _, step := range steps {
		if err := r.stage(ctx, &report, step.name, step.code, step.fn); err != nil {
			r.recoverPolicy(state, &report)
			r.finishFailure(&report, step.name, err)
			return report, err
		}
	}
	if report.Mode == applicationacceptance.ModeValidation {
		r.finishValidation(&report)
		return report, nil
	}
	if report.Cleanup != nil && report.Cleanup.Requested {
		if err := r.stage(ctx, &report, applicationacceptance.SessionStageCleanup, applicationacceptance.SessionFailureCleanup, func(stage context.Context) error {
			return r.cleanup(stage, state, &report)
		}); err != nil {
			r.finishFailure(&report, applicationacceptance.SessionStageCleanup, err)
			return report, err
		}
	} else if report.Cleanup != nil {
		report.Cleanup.Result = applicationacceptance.ResultSkipped
		report.Cleanup.PolicyDisabled = state.policyDisabled
	}
	report.Result = applicationacceptance.ResultSuccess
	report.FinishedAt = r.now()
	return report, nil
}

func (r *SessionRunner) stage(
	parent context.Context,
	report *applicationacceptance.SessionSemanticReport,
	name applicationacceptance.SessionSemanticStageName,
	code applicationacceptance.SessionSemanticFailureCode,
	fn func(context.Context) error,
) error {
	started := r.now()
	ctx, cancel := context.WithTimeout(parent, r.config.StageTimeout)
	defer cancel()
	err := fn(ctx)
	for index := range report.Stages {
		if report.Stages[index].Name != name {
			continue
		}
		report.Stages[index].DurationMS = max(r.now().Sub(started).Milliseconds(), 0)
		if err == nil {
			report.Stages[index].Result = applicationacceptance.ResultSuccess
		} else {
			report.Stages[index].Result = applicationacceptance.ResultFailed
			report.Stages[index].Failure = sessionFailureForContext(err, code)
		}
		break
	}
	return err
}

func (r *SessionRunner) preflight(
	ctx context.Context,
	state *sessionRunState,
	report *applicationacceptance.SessionSemanticReport,
) error {
	checks := []struct {
		name string
		fn   func() error
	}{
		{"api_health", func() error { return r.runtime.CheckHealth(ctx, r.config.APIEndpoint) }},
		{"postgres_read", func() error { return r.store.Ping(ctx) }},
	}
	for _, check := range checks {
		result := applicationacceptance.ResultSuccess
		if err := check.fn(); err != nil {
			result = applicationacceptance.ResultFailed
			report.Prerequisites = append(report.Prerequisites, applicationacceptance.PrerequisiteResult{Name: check.name, Result: result})
			return err
		}
		report.Prerequisites = append(report.Prerequisites, applicationacceptance.PrerequisiteResult{Name: check.name, Result: result})
	}
	contract, fixtures, err := r.store.VerifyFixtures(ctx, r.config)
	report.Prerequisites = append(report.Prerequisites, applicationacceptance.PrerequisiteResult{
		Name: "active_contract_fixtures", Result: mapSessionPrerequisiteResult(err),
	})
	if err != nil {
		return err
	}
	report.Contract = &contract
	report.Fixtures = &fixtures
	state.baselineAPI, err = r.runtime.CollectMetrics(ctx, r.config.APIMetricsEndpoint)
	if err != nil {
		return err
	}
	if r.config.AdapterMetricsEndpoint != "" {
		state.baselineAdapter, err = r.runtime.CollectMetrics(ctx, r.config.AdapterMetricsEndpoint)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *SessionRunner) login(ctx context.Context, state *sessionRunState, report *applicationacceptance.SessionSemanticReport) error {
	var err error
	state.token, err = r.api.Login(ctx, false, r.config.UserAccount, r.config.UserPassword)
	if err != nil {
		return err
	}
	state.userID, err = r.api.Me(ctx, state.token)
	if err != nil {
		return err
	}
	active, err := r.store.FavoriteActive(ctx, state.userID, r.config.PositiveSeedVideoID)
	report.Prerequisites = append(report.Prerequisites, applicationacceptance.PrerequisiteResult{
		Name: "positive_seed_not_favorited", Result: mapSessionPrerequisiteResult(func() error {
			if err != nil {
				return err
			}
			if active {
				return errors.New("positive fixture is already favorited")
			}
			return nil
		}()),
	})
	if err != nil {
		return err
	}
	if active {
		return errors.New("positive fixture is already favorited")
	}
	if report.Request == nil {
		report.Request = &applicationacceptance.SessionRequestEvidence{}
	}
	report.Request.UserID = state.userID
	return nil
}

func (r *SessionRunner) installPolicy(ctx context.Context, state *sessionRunState, report *applicationacceptance.SessionSemanticReport) error {
	profile, err := multimodalprofile.Resolve(r.config.ExpectedProfile)
	if err != nil {
		return err
	}
	state.policy, state.requestID, err = r.store.InstallPolicy(ctx, report.RunID, state.userID, profile.Contract.Key())
	if err != nil {
		return err
	}
	state.policyCreated = true
	report.Policy = &state.policy
	if report.Request == nil {
		report.Request = &applicationacceptance.SessionRequestEvidence{}
	}
	report.Request.UserID = state.userID
	report.Request.RequestID = state.requestID
	report.Request.SessionID = state.sessionID
	return nil
}

func (r *SessionRunner) positiveFacts(ctx context.Context, state *sessionRunState) error {
	now := r.now()
	if err := r.api.CreateSessionViewEvent(
		ctx, state.token, r.config.PositiveSeedVideoID, state.requestID,
		"complete", reportIdentity(state.requestID, "positive-complete"), reportIdentity(state.requestID, "positive-playback"),
		1, 100_000, 90_000, 100_000, true, now,
	); err != nil {
		return err
	}
	if err := r.api.SetSessionFavorite(
		ctx, state.token, r.config.PositiveSeedVideoID, state.requestID,
		reportIdentity(state.requestID, "favorite"), true,
	); err != nil {
		return err
	}
	state.favoriteCreated = true
	return nil
}

func (r *SessionRunner) negativeFacts(ctx context.Context, state *sessionRunState) error {
	return r.api.CreateSessionViewEvent(
		ctx, state.token, r.config.NegativeSeedVideoID, state.requestID,
		"skip", reportIdentity(state.requestID, "negative-skip"), reportIdentity(state.requestID, "negative-playback"),
		1, 10_000, 5_000, 100_000, false, r.now(),
	)
}

func (r *SessionRunner) firstPage(ctx context.Context, state *sessionRunState, report *applicationacceptance.SessionSemanticReport) error {
	page, err := r.api.SessionFeed(
		ctx, state.token, state.requestID, state.sessionID, "",
		r.config.PositiveSeedVideoID, r.config.NegativeSeedVideoID, 1,
	)
	if err != nil {
		return err
	}
	if len(page.VideoIDs) == 0 || !page.HasMore || strings.TrimSpace(page.NextCursor) == "" {
		return errors.New("snapshot cursor prerequisite missing")
	}
	state.firstPage = page
	state.afterFirstAPI, err = r.runtime.CollectMetrics(ctx, r.config.APIMetricsEndpoint)
	if err != nil {
		return err
	}
	if report.Request == nil {
		report.Request = &applicationacceptance.SessionRequestEvidence{}
	}
	report.Request.FirstPageVideoIDs = append([]int64(nil), page.VideoIDs...)
	return nil
}

func (r *SessionRunner) requestEvidence(ctx context.Context, state *sessionRunState, report *applicationacceptance.SessionSemanticReport) error {
	var evidence SessionRequestLogEvidence
	err := r.poll(ctx, func() (bool, error) {
		value, err := r.store.RequestLog(ctx, state.userID, state.requestID, r.config.ExpectedTargetVideoID)
		if err != nil {
			var unavailable *EvidenceError
			if errors.As(err, &unavailable) && unavailable.Code == EvidenceUnavailable {
				return false, nil
			}
			return false, err
		}
		evidence = value
		return true, nil
	})
	if err != nil {
		return err
	}
	if evidence.PolicyVersion != state.policy.Version || evidence.Semantic == nil {
		return errors.New("request evidence policy mismatch")
	}
	state.requestLog = evidence
	report.Request.PolicyVersion = evidence.PolicyVersion
	report.Request.Confidence = evidence.Semantic.Confidence
	report.Request.ConfidenceBand = string(evidence.Semantic.ConfidenceBand)
	report.Request.PositiveCount = evidence.Semantic.PositiveCount
	report.Request.NegativeCount = evidence.Semantic.NegativeCount
	report.Request.CompatibleCount = evidence.Semantic.CompatibleCount
	report.Request.ExpectedTargetSeen = evidence.ExpectedTargetSeen
	report.Request.SemanticSimilarity = evidence.SemanticSimilarity
	return nil
}

func (r *SessionRunner) snapshotPage(ctx context.Context, state *sessionRunState, report *applicationacceptance.SessionSemanticReport) error {
	page, err := r.api.SessionFeed(
		ctx, state.token, state.requestID, state.sessionID, state.firstPage.NextCursor,
		r.config.PositiveSeedVideoID, r.config.NegativeSeedVideoID, 1,
	)
	if err != nil {
		return err
	}
	state.afterSecondAPI, err = r.runtime.CollectMetrics(ctx, r.config.APIMetricsEndpoint)
	if err != nil {
		return err
	}
	report.Request.SecondPageVideoIDs = append([]int64(nil), page.VideoIDs...)
	return nil
}

func (r *SessionRunner) metrics(ctx context.Context, state *sessionRunState, report *applicationacceptance.SessionSemanticReport) error {
	metrics := &applicationacceptance.SessionMetricEvidence{}
	var available bool
	metrics.BuilderFirstPageDelta, available = MetricDeltaSum(
		state.baselineAPI, state.afterFirstAPI,
		"frux_recommendation_session_semantic_operations_total",
		map[string]string{"stage": "builder", "result": "success"},
	)
	if !available || metrics.BuilderFirstPageDelta != 1 {
		return errors.New("builder first-page metric mismatch")
	}
	metrics.ProviderFirstPageDelta, available = MetricDeltaSum(
		state.baselineAPI, state.afterFirstAPI,
		"frux_recommendation_session_semantic_operations_total",
		map[string]string{"stage": "provider", "result": "success"},
	)
	if !available || metrics.ProviderFirstPageDelta != 1 {
		return errors.New("provider first-page metric mismatch")
	}
	metrics.BuilderSnapshotDelta, available = MetricDeltaSum(
		state.afterFirstAPI, state.afterSecondAPI,
		"frux_recommendation_session_semantic_operations_total",
		map[string]string{"stage": "builder"},
	)
	if !available || metrics.BuilderSnapshotDelta != 0 {
		return errors.New("builder snapshot metric mismatch")
	}
	metrics.ProviderSnapshotDelta, available = MetricDeltaSum(
		state.afterFirstAPI, state.afterSecondAPI,
		"frux_recommendation_session_semantic_operations_total",
		map[string]string{"stage": "provider"},
	)
	if !available || metrics.ProviderSnapshotDelta != 0 {
		return errors.New("provider snapshot metric mismatch")
	}
	metrics.SnapshotWriteDelta, available = MetricDeltaSum(
		state.baselineAPI, state.afterFirstAPI,
		"frux_recommendation_snapshot_operations_total", map[string]string{"result": "write_success"},
	)
	if !available || metrics.SnapshotWriteDelta < 1 {
		return errors.New("snapshot write metric missing")
	}
	metrics.SnapshotHitDelta, available = MetricDeltaSum(
		state.afterFirstAPI, state.afterSecondAPI,
		"frux_recommendation_snapshot_operations_total", map[string]string{"result": "hit"},
	)
	if !available || metrics.SnapshotHitDelta < 1 {
		return errors.New("snapshot hit metric missing")
	}
	if r.config.AdapterMetricsEndpoint != "" {
		var err error
		state.afterAdapter, err = r.runtime.CollectMetrics(ctx, r.config.AdapterMetricsEndpoint)
		if err != nil {
			return err
		}
		metrics.AdapterEvidenceAvailable = true
		metrics.AdapterOperationDelta, available = MetricDeltaSum(
			state.baselineAdapter, state.afterAdapter,
			"frux_tongyi_provider_operations_total", map[string]string{},
		)
		if !available || metrics.AdapterOperationDelta != 0 {
			return errors.New("adapter operation delta is non-zero")
		}
	}
	report.Metrics = metrics
	return nil
}

func (r *SessionRunner) disablePolicy(ctx context.Context, state *sessionRunState, report *applicationacceptance.SessionSemanticReport) error {
	if !state.policyCreated || state.policyDisabled {
		return nil
	}
	if err := r.store.DisablePolicy(ctx, state.policy.ID, state.policy.Version); err != nil {
		return err
	}
	state.policyDisabled = true
	if report.Policy != nil {
		report.Policy.Disabled = true
	}
	if report.Cleanup != nil {
		report.Cleanup.PolicyDisabled = true
	}
	return nil
}

func (r *SessionRunner) cleanup(ctx context.Context, state *sessionRunState, report *applicationacceptance.SessionSemanticReport) error {
	if state.favoriteCreated {
		if err := r.api.SetSessionFavorite(
			ctx, state.token, r.config.PositiveSeedVideoID, state.requestID,
			reportIdentity(state.requestID, "favorite-cleanup"), false,
		); err != nil {
			return err
		}
		report.Cleanup.FavoriteReverted = true
	}
	if state.policyCreated {
		if !state.policyDisabled {
			return errors.New("policy is not disabled")
		}
		if err := r.store.DeleteDisabledPolicy(ctx, state.policy.ID, state.policy.Version); err != nil {
			return err
		}
		report.Cleanup.PolicyDeleted = true
		if report.Policy != nil {
			report.Policy.Deleted = true
		}
	}
	report.Cleanup.PolicyDisabled = state.policyDisabled
	report.Cleanup.Result = applicationacceptance.ResultSuccess
	return nil
}

func (r *SessionRunner) recoverPolicy(state *sessionRunState, report *applicationacceptance.SessionSemanticReport) {
	if state == nil || !state.policyCreated || state.policyDisabled {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), min(r.config.StageTimeout, 10*time.Second))
	defer cancel()
	if err := r.store.DisablePolicy(ctx, state.policy.ID, state.policy.Version); err == nil {
		state.policyDisabled = true
		if report.Policy != nil {
			report.Policy.Disabled = true
		}
		if report.Cleanup != nil {
			report.Cleanup.PolicyDisabled = true
		}
	} else if report.Cleanup != nil {
		report.Cleanup.Result = applicationacceptance.ResultFailed
	}
}

func (r *SessionRunner) poll(ctx context.Context, check func() (bool, error)) error {
	ticker := time.NewTicker(r.config.PollInterval)
	defer ticker.Stop()
	for {
		done, err := check()
		if err != nil || done {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *SessionRunner) finishValidation(report *applicationacceptance.SessionSemanticReport) {
	report.Result = applicationacceptance.ResultSuccess
	report.FinishedAt = r.now()
	foundPreflight := false
	for index := range report.Stages {
		if report.Stages[index].Name == applicationacceptance.SessionStagePreflight {
			foundPreflight = true
			continue
		}
		if foundPreflight && report.Stages[index].Result == applicationacceptance.ResultPlanned {
			report.Stages[index].Result = applicationacceptance.ResultSkipped
		}
	}
	if report.Cleanup != nil {
		report.Cleanup.Result = applicationacceptance.ResultSkipped
	}
}

func (r *SessionRunner) finishFailure(report *applicationacceptance.SessionSemanticReport, failed applicationacceptance.SessionSemanticStageName, err error) {
	report.Result = applicationacceptance.ResultFailed
	report.FinishedAt = r.now()
	found := false
	for index := range report.Stages {
		if report.Stages[index].Name == failed {
			found = true
			report.Failure = report.Stages[index].Failure
			continue
		}
		if found && report.Stages[index].Result == applicationacceptance.ResultPlanned {
			report.Stages[index].Result = applicationacceptance.ResultSkipped
		}
	}
	if report.Failure == "" {
		report.Failure = sessionFailureForContext(err, applicationacceptance.SessionFailureInternal)
	}
}

func sessionFailureForContext(err error, fallback applicationacceptance.SessionSemanticFailureCode) applicationacceptance.SessionSemanticFailureCode {
	if errors.Is(err, context.Canceled) {
		return applicationacceptance.SessionFailureCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return applicationacceptance.SessionFailureTimeout
	}
	return fallback
}

func mapSessionPrerequisiteResult(err error) applicationacceptance.Result {
	if err != nil {
		return applicationacceptance.ResultFailed
	}
	return applicationacceptance.ResultSuccess
}

func reportIdentity(requestID, suffix string) string {
	value := strings.TrimSpace(requestID) + "-" + strings.TrimSpace(suffix)
	if len(value) <= 128 {
		return value
	}
	return fmt.Sprintf("session-acceptance-%x", []byte(value)[:48])
}
