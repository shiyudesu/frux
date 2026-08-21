package infraacceptance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
)

type acceptanceAPI interface {
	Login(context.Context, bool, string, string) (string, error)
	UploadFixture(context.Context, string, string, string, string) (CreatedAsset, error)
	CreateVideo(context.Context, string, int64, int64, string, string, string) (CreatedVideo, error)
	ClaimReview(context.Context, string, int64, int) (ReviewLease, error)
	ApproveReview(context.Context, string, int64, int, ReviewLease, string) error
	Similar(context.Context, int64) (SimilarResult, error)
	Hybrid(context.Context, string) ([]int64, error)
	DeleteVideo(context.Context, string, int64) error
}

type acceptanceEvidence interface {
	Ping(context.Context) error
	ReviewCase(context.Context, int64) (ReviewCaseEvidence, error)
	Multimodal(context.Context, int64, string) (DatabaseEvidence, error)
}

type acceptanceRuntime interface {
	CheckHealth(context.Context, string) error
	CollectMetrics(context.Context, string) (MetricSnapshot, error)
}

type Runner struct {
	config   applicationacceptance.Config
	runtime  acceptanceRuntime
	api      acceptanceAPI
	evidence acceptanceEvidence
	now      func() time.Time
}

func NewRunner(
	config applicationacceptance.Config,
	runtime acceptanceRuntime,
	api acceptanceAPI,
	evidence acceptanceEvidence,
) (*Runner, error) {
	if runtime == nil || api == nil || evidence == nil {
		return nil, ErrInvalidAcceptanceConfig
	}
	return &Runner{config: config, runtime: runtime, api: api, evidence: evidence, now: func() time.Time { return time.Now().UTC() }}, nil
}

type runState struct {
	userToken  string
	adminToken string
	fixtures   [2]applicationacceptance.FixtureEvidence
	videos     [2]CreatedVideo
	database   [2]DatabaseEvidence
	baseline   MetricSnapshot
}

func (r *Runner) Run(ctx context.Context, report applicationacceptance.Report) (applicationacceptance.Report, error) {
	state := &runState{}
	steps := []struct {
		name applicationacceptance.StageName
		code applicationacceptance.FailureCode
		fn   func(context.Context) error
	}{
		{applicationacceptance.StagePreflight, applicationacceptance.FailurePrerequisite, func(stage context.Context) error { return r.preflight(stage, state, &report) }},
		{applicationacceptance.StageLogin, applicationacceptance.FailureAuthentication, func(stage context.Context) error { return r.login(stage, state) }},
		{applicationacceptance.StageUploadFixtureA, applicationacceptance.FailureUpload, func(stage context.Context) error { return r.upload(stage, state, 0) }},
		{applicationacceptance.StageUploadFixtureB, applicationacceptance.FailureUpload, func(stage context.Context) error { return r.upload(stage, state, 1) }},
		{applicationacceptance.StageCreateVideoA, applicationacceptance.FailureUpload, func(stage context.Context) error { return r.createVideo(stage, state, 0, report.RunID) }},
		{applicationacceptance.StageCreateVideoB, applicationacceptance.FailureUpload, func(stage context.Context) error { return r.createVideo(stage, state, 1, report.RunID) }},
		{applicationacceptance.StageApproveVideoA, applicationacceptance.FailureReview, func(stage context.Context) error { return r.approve(stage, state, 0, report.RunID) }},
		{applicationacceptance.StageApproveVideoB, applicationacceptance.FailureReview, func(stage context.Context) error { return r.approve(stage, state, 1, report.RunID) }},
		{applicationacceptance.StageWaitEmbeddingA, applicationacceptance.FailureEmbedding, func(stage context.Context) error { return r.waitEmbedding(stage, state, 0) }},
		{applicationacceptance.StageWaitEmbeddingB, applicationacceptance.FailureEmbedding, func(stage context.Context) error { return r.waitEmbedding(stage, state, 1) }},
		{applicationacceptance.StageVerifyFactProjection, applicationacceptance.FailureEvidence, func(context.Context) error { r.populateEvidence(state, &report); return nil }},
		{applicationacceptance.StageSimilar, applicationacceptance.FailureSimilar, func(stage context.Context) error { return r.similar(stage, state, &report) }},
		{applicationacceptance.StageHybrid, applicationacceptance.FailureHybrid, func(stage context.Context) error { return r.hybrid(stage, state, &report) }},
		{applicationacceptance.StageMetrics, applicationacceptance.FailureMetrics, func(stage context.Context) error { return r.metrics(stage, state, &report) }},
	}
	for _, step := range steps {
		if err := r.stage(ctx, &report, step.name, step.code, step.fn); err != nil {
			r.finishFailure(&report, step.name, err)
			return report, err
		}
	}
	if report.Cleanup != nil && report.Cleanup.Requested {
		if err := r.stage(ctx, &report, applicationacceptance.StageCleanup, applicationacceptance.FailureCleanup, func(stage context.Context) error {
			return r.cleanup(stage, state, &report)
		}); err != nil {
			r.finishFailure(&report, applicationacceptance.StageCleanup, err)
			return report, err
		}
	}
	report.Result = applicationacceptance.ResultSuccess
	report.FinishedAt = r.now()
	return report, nil
}

