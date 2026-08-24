package rollout

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	infrarecommendation "github.com/shiyudesu/frux/internal/infra/persistence/recommendation"
	shadowevaluation "github.com/shiyudesu/frux/internal/infra/shadowevaluation"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const OperatorReportSchemaV1 = "session-semantic-rollout-report/v1"
const OperatorToolVersionV1 = "session-semantic-rollout/v1"

var ErrOperatorBlocked = errors.New("session semantic rollout operator blocked")
var ErrOperatorFailed = errors.New("session semantic rollout operator failed")

type Preflight struct {
	Name   string `json:"name"`
	Result string `json:"result"`
}

type CohortReport struct {
	Samples  int   `json:"samples"`
	Selected int   `json:"selected"`
	Fallback int   `json:"fallback"`
	Buckets  []int `json:"buckets"`
}

type TargetReport struct {
	Exists     bool `json:"exists"`
	Compatible bool `json:"compatible"`
	Enabled    bool `json:"enabled"`
}

type OperatorReport struct {
	Schema             string       `json:"schema"`
	ToolVersion        string       `json:"tool_version"`
	Action             string       `json:"action"`
	Mode               string       `json:"mode"`
	Result             string       `json:"result"`
	Failure            string       `json:"failure,omitempty"`
	Profile            string       `json:"profile,omitempty"`
	SourceVersion      int          `json:"source_version,omitempty"`
	TargetVersion      int          `json:"target_version"`
	RolloutPercentage  int          `json:"rollout_percentage,omitempty"`
	ContractKey        string       `json:"contract_key,omitempty"`
	ConfigDigest       string       `json:"config_digest,omitempty"`
	ShadowReportSHA256 string       `json:"shadow_report_sha256,omitempty"`
	RuntimeReady       bool         `json:"runtime_ready"`
	ActivationReady    bool         `json:"activation_ready"`
	BaselineReady      bool         `json:"baseline_ready"`
	Target             TargetReport `json:"target"`
	MutationReplayed   bool         `json:"mutation_replayed"`
	PolicyChanges      []string     `json:"policy_changes,omitempty"`
	Cohort             CohortReport `json:"cohort"`
	Preflights         []Preflight  `json:"preflights"`
	RecoveryCommand    string       `json:"recovery_command"`
	ExternalModelCalls int          `json:"external_model_calls"`
	Limitations        []string     `json:"limitations"`
}

type evidenceLoader func(string) (*shadowevaluation.Report, string, error)
type readinessProbe func(context.Context, string, time.Duration, int64) (bool, error)

type Runner struct {
	service        *applicationrecommendation.SessionSemanticRolloutService
	loadEvidence   evidenceLoader
	probeReadiness readinessProbe
}

func NewRunner(
	service *applicationrecommendation.SessionSemanticRolloutService,
	loadEvidence evidenceLoader,
	probeReadiness readinessProbe,
) *Runner {
	if loadEvidence == nil {
		loadEvidence = shadowevaluation.LoadReport
	}
	if probeReadiness == nil {
		probeReadiness = ProbeSessionSemanticRuntimeReady
	}
	return &Runner{service: service, loadEvidence: loadEvidence, probeReadiness: probeReadiness}
}

func Run(ctx context.Context, config Config, execute bool, gate string) (OperatorReport, error) {
	sqlDB, err := sql.Open("pgx", config.PostgresDSN)
	if err != nil {
		return baseReport(config, execute), ErrOperatorFailed
	}
	defer sqlDB.Close()
	pingCtx, cancel := context.WithTimeout(ctx, config.HTTPTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return baseReport(config, execute), ErrOperatorFailed
	}
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		return baseReport(config, execute), ErrOperatorFailed
	}
	service := applicationrecommendation.NewSessionSemanticRolloutService(infrarecommendation.New(db), nil)
	return NewRunner(service, nil, nil).Execute(ctx, config, execute, gate)
}

