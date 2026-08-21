package applicationacceptance

import (
	"strings"
	"time"
)

const (
	SessionSemanticReportVersion = "session-semantic-acceptance-v1"
	SessionSemanticReportKind    = "technical_acceptance"
	SessionSemanticMutationGate  = "FRUX_SESSION_SEMANTIC_ACCEPTANCE_ALLOW_MUTATION"
)

type SessionSemanticStageName string

const (
	SessionStagePreflight       SessionSemanticStageName = "preflight"
	SessionStageLogin           SessionSemanticStageName = "login"
	SessionStagePolicy          SessionSemanticStageName = "install_policy"
	SessionStagePositiveFacts   SessionSemanticStageName = "positive_facts"
	SessionStageNegativeFacts   SessionSemanticStageName = "negative_facts"
	SessionStageFirstPage       SessionSemanticStageName = "first_page"
	SessionStageRequestEvidence SessionSemanticStageName = "request_evidence"
	SessionStageSnapshotPage    SessionSemanticStageName = "snapshot_page"
	SessionStageMetrics         SessionSemanticStageName = "metrics"
	SessionStageDisablePolicy   SessionSemanticStageName = "disable_policy"
	SessionStageCleanup         SessionSemanticStageName = "cleanup"
)

var SessionSemanticExecutionStages = []SessionSemanticStageName{
	SessionStagePreflight,
	SessionStageLogin,
	SessionStagePolicy,
	SessionStagePositiveFacts,
	SessionStageNegativeFacts,
	SessionStageFirstPage,
	SessionStageRequestEvidence,
	SessionStageSnapshotPage,
	SessionStageMetrics,
	SessionStageDisablePolicy,
}

type SessionSemanticFailureCode string

const (
	SessionFailureConfiguration  SessionSemanticFailureCode = "configuration"
	SessionFailurePrerequisite   SessionSemanticFailureCode = "prerequisite"
	SessionFailureAuthentication SessionSemanticFailureCode = "authentication"
	SessionFailurePolicy         SessionSemanticFailureCode = "policy"
	SessionFailureBehavior       SessionSemanticFailureCode = "behavior"
	SessionFailureFeed           SessionSemanticFailureCode = "feed"
	SessionFailureEvidence       SessionSemanticFailureCode = "evidence"
	SessionFailureSnapshot       SessionSemanticFailureCode = "snapshot"
	SessionFailureMetrics        SessionSemanticFailureCode = "metrics"
	SessionFailureCleanup        SessionSemanticFailureCode = "cleanup"
	SessionFailureTimeout        SessionSemanticFailureCode = "timeout"
	SessionFailureCancelled      SessionSemanticFailureCode = "cancelled"
	SessionFailureInternal       SessionSemanticFailureCode = "internal"
)

type SessionSemanticStageResult struct {
	Name       SessionSemanticStageName   `json:"name"`
	Result     Result                     `json:"result"`
	DurationMS int64                      `json:"duration_ms"`
	Failure    SessionSemanticFailureCode `json:"failure_code,omitempty"`
}

type SessionSemanticConfig struct {
	APIEndpoint            string        `json:"-"`
	APIMetricsEndpoint     string        `json:"-"`
	AdapterMetricsEndpoint string        `json:"-"`
	PostgresDSN            string        `json:"-"`
	UserAccount            string        `json:"-"`
	UserPassword           string        `json:"-"`
	ExpectedProfile        string        `json:"-"`
	PositiveSeedVideoID    int64         `json:"-"`
	NegativeSeedVideoID    int64         `json:"-"`
	ExpectedTargetVideoID  int64         `json:"-"`
	PollInterval           time.Duration `json:"-"`
	StageTimeout           time.Duration `json:"-"`
	HTTPTimeout            time.Duration `json:"-"`
	MaxResponseBytes       int64         `json:"-"`
}

type SessionFixtureEvidence struct {
	PositiveSeedVideoID   int64   `json:"positive_seed_video_id"`
	NegativeSeedVideoID   int64   `json:"negative_seed_video_id"`
	ExpectedTargetVideoID int64   `json:"expected_target_video_id"`
	TargetSimilarity      float64 `json:"target_similarity,omitempty"`
}

type SessionPolicyEvidence struct {
	ID             int64 `json:"id,omitempty"`
	Version        int   `json:"version,omitempty"`
	RolloutPercent int   `json:"rollout_percent,omitempty"`
	Disabled       bool  `json:"disabled"`
	Deleted        bool  `json:"deleted"`
}

