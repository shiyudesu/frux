package rollout

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
	shadowevaluation "github.com/shiyudesu/frux/internal/infra/shadowevaluation"
)

type rolloutMemoryRepo struct {
	mu       sync.Mutex
	policies map[int]*domainrecommendation.Policy
}

func newRolloutMemoryRepo(t testing.TB) *rolloutMemoryRepo {
	t.Helper()
	policies, err := domainrecommendation.InitialRecommendationPolicies(time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	repo := &rolloutMemoryRepo{policies: map[int]*domainrecommendation.Policy{}}
	for _, policy := range policies {
		repo.policies[policy.Version] = policy.Clone()
	}
	return repo
}

func (r *rolloutMemoryRepo) CreatePolicy(_ context.Context, policy *domainrecommendation.Policy) (*domainrecommendation.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.policies[policy.Version] != nil {
		return nil, domainrecommendation.ErrInvalidPolicyVersion
	}
	r.policies[policy.Version] = policy.Clone()
	return policy.Clone(), nil
}

func (r *rolloutMemoryRepo) ActivatePolicy(_ context.Context, _ string, version int) (*domainrecommendation.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	policy := r.policies[version]
	if policy == nil {
		return nil, domainrecommendation.ErrPolicyNotFound
	}
	policy.Enabled = true
	return policy.Clone(), nil
}

func (*rolloutMemoryRepo) RollbackPolicy(context.Context, string, int) (*domainrecommendation.Policy, error) {
	return nil, errors.New("broad rollback unavailable")
}

func (r *rolloutMemoryRepo) DisablePolicy(_ context.Context, _ string, version int) (*domainrecommendation.Policy, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	policy := r.policies[version]
	if policy == nil {
		return nil, false, domainrecommendation.ErrPolicyNotFound
	}
	replayed := !policy.Enabled
	policy.Enabled = false
	return policy.Clone(), replayed, nil
}

func (r *rolloutMemoryRepo) ListEnabledPolicies(ctx context.Context, scene string) ([]*domainrecommendation.Policy, error) {
	policies, err := r.ListPolicies(ctx, scene)
	if err != nil {
		return nil, err
	}
	output := policies[:0]
	for _, policy := range policies {
		if policy.Enabled {
			output = append(output, policy)
		}
	}
	return output, nil
}

func (r *rolloutMemoryRepo) ListPolicies(context.Context, string) ([]*domainrecommendation.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	output := make([]*domainrecommendation.Policy, 0, len(r.policies))
	for _, policy := range r.policies {
		output = append(output, policy.Clone())
	}
	sort.Slice(output, func(i, j int) bool { return output[i].Version > output[j].Version })
	return output, nil
}

func TestRunnerPlanCreateActivateStatusDisableLifecycle(t *testing.T) {
	repo := newRolloutMemoryRepo(t)
	service := applicationrecommendation.NewSessionSemanticRolloutService(repo, func() time.Time { return time.Unix(100, 0).UTC() })
	config := rolloutTestConfig(t, ActionPlan)
	ready := false
	evidenceCalls := 0
	runner := NewRunner(
		service,
		func(string) (*shadowevaluation.Report, string, error) {
			evidenceCalls++
			return &shadowevaluation.Report{ContractKey: config.Contract.Key()}, strings.Repeat("a", 64), nil
		},
		func(context.Context, string, time.Duration, int64) (bool, error) { return ready, nil },
	)
	plan, err := runner.Execute(context.Background(), config, false, "")
	if err != nil || plan.Result != "planned" || plan.RuntimeReady || plan.ActivationReady ||
		plan.Target.Exists || plan.ConfigDigest == "" || evidenceCalls != 1 {
		t.Fatalf("plan=%#v calls=%d error=%v", plan, evidenceCalls, err)
	}
	secondPlan, err := runner.Execute(context.Background(), config, false, "")
	if err != nil || plan.ConfigDigest != secondPlan.ConfigDigest ||
		plan.Cohort.Selected != secondPlan.Cohort.Selected || !equalPreflights(plan.Preflights, secondPlan.Preflights) {
		t.Fatalf("plan=%#v second=%#v error=%v", plan, secondPlan, err)
	}

	config.Action = ActionCreate
	blocked, err := runner.Execute(context.Background(), config, true, "false")
	if !errors.Is(err, ErrOperatorBlocked) || blocked.Result != "blocked" || repo.policies[3] != nil {
		t.Fatalf("blocked=%#v error=%v", blocked, err)
	}
	created, err := runner.Execute(context.Background(), config, true, "true")
	if err != nil || created.Result != "success" || !created.Target.Exists || created.Target.Enabled {
		t.Fatalf("created=%#v error=%v", created, err)
	}
	replayedCreate, err := runner.Execute(context.Background(), config, true, "true")
	if err != nil || replayedCreate.Result != "replay" || !replayedCreate.MutationReplayed {
		t.Fatalf("replayed=%#v error=%v", replayedCreate, err)
	}

	config.Action = ActionActivate
	blockedActivation, err := runner.Execute(context.Background(), config, true, "true")
	if !errors.Is(err, applicationrecommendation.ErrSessionSemanticRolloutBlocked) || blockedActivation.Result != "blocked" {
		t.Fatalf("blocked activation=%#v error=%v", blockedActivation, err)
	}
	ready = true
	activated, err := runner.Execute(context.Background(), config, true, "true")
	if err != nil || activated.Result != "success" || !activated.Target.Enabled || !activated.ActivationReady {
		t.Fatalf("activated=%#v error=%v", activated, err)
	}

	config.Action = ActionStatus
	status, err := runner.Execute(context.Background(), config, false, "")
	if err != nil || status.Result != "success" || !status.Target.Enabled {
		t.Fatalf("status=%#v error=%v", status, err)
	}

	config = rolloutTestConfig(t, ActionDisable)
	beforeEvidenceCalls := evidenceCalls
	disabled, err := runner.Execute(context.Background(), config, true, "true")
	if err != nil || disabled.Result != "success" || disabled.Target.Enabled || evidenceCalls != beforeEvidenceCalls {
		t.Fatalf("disabled=%#v calls=%d error=%v", disabled, evidenceCalls, err)
	}
	replayedDisable, err := runner.Execute(context.Background(), config, true, "true")
	if err != nil || replayedDisable.Result != "replay" {
		t.Fatalf("replayed disable=%#v error=%v", replayedDisable, err)
	}
}

func TestRunnerRejectsEvidenceReadinessAndTargetConflicts(t *testing.T) {
	repo := newRolloutMemoryRepo(t)
	service := applicationrecommendation.NewSessionSemanticRolloutService(repo, nil)
	config := rolloutTestConfig(t, ActionPlan)
	runner := NewRunner(
		service,
		func(string) (*shadowevaluation.Report, string, error) {
			return nil, "", shadowevaluation.ErrInvalidReport
		},
		func(context.Context, string, time.Duration, int64) (bool, error) { return false, nil },
	)
	report, err := runner.Execute(context.Background(), config, false, "")
	if !errors.Is(err, ErrOperatorBlocked) || report.Failure != "shadow_evidence" {
		t.Fatalf("report=%#v error=%v", report, err)
	}
	runner = NewRunner(
		service,
		func(string) (*shadowevaluation.Report, string, error) {
			return &shadowevaluation.Report{ContractKey: config.Contract.Key()}, strings.Repeat("b", 64), nil
		},
		func(context.Context, string, time.Duration, int64) (bool, error) {
			return false, ErrInvalidReadinessResponse
		},
	)
	report, err = runner.Execute(context.Background(), config, false, "")
	if !errors.Is(err, ErrOperatorBlocked) || report.Failure != "runtime_readiness" {
		t.Fatalf("report=%#v error=%v", report, err)
	}
}

func TestOperatorReportIsPermissionRestrictedAndSecretFree(t *testing.T) {
	report := OperatorReport{
		Schema: OperatorReportSchemaV1, ToolVersion: OperatorToolVersionV1,
		Action: "plan", Mode: "read_only", Result: "planned", TargetVersion: 3,
		ConfigDigest: strings.Repeat("a", 64), ShadowReportSHA256: strings.Repeat("b", 64),
		Preflights: []Preflight{{Name: "configuration", Result: "success"}},
	}
	var output bytes.Buffer
	path := filepath.Join(t.TempDir(), "rollout-report.json")
	if err := WriteReport(&output, path, report); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, output.Bytes()) || bytes.Contains(payload, []byte("postgres://")) ||
		bytes.Contains(payload, []byte("raw error")) || bytes.Contains(payload, []byte("/tmp/shadow")) {
		t.Fatal("operator report was unstable or exposed sensitive input")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v error=%v", info.Mode().Perm(), err)
	}
	first := append([]byte(nil), payload...)
	if err := WriteReport(ioDiscard{}, path, report); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if !bytes.Equal(first, second) {
		t.Fatal("identical report was not byte stable")
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(payload []byte) (int, error) { return len(payload), nil }

func rolloutTestConfig(t testing.TB, action Action) Config {
	t.Helper()
	profile, err := multimodalprofile.Resolve(multimodalprofile.TongyiFlashSnapshotProfile)
	if err != nil {
		t.Fatal(err)
	}
	return Config{
		Action: action, PostgresDSN: "postgres://frux:secret@127.0.0.1:5432/frux?sslmode=disable",
		MetricsEndpoint: "http://127.0.0.1:8080/metrics", ShadowReportPath: "/tmp/shadow.json",
		SourceVersion: 2, TargetVersion: 3, RolloutPercentage: 1,
		HTTPTimeout: time.Second, MaxResponseBytes: 1 << 20,
		Contract: profile.Contract, ExpectedProfile: profile.ID,
	}
}

func equalPreflights(left, right []Preflight) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