func (r *Runner) Execute(
	ctx context.Context,
	config Config,
	execute bool,
	gate string,
) (OperatorReport, error) {
	report := baseReport(config, execute)
	if r == nil || r.service == nil || validateConfig(config) != nil {
		return finishReport(report, "error", "configuration", false), ErrOperatorFailed
	}
	report.Preflights = append(report.Preflights, Preflight{Name: "configuration", Result: "success"})

	evidenceAccepted := false
	if config.Action == ActionPlan || config.Action == ActionCreate || config.Action == ActionActivate {
		evidence, digest, err := r.loadEvidence(config.ShadowReportPath)
		if err != nil || evidence == nil || evidence.ContractKey != config.Contract.Key() {
			report.Preflights = append(report.Preflights, Preflight{Name: "shadow_evidence", Result: "failed"})
			return finishReport(report, "blocked", "shadow_evidence", false), ErrOperatorBlocked
		}
		evidenceAccepted = true
		report.ShadowReportSHA256 = digest
		report.Preflights = append(report.Preflights, Preflight{Name: "shadow_evidence", Result: "success"})
	} else {
		report.Preflights = append(report.Preflights, Preflight{Name: "shadow_evidence", Result: "skipped"})
	}

	runtimeReady := false
	if config.Action == ActionPlan || config.Action == ActionCreate || config.Action == ActionActivate {
		ready, err := r.probeReadiness(ctx, config.MetricsEndpoint, config.HTTPTimeout, config.MaxResponseBytes)
		if err != nil {
			report.Preflights = append(report.Preflights, Preflight{Name: "runtime_readiness", Result: "failed"})
			return finishReport(report, "blocked", "runtime_readiness", false), ErrOperatorBlocked
		}
		runtimeReady = ready
		report.RuntimeReady = ready
		result := "failed"
		if ready {
			result = "success"
		}
		report.Preflights = append(report.Preflights, Preflight{Name: "runtime_readiness", Result: result})
	} else {
		report.Preflights = append(report.Preflights, Preflight{Name: "runtime_readiness", Result: "skipped"})
	}

	input := applicationrecommendation.SessionSemanticRolloutLifecycleInput{
		SourceVersion: config.SourceVersion, TargetVersion: config.TargetVersion,
		RolloutPercentage: config.RolloutPercentage, AllowFullRollout: config.AllowFullRollout,
		Contract:         config.Contract,
		EvidenceAccepted: evidenceAccepted, RuntimeReady: runtimeReady,
	}

	if config.Action == ActionDisable {
		if !MutationAllowed(config.Action, execute, gate) {
			report.Preflights = append(report.Preflights, Preflight{Name: "mutation_gate", Result: "failed"})
			return finishReport(report, "blocked", "mutation_gate", false), ErrOperatorBlocked
		}
		report.Preflights = append(report.Preflights, Preflight{Name: "mutation_gate", Result: "success"})
		mutation, err := r.service.Disable(ctx, config.TargetVersion)
		if err != nil {
			return finishReport(report, "error", failureCode(err), false), ErrOperatorFailed
		}
		applyMutationToReport(&report, mutation)
		result := "success"
		if mutation.Replayed {
			result = "replay"
		}
		return finishReport(report, result, "", mutation.Replayed), nil
	}

	state, err := r.service.Status(ctx, input)
	if err != nil {
		return finishReport(report, "error", failureCode(err), false), ErrOperatorFailed
	}
	applyStateToReport(&report, state)
	report.ActivationReady = evidenceAccepted && runtimeReady && state.BaselineReady &&
		state.TargetExists && state.TargetCompatible
	report.Preflights = append(report.Preflights,
		Preflight{Name: "baseline_fallback", Result: boolResult(state.BaselineReady)},
		Preflight{Name: "target_compatibility", Result: targetResult(state)},
	)

	switch config.Action {
	case ActionPlan:
		return finishReport(report, "planned", "", false), nil
	case ActionStatus:
		return finishReport(report, "success", "", false), nil
	case ActionCreate, ActionActivate:
		if !MutationAllowed(config.Action, execute, gate) {
			report.Preflights = append(report.Preflights, Preflight{Name: "mutation_gate", Result: "failed"})
			return finishReport(report, "blocked", "mutation_gate", false), ErrOperatorBlocked
		}
		report.Preflights = append(report.Preflights, Preflight{Name: "mutation_gate", Result: "success"})
		var mutation *applicationrecommendation.SessionSemanticRolloutMutation
		if config.Action == ActionCreate {
			mutation, err = r.service.Create(ctx, input)
		} else {
			mutation, err = r.service.Activate(ctx, input)
		}
		if err != nil {
			result := "error"
			if errors.Is(err, applicationrecommendation.ErrSessionSemanticRolloutBlocked) {
				result = "blocked"
			}
			return finishReport(report, result, failureCode(err), false), err
		}
		applyMutationToReport(&report, mutation)
		result := "success"
		if mutation.Replayed {
			result = "replay"
		}
		return finishReport(report, result, "", mutation.Replayed), nil
	default:
		return finishReport(report, "error", "action", false), ErrOperatorFailed
	}
}

