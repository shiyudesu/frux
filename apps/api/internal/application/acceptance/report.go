package applicationacceptance

import "time"

const ReportVersionV1 = "multimodal-acceptance-v1"
const ReportKindTechnicalAcceptance = "technical_acceptance"

type Mode string

const (
	ModeValidation Mode = "validation"
	ModeExecution  Mode = "execution"
)

type Result string

const (
	ResultPlanned Result = "planned"
	ResultSuccess Result = "success"
	ResultFailed  Result = "failed"
	ResultSkipped Result = "skipped"
	ResultUnknown Result = "unknown"
)

type FailureCode string

const (
	FailureConfiguration  FailureCode = "configuration"
	FailureBillableGate   FailureCode = "billable_gate"
	FailurePrerequisite   FailureCode = "prerequisite"
	FailureAuthentication FailureCode = "authentication"
	FailureUpload         FailureCode = "upload"
	FailureReview         FailureCode = "review"
	FailureEmbedding      FailureCode = "embedding"
	FailureEvidence       FailureCode = "evidence"
	FailureSimilar        FailureCode = "similar"
	FailureHybrid         FailureCode = "hybrid"
	FailureMetrics        FailureCode = "metrics"
	FailureTimeout        FailureCode = "timeout"
	FailureCancelled      FailureCode = "cancelled"
	FailureCleanup        FailureCode = "cleanup"
	FailureInternal       FailureCode = "internal"
)

type StageName string

const (
	StagePreflight            StageName = "preflight"
	StageLogin                StageName = "login"
	StageUploadFixtureA       StageName = "upload_fixture_a"
	StageUploadFixtureB       StageName = "upload_fixture_b"
	StageCreateVideoA         StageName = "create_video_a"
	StageCreateVideoB         StageName = "create_video_b"
	StageApproveVideoA        StageName = "approve_video_a"
	StageApproveVideoB        StageName = "approve_video_b"
	StageWaitEmbeddingA       StageName = "wait_embedding_a"
	StageWaitEmbeddingB       StageName = "wait_embedding_b"
	StageVerifyFactProjection StageName = "verify_fact_projection"
	StageSimilar              StageName = "similar"
	StageHybrid               StageName = "hybrid"
	StageMetrics              StageName = "metrics"
	StageCleanup              StageName = "cleanup"
)

var ExecutionStages = []StageName{
	StagePreflight,
	StageLogin,
	StageUploadFixtureA,
	StageUploadFixtureB,
	StageCreateVideoA,
	StageCreateVideoB,
	StageApproveVideoA,
	StageApproveVideoB,
	StageWaitEmbeddingA,
	StageWaitEmbeddingB,
	StageVerifyFactProjection,
	StageSimilar,
	StageHybrid,
	StageMetrics,
}

type StageResult struct {
	Name       StageName   `json:"name"`
	Result     Result      `json:"result"`
	DurationMS int64       `json:"duration_ms"`
	Failure    FailureCode `json:"failure_code,omitempty"`
}

type PrerequisiteResult struct {
	Name   string `json:"name"`
	Result Result `json:"result"`
}

type FixtureEvidence struct {
	Label        string `json:"label"`
	VideoID      int64  `json:"video_id,omitempty"`
	MediaAssetID int64  `json:"media_asset_id,omitempty"`
	CoverAssetID int64  `json:"cover_asset_id,omitempty"`
	JobID        int64  `json:"job_id,omitempty"`
	JobState     string `json:"job_state,omitempty"`
	Attempts     int    `json:"attempts,omitempty"`
}

type ContractEvidence struct {
	ProviderAlias            string `json:"provider_alias,omitempty"`
	ModelAlias               string `json:"model_alias,omitempty"`
	RevisionAlias            string `json:"revision_alias,omitempty"`
	Dimension                int    `json:"dimension,omitempty"`
	TextCanonicalizer        string `json:"text_canonicalizer,omitempty"`
	FrameSamplingPolicy      string `json:"frame_sampling_policy,omitempty"`
	ImagePreprocessingPolicy string `json:"image_preprocessing_policy,omitempty"`
	FusionPolicy             string `json:"fusion_policy,omitempty"`
}

type VectorEvidence struct {
	VideoID         int64   `json:"video_id"`
	Dimension       int     `json:"dimension"`
	Norm            float64 `json:"norm"`
	DigestMatches   bool    `json:"digest_matches"`
	ContractMatches bool    `json:"contract_matches"`
}

type RetrievalEvidence struct {
	SimilarSourceVideoID int64   `json:"similar_source_video_id,omitempty"`
	SimilarAvailable     bool    `json:"similar_available"`
	SimilarVideoIDs      []int64 `json:"similar_video_ids,omitempty"`
	HybridQuery          string  `json:"hybrid_query,omitempty"`
	HybridVideoIDs       []int64 `json:"hybrid_video_ids,omitempty"`
}

type MetricDelta struct {
	Operation string `json:"operation"`
	Kind      string `json:"kind"`
	Value     int64  `json:"value,omitempty"`
	Available bool   `json:"available"`
}

type CleanupEvidence struct {
	Requested bool    `json:"requested"`
	Result    Result  `json:"result"`
	VideoIDs  []int64 `json:"video_ids,omitempty"`
}

type Report struct {
	Version           string               `json:"version"`
	Kind              string               `json:"kind"`
	RunID             string               `json:"run_id"`
	Mode              Mode                 `json:"mode"`
	Result            Result               `json:"result"`
	Failure           FailureCode          `json:"failure_code,omitempty"`
	StartedAt         time.Time            `json:"started_at"`
	FinishedAt        time.Time            `json:"finished_at"`
	PlannedModelCalls int                  `json:"planned_model_calls"`
	Stages            []StageResult        `json:"stages"`
	Prerequisites     []PrerequisiteResult `json:"prerequisites,omitempty"`
	Contract          *ContractEvidence    `json:"contract,omitempty"`
	Fixtures          []FixtureEvidence    `json:"fixtures,omitempty"`
	Vectors           []VectorEvidence     `json:"vectors,omitempty"`
	Retrieval         *RetrievalEvidence   `json:"retrieval,omitempty"`
	MetricDeltas      []MetricDelta        `json:"metric_deltas,omitempty"`
	Cleanup           *CleanupEvidence     `json:"cleanup,omitempty"`
}

func NewReport(runID string, mode Mode, startedAt time.Time, cleanup bool) Report {
	stages := make([]StageResult, 0, len(ExecutionStages)+1)
	for _, stage := range ExecutionStages {
		stages = append(stages, StageResult{Name: stage, Result: ResultPlanned})
	}
	if cleanup {
		stages = append(stages, StageResult{Name: StageCleanup, Result: ResultPlanned})
	}
	return Report{
		Version: ReportVersionV1, Kind: ReportKindTechnicalAcceptance,
		RunID: runID, Mode: mode, Result: ResultPlanned, StartedAt: startedAt.UTC(),
		PlannedModelCalls: 3, Stages: stages,
		Cleanup: &CleanupEvidence{Requested: cleanup, Result: ResultPlanned},
	}
}