func (r *Runner) stage(parent context.Context, report *applicationacceptance.Report, name applicationacceptance.StageName, code applicationacceptance.FailureCode, fn func(context.Context) error) error {
	started := r.now()
	ctx, cancel := context.WithTimeout(parent, r.config.StageTimeout)
	defer cancel()
	err := fn(ctx)
	result := applicationacceptance.ResultSuccess
	if err != nil {
		result = applicationacceptance.ResultFailed
	}
	for index := range report.Stages {
		if report.Stages[index].Name == name {
			report.Stages[index].Result = result
			report.Stages[index].DurationMS = max(r.now().Sub(started).Milliseconds(), 0)
			if err != nil {
				report.Stages[index].Failure = failureForContext(err, code)
			}
			break
		}
	}
	return err
}

func (r *Runner) preflight(ctx context.Context, state *runState, report *applicationacceptance.Report) error {
	checks := []struct {
		name string
		fn   func() error
	}{
		{"api_health", func() error { return r.runtime.CheckHealth(ctx, r.config.APIEndpoint) }},
		{"adapter_health", func() error { return r.runtime.CheckHealth(ctx, r.config.AdapterEndpoint) }},
		{"postgres_read", func() error { return r.evidence.Ping(ctx) }},
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
	baseline, err := r.collectAllMetrics(ctx)
	if err != nil {
		return err
	}
	if baseline["frux_multimodal_provider_transport_total{operation=readiness,result=success}"] < 2 ||
		baseline["frux_tongyi_provider_operations_total{operation=startup,result=success}"] < 1 {
		return errors.New("multimodal runtime readiness evidence missing")
	}
	state.baseline = baseline
	return nil
}

func (r *Runner) login(ctx context.Context, state *runState) error {
	var err error
	state.userToken, err = r.api.Login(ctx, false, r.config.UserAccount, r.config.UserPassword)
	if err != nil {
		return err
	}
	state.adminToken, err = r.api.Login(ctx, true, r.config.AdminAccount, r.config.AdminPassword)
	return err
}

func (r *Runner) upload(ctx context.Context, state *runState, index int) error {
	label := string(rune('a' + index))
	video, err := r.api.UploadFixture(ctx, state.userToken, "video", r.config.VideoFixturePath, "acceptance-"+label+"-video")
	if err != nil {
		return err
	}
	cover, err := r.api.UploadFixture(ctx, state.userToken, "cover", r.config.CoverFixturePath, "acceptance-"+label+"-cover")
	if err != nil {
		return err
	}
	state.fixtures[index] = applicationacceptance.FixtureEvidence{Label: strings.ToUpper(label), MediaAssetID: video.ID, CoverAssetID: cover.ID}
	return nil
}

func (r *Runner) createVideo(ctx context.Context, state *runState, index int, runID string) error {
	label := state.fixtures[index].Label
	created, err := r.api.CreateVideo(ctx, state.userToken, state.fixtures[index].MediaAssetID, state.fixtures[index].CoverAssetID,
		r.config.Query+" 技术验收 "+label, "multimodal technical acceptance fixture "+label,
		runID+"-video-"+strings.ToLower(label))
	if err != nil {
		return err
	}
	state.videos[index] = created
	state.fixtures[index].VideoID = created.ID
	return nil
}

func (r *Runner) approve(ctx context.Context, state *runState, index int, runID string) error {
	videoID := state.videos[index].ID
	var review ReviewCaseEvidence
	err := r.poll(ctx, func() (bool, error) {
		value, err := r.evidence.ReviewCase(ctx, videoID)
		if err != nil {
			var failure *EvidenceError
			if errors.As(err, &failure) && failure.Code == EvidenceUnavailable {
				return false, nil
			}
			return false, err
		}
		review = value
		return true, nil
	})
	if err != nil {
		return err
	}
	lease, err := r.api.ClaimReview(ctx, state.adminToken, review.ID, review.Version)
	if err != nil {
		return err
	}
	return r.api.ApproveReview(ctx, state.adminToken, review.ID, review.ReviewVersion, lease, runID+"-approve-"+strings.ToLower(state.fixtures[index].Label))
}

func (r *Runner) waitEmbedding(ctx context.Context, state *runState, index int) error {
	return r.poll(ctx, func() (bool, error) {
		evidence, err := r.evidence.Multimodal(ctx, state.videos[index].ID, r.config.ExpectedProfile)
		if err != nil {
			var failure *EvidenceError
			if errors.As(err, &failure) && (failure.Code == EvidenceUnavailable || failure.Code == EvidenceJobIncomplete || failure.Code == EvidenceFactMissing || failure.Code == EvidenceProjectionMissing) {
				return false, nil
			}
			return false, err
		}
		state.database[index] = evidence
		return true, nil
	})
}

func (r *Runner) populateEvidence(state *runState, report *applicationacceptance.Report) {
	report.Fixtures = make([]applicationacceptance.FixtureEvidence, 0, 2)
	report.Vectors = make([]applicationacceptance.VectorEvidence, 0, 2)
	for index := range state.database {
		fixture, vector := state.database[index].Report(state.videos[index].ID)
		fixture.Label = state.fixtures[index].Label
		fixture.MediaAssetID = state.fixtures[index].MediaAssetID
		fixture.CoverAssetID = state.fixtures[index].CoverAssetID
		report.Fixtures = append(report.Fixtures, fixture)
		report.Vectors = append(report.Vectors, vector)
	}
	contract := state.database[0].Contract
	report.Contract = &applicationacceptance.ContractEvidence{
		ProviderAlias: contract.ProviderAlias, ModelAlias: contract.ModelAlias, RevisionAlias: contract.RevisionAlias,
		Dimension: contract.Dimension, TextCanonicalizer: contract.TextCanonicalizer,
		FrameSamplingPolicy: contract.FrameSamplingPolicy, ImagePreprocessingPolicy: contract.ImagePreprocessingPolicy,
		FusionPolicy: contract.FusionPolicy,
	}
}

func (r *Runner) similar(ctx context.Context, state *runState, report *applicationacceptance.Report) error {
	result, err := r.api.Similar(ctx, state.videos[0].ID)
	if err != nil {
		return err
	}
	if !result.Available || !containsID(result.VideoIDs, state.videos[1].ID) {
		return errors.New("similar fixture missing")
	}
	report.Retrieval = &applicationacceptance.RetrievalEvidence{SimilarSourceVideoID: state.videos[0].ID, SimilarAvailable: true, SimilarVideoIDs: result.VideoIDs}
	return nil
}

func (r *Runner) hybrid(ctx context.Context, state *runState, report *applicationacceptance.Report) error {
	ids, err := r.api.Hybrid(ctx, r.config.Query)
	if err != nil {
		return err
	}
	if !containsID(ids, state.videos[0].ID) || !containsID(ids, state.videos[1].ID) {
		return errors.New("hybrid fixtures missing")
	}
	if report.Retrieval == nil {
		report.Retrieval = &applicationacceptance.RetrievalEvidence{}
	}
	report.Retrieval.HybridQuery = r.config.Query
	report.Retrieval.HybridVideoIDs = ids
	return nil
}

func (r *Runner) metrics(ctx context.Context, state *runState, report *applicationacceptance.Report) error {
	after, err := r.collectAllMetrics(ctx)
	if err != nil {
		return err
	}
	report.MetricDeltas = MetricDeltas(state.baseline, after)
	return nil
}

func (r *Runner) cleanup(ctx context.Context, state *runState, report *applicationacceptance.Report) error {
	videoIDs := []int64{}
	for _, video := range state.videos {
		if video.ID <= 0 {
			continue
		}
		videoIDs = append(videoIDs, video.ID)
		if err := r.api.DeleteVideo(ctx, state.userToken, video.ID); err != nil {
			return err
		}
	}
	report.Cleanup.Result = applicationacceptance.ResultSuccess
	report.Cleanup.VideoIDs = videoIDs
	return nil
}

func (r *Runner) collectAllMetrics(ctx context.Context) (MetricSnapshot, error) {
	combined := MetricSnapshot{}
	for _, endpoint := range []string{r.config.AdapterEndpoint + "/metrics", r.config.APIMetricsEndpoint, r.config.WorkerMetricsEndpoint} {
		snapshot, err := r.runtime.CollectMetrics(ctx, endpoint)
		if err != nil {
			return nil, err
		}
		for key, value := range snapshot {
			combined[key] += value
		}
	}
	return combined, nil
}

func (r *Runner) poll(ctx context.Context, check func() (bool, error)) error {
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

func (r *Runner) finishFailure(report *applicationacceptance.Report, failed applicationacceptance.StageName, err error) {
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
		report.Failure = failureForContext(err, applicationacceptance.FailureInternal)
	}
}

func failureForContext(err error, fallback applicationacceptance.FailureCode) applicationacceptance.FailureCode {
	if errors.Is(err, context.Canceled) {
		return applicationacceptance.FailureCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return applicationacceptance.FailureTimeout
	}
	return fallback
}

func containsID(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (r *Runner) String() string {
	return fmt.Sprintf("multimodal acceptance runner for %s", r.config.ExpectedProfile)
}