func WriteReport(output io.Writer, path string, report OperatorReport) error {
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return ErrOperatorFailed
	}
	payload = append(payload, '\n')
	if output != nil {
		if _, err := output.Write(payload); err != nil {
			return ErrOperatorFailed
		}
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	directory := filepath.Dir(path)
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		return ErrOperatorFailed
	}
	file, err := os.CreateTemp(directory, ".frux-rollout-*")
	if err != nil {
		return ErrOperatorFailed
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return ErrOperatorFailed
	}
	if _, err := file.Write(payload); err != nil || file.Sync() != nil || file.Close() != nil {
		_ = file.Close()
		return ErrOperatorFailed
	}
	if err := os.Rename(temporary, path); err != nil {
		return ErrOperatorFailed
	}
	return os.Chmod(path, 0o600)
}

func baseReport(config Config, execute bool) OperatorReport {
	mode := "read_only"
	if MutatingAction(config.Action) && execute {
		mode = "mutation_requested"
	}
	contractKey := ""
	if config.ExpectedProfile != "" {
		contractKey = config.Contract.Key()
	}
	return OperatorReport{
		Schema: OperatorReportSchemaV1, ToolVersion: OperatorToolVersionV1,
		Action: string(config.Action), Mode: mode, Result: "planned",
		Profile: config.ExpectedProfile, SourceVersion: config.SourceVersion,
		TargetVersion: config.TargetVersion, RolloutPercentage: config.RolloutPercentage,
		ContractKey: contractKey, RecoveryCommand: "go run ./cmd/session-semantic-rollout --action disable --execute",
		ExternalModelCalls: 0,
		Limitations: []string{
			"Rollout evidence is operational and offline; it does not prove causal CTR, watch-time, or retention lift.",
			"Expansion beyond the configured target cohort requires a separate higher policy version and review.",
		},
	}
}

func applyStateToReport(report *OperatorReport, state *applicationrecommendation.SessionSemanticRolloutState) {
	if report == nil || state == nil {
		return
	}
	if state.Plan != nil {
		report.ConfigDigest = state.Plan.ConfigDigest
		report.PolicyChanges = append([]string(nil), state.Plan.Changes...)
		report.Cohort = CohortReport{
			Samples: state.Plan.Cohort.Samples, Selected: state.Plan.Cohort.Selected,
			Fallback: state.Plan.Cohort.Fallback, Buckets: append([]int(nil), state.Plan.Cohort.Buckets...),
		}
	}
	report.BaselineReady = state.BaselineReady
	report.Target = TargetReport{
		Exists: state.TargetExists, Compatible: state.TargetCompatible,
		Enabled: state.Target != nil && state.Target.Enabled,
	}
}

func applyMutationToReport(report *OperatorReport, mutation *applicationrecommendation.SessionSemanticRolloutMutation) {
	if report == nil || mutation == nil {
		return
	}
	applyStateToReport(report, mutation.State)
	report.MutationReplayed = mutation.Replayed
	if mutation.Policy != nil {
		report.Target.Exists = true
		report.Target.Enabled = mutation.Policy.Enabled
		report.Target.Compatible = applicationrecommendation.IsSessionSemanticRolloutPolicy(mutation.Policy)
	}
}

func finishReport(report OperatorReport, result, failure string, replayed bool) OperatorReport {
	report.Result = result
	report.Failure = failure
	report.MutationReplayed = replayed
	metricResult := result
	if metricResult == "planned" {
		metricResult = "success"
	}
	inframetrics.ObserveRecommendationSessionSemanticRollout(report.Action, metricResult)
	return report
}

func failureCode(err error) string {
	switch {
	case errors.Is(err, applicationrecommendation.ErrSessionSemanticRolloutBlocked):
		return "prerequisite"
	case errors.Is(err, applicationrecommendation.ErrSessionSemanticRolloutConflict):
		return "target_conflict"
	case errors.Is(err, applicationrecommendation.ErrInvalidSessionSemanticRollout):
		return "policy"
	case errors.Is(err, domainrecommendation.ErrPolicyNotFound):
		return "policy_not_found"
	default:
		return "infrastructure"
	}
}

func boolResult(value bool) string {
	if value {
		return "success"
	}
	return "failed"
}

func targetResult(state *applicationrecommendation.SessionSemanticRolloutState) string {
	if state == nil || !state.TargetExists {
		return "absent"
	}
	if state.TargetCompatible {
		return "success"
	}
	return "failed"
}