type SessionRequestEvidence struct {
	UserID             int64   `json:"user_id,omitempty"`
	RequestID          string  `json:"request_id,omitempty"`
	SessionID          string  `json:"session_id,omitempty"`
	PolicyVersion      int     `json:"policy_version,omitempty"`
	Confidence         float64 `json:"confidence,omitempty"`
	ConfidenceBand     string  `json:"confidence_band,omitempty"`
	PositiveCount      int     `json:"positive_count,omitempty"`
	NegativeCount      int     `json:"negative_count,omitempty"`
	CompatibleCount    int     `json:"compatible_count,omitempty"`
	ExpectedTargetSeen bool    `json:"expected_target_seen"`
	SemanticSimilarity float64 `json:"semantic_similarity,omitempty"`
	FirstPageVideoIDs  []int64 `json:"first_page_video_ids,omitempty"`
	SecondPageVideoIDs []int64 `json:"second_page_video_ids,omitempty"`
}

type SessionMetricEvidence struct {
	BuilderFirstPageDelta    int64 `json:"builder_first_page_delta"`
	ProviderFirstPageDelta   int64 `json:"provider_first_page_delta"`
	BuilderSnapshotDelta     int64 `json:"builder_snapshot_delta"`
	ProviderSnapshotDelta    int64 `json:"provider_snapshot_delta"`
	SnapshotWriteDelta       int64 `json:"snapshot_write_delta"`
	SnapshotHitDelta         int64 `json:"snapshot_hit_delta"`
	AdapterEvidenceAvailable bool  `json:"adapter_evidence_available"`
	AdapterOperationDelta    int64 `json:"adapter_operation_delta,omitempty"`
}

type SessionCleanupEvidence struct {
	Requested        bool   `json:"requested"`
	Result           Result `json:"result"`
	FavoriteReverted bool   `json:"favorite_reverted"`
	PolicyDisabled   bool   `json:"policy_disabled"`
	PolicyDeleted    bool   `json:"policy_deleted"`
}

type SessionSemanticReport struct {
	Version            string                       `json:"version"`
	Kind               string                       `json:"kind"`
	RunID              string                       `json:"run_id"`
	Mode               Mode                         `json:"mode"`
	Result             Result                       `json:"result"`
	Failure            SessionSemanticFailureCode   `json:"failure_code,omitempty"`
	StartedAt          time.Time                    `json:"started_at"`
	FinishedAt         time.Time                    `json:"finished_at"`
	ExternalModelCalls int                          `json:"external_model_calls"`
	Stages             []SessionSemanticStageResult `json:"stages"`
	Prerequisites      []PrerequisiteResult         `json:"prerequisites,omitempty"`
	Contract           *ContractEvidence            `json:"contract,omitempty"`
	Fixtures           *SessionFixtureEvidence      `json:"fixtures,omitempty"`
	Policy             *SessionPolicyEvidence       `json:"policy,omitempty"`
	Request            *SessionRequestEvidence      `json:"request,omitempty"`
	Metrics            *SessionMetricEvidence       `json:"metrics,omitempty"`
	Cleanup            *SessionCleanupEvidence      `json:"cleanup,omitempty"`
}

func NewSessionSemanticReport(
	runID string,
	mode Mode,
	startedAt time.Time,
	cleanup bool,
) SessionSemanticReport {
	stages := make([]SessionSemanticStageResult, 0, len(SessionSemanticExecutionStages)+1)
	for _, name := range SessionSemanticExecutionStages {
		stages = append(stages, SessionSemanticStageResult{Name: name, Result: ResultPlanned})
	}
	if cleanup {
		stages = append(stages, SessionSemanticStageResult{Name: SessionStageCleanup, Result: ResultPlanned})
	}
	return SessionSemanticReport{
		Version: SessionSemanticReportVersion, Kind: SessionSemanticReportKind,
		RunID: runID, Mode: mode, Result: ResultPlanned, StartedAt: startedAt.UTC(),
		ExternalModelCalls: 0, Stages: stages,
		Cleanup: &SessionCleanupEvidence{Requested: cleanup, Result: ResultPlanned},
	}
}

func DecideSessionSemanticMutation(execute bool, environmentValue string) ExecutionDecision {
	approved := strings.EqualFold(strings.TrimSpace(environmentValue), "true")
	confirmed := execute && approved
	mode := ModeValidation
	if confirmed {
		mode = ModeExecution
	}
	return ExecutionDecision{
		Mode: mode, ExecuteRequested: execute,
		EnvironmentApproved: approved, Confirmed: confirmed,
	}
}
