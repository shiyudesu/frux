package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	applicationreview "github.com/shiyudesu/frux/internal/application/review"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	interfaceshttpreview "github.com/shiyudesu/frux/internal/interfaces/http/review"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type reviewAPIMemoryRepo struct {
	mu      sync.Mutex
	cases   map[int64]*domainreview.ReviewCase
	results map[string]reviewAPIStoredResult
	policy  *domainreview.Policy
}

type reviewAPIStoredResult struct {
	hash   string
	result *domainreview.ProcessingResult
}

func newReviewAPIMemoryRepo(t *testing.T, config domainreview.PolicyConfiguration) *reviewAPIMemoryRepo {
	t.Helper()
	policy, err := domainreview.NewPolicy(1, true, config, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &reviewAPIMemoryRepo{cases: map[int64]*domainreview.ReviewCase{}, results: map[string]reviewAPIStoredResult{}, policy: policy}
}

func (r *reviewAPIMemoryRepo) addCase(videoID int64) *domainreview.ReviewCase {
	reviewCase, _ := domainreview.NewCase(videoID, 1, 1, time.Now())
	reviewCase.ID = videoID
	r.cases[reviewCase.ID] = reviewCase
	return reviewCase
}

func (r *reviewAPIMemoryRepo) CreateOrGetCase(context.Context, int64) (*domainreview.ReviewCase, bool, error) {
	return nil, false, errors.New("not used")
}

func (r *reviewAPIMemoryRepo) ListReviewableVideoIDsWithoutCase(context.Context, int) ([]int64, error) {
	return nil, nil
}

func (r *reviewAPIMemoryRepo) ProcessMachineResult(_ context.Context, input *domainreview.MachineResult) (*domainreview.ProcessingResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := input.Provider + "|" + input.ResultID
	if stored, exists := r.results[key]; exists {
		if stored.hash != input.PayloadHash {
			return nil, domainreview.ErrResultIdentityConflict
		}
		duplicate := *stored.result
		duplicate.Duplicate = true
		return &duplicate, nil
	}
	reviewCase := r.cases[input.CaseID]
	if reviewCase == nil {
		return nil, domainreview.ErrReviewCaseNotFound
	}
	if reviewCase.VideoID != input.VideoID || reviewCase.ReviewVersion != input.ReviewVersion {
		return nil, domainreview.ErrReviewSubjectStale
	}
	if reviewCase.Status != domainreview.CaseStatusOpen {
		return nil, domainreview.ErrReviewCaseNotOpen
	}
	policyOutcome, err := r.policy.Route(input.Signals)
	if err != nil {
		return nil, err
	}
	outcome, _, err := domainreview.RestrictAutomatedOutcome(input.RolloutMode, policyOutcome, 0)
	if err != nil {
		return nil, err
	}
	switch outcome {
	case domainreview.OutcomeApprove:
		reviewCase.Status = domainreview.CaseStatusApproved
	case domainreview.OutcomeReject:
		reviewCase.Status = domainreview.CaseStatusRejected
	default:
		reviewCase.Status = domainreview.CaseStatusPendingHuman
	}
	processed := &domainreview.ProcessingResult{
		Case: reviewCase,
		Decision: &domainreview.AutomatedDecision{
			ID: int64(len(r.results) + 1), CaseID: reviewCase.ID, ResultID: input.ResultID,
			Outcome: outcome, PolicyVersion: input.PolicyVersion,
			RolloutMode: input.RolloutMode, CreatedAt: time.Now(),
		},
	}
	r.results[key] = reviewAPIStoredResult{hash: input.PayloadHash, result: processed}
	return processed, nil
}

func TestReviewMachineResultAPIFlow(t *testing.T) {
	human := 0.4
	reject := 0.8
	repo := newReviewAPIMemoryRepo(t, domainreview.PolicyConfiguration{
		DefaultOutcome: domainreview.OutcomeApprove,
		Rules: []domainreview.LabelRule{
			{Label: domainreview.LabelHate, HumanThreshold: &human, RejectThreshold: &reject},
			{Label: domainreview.LabelSafe},
		},
	})
	handler := interfaceshttpreview.New(applicationreview.New(repo), nil)
	router := server.Default()
	router.PUT(
		"/internal/review/cases/:caseId/machine-results/:resultId",
		interfaceshttpmiddleware.NewInternalTokenAuth(testInternalToken),
		handler.PutMachineResult,
	)

	t.Run("requires internal token", func(t *testing.T) {
		reviewCase := repo.addCase(101)
		response := performReviewResultRequest(t, router, reviewCase.ID, "auth", "", validReviewBody(101, domainreview.LabelSafe, 1, false))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", response.Code)
		}
	})

	t.Run("strict binding rejects unknown field", func(t *testing.T) {
		reviewCase := repo.addCase(102)
		response := performReviewResultRequest(t, router, reviewCase.ID, "strict", testInternalToken, validReviewBody(102, domainreview.LabelSafe, 1, true))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("approve and replay", func(t *testing.T) {
		reviewCase := repo.addCase(103)
		body := validReviewBody(103, domainreview.LabelSafe, 1, false)
		response := performReviewResultRequest(t, router, reviewCase.ID, "approve", testInternalToken, body)
		assertReviewAPIOutcome(t, response, domainreview.OutcomeApprove, false)
		response = performReviewResultRequest(t, router, reviewCase.ID, "approve", testInternalToken, body)
		assertReviewAPIOutcome(t, response, domainreview.OutcomeApprove, true)
	})

	t.Run("reject precedence", func(t *testing.T) {
		reviewCase := repo.addCase(104)
		response := performReviewResultRequest(t, router, reviewCase.ID, "reject", testInternalToken, validReviewBody(104, domainreview.LabelHate, 0.9, false))
		assertReviewAPIOutcome(t, response, domainreview.OutcomeReject, false)
	})

	t.Run("human band and unknown label", func(t *testing.T) {
		reviewCase := repo.addCase(105)
		response := performReviewResultRequest(t, router, reviewCase.ID, "human-band", testInternalToken, validReviewBody(105, domainreview.LabelHate, 0.5, false))
		assertReviewAPIOutcome(t, response, domainreview.OutcomeHuman, false)
		reviewCase = repo.addCase(106)
		response = performReviewResultRequest(t, router, reviewCase.ID, "human-unknown", testInternalToken, validReviewBody(106, "provider_new_label", 0.01, false))
		assertReviewAPIOutcome(t, response, domainreview.OutcomeHuman, false)
	})

	t.Run("rollout modes restrict policy outcomes", func(t *testing.T) {
		reviewCase := repo.addCase(109)
		response := performReviewResultRequest(
			t, router, reviewCase.ID, "observe", testInternalToken,
			reviewBodyWithMode(109, domainreview.LabelSafe, 1, "observe"),
		)
		assertReviewAPIOutcome(t, response, domainreview.OutcomeHuman, false)
		var payload struct {
			RolloutMode string `json:"rollout_mode"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil ||
			payload.RolloutMode != domainreview.ModerationModeObserve {
			t.Fatalf("observe payload = %#v err=%v", payload, err)
		}

		reviewCase = repo.addCase(110)
		response = performReviewResultRequest(
			t, router, reviewCase.ID, "approve-only", testInternalToken,
			reviewBodyWithMode(110, domainreview.LabelHate, 0.9, "approve_only"),
		)
		assertReviewAPIOutcome(t, response, domainreview.OutcomeHuman, false)
	})

	t.Run("requires explicit valid provenance", func(t *testing.T) {
		reviewCase := repo.addCase(111)
		body := strings.Replace(
			validReviewBody(111, domainreview.LabelSafe, 1, false),
			`"source_kind":"test_seed",`, "", 1,
		)
		response := performReviewResultRequest(
			t, router, reviewCase.ID, "missing-source", testInternalToken, body,
		)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("missing source status = %d body=%s", response.Code, response.Body.String())
		}
		reviewCase = repo.addCase(112)
		body = strings.Replace(
			validReviewBody(112, domainreview.LabelSafe, 1, false),
			"2026-08-01T00:00:00Z", "2999-01-01T00:00:00Z", 1,
		)
		response = performReviewResultRequest(
			t, router, reviewCase.ID, "future-time", testInternalToken, body,
		)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("future generated time status = %d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("same identity different payload conflicts", func(t *testing.T) {
		reviewCase := repo.addCase(107)
		response := performReviewResultRequest(t, router, reviewCase.ID, "conflict", testInternalToken, validReviewBody(107, domainreview.LabelSafe, 0.8, false))
		if response.Code != http.StatusOK {
			t.Fatalf("first status = %d", response.Code)
		}
		response = performReviewResultRequest(t, router, reviewCase.ID, "conflict", testInternalToken, validReviewBody(107, domainreview.LabelSafe, 0.9, false))
		if response.Code != http.StatusConflict {
			t.Fatalf("conflict status = %d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("stale subject conflicts without decision", func(t *testing.T) {
		reviewCase := repo.addCase(108)
		response := performReviewResultRequest(t, router, reviewCase.ID, "stale", testInternalToken, validReviewBody(999, domainreview.LabelSafe, 1, false))
		if response.Code != http.StatusConflict {
			t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
		}
	})
}

func validReviewBody(videoID int64, label string, confidence float64, unknown bool) string {
	extra := ""
	if unknown {
		extra = `,"unexpected":true`
	}
	return reviewBodyWithModeAndExtra(videoID, label, confidence, "enforce", extra)
}

func reviewBodyWithMode(videoID int64, label string, confidence float64, mode string) string {
	return reviewBodyWithModeAndExtra(videoID, label, confidence, mode, "")
}

func reviewBodyWithModeAndExtra(
	videoID int64,
	label string,
	confidence float64,
	mode string,
	extra string,
) string {
	return fmt.Sprintf(`{"video_id":%d,"review_version":1,"provider":"provider","model_version":"model-v1","source_kind":"test_seed","generated_at":"2026-08-01T00:00:00Z","rollout_mode":%q,"policy_version":1,"signals":[{"label":%q,"confidence":%g,"evidence_refs":["frame://1"]}]%s}`,
		videoID, mode, label, confidence, extra)
}

func performReviewResultRequest(t *testing.T, router *server.Hertz, caseID int64, resultID, token, body string) *ut.ResponseRecorder {
	t.Helper()
	headers := []ut.Header{{Key: "Content-Type", Value: "application/json"}}
	if token != "" {
		headers = append(headers, ut.Header{Key: "X-Internal-Token", Value: token})
	}
	return ut.PerformRequest(
		router.Engine, http.MethodPut,
		fmt.Sprintf("/internal/review/cases/%d/machine-results/%s", caseID, resultID),
		&ut.Body{Body: strings.NewReader(body), Len: len(body)}, headers...,
	)
}

func assertReviewAPIOutcome(t *testing.T, response *ut.ResponseRecorder, outcome string, duplicate bool) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Outcome   string `json:"outcome"`
		Duplicate bool   `json:"duplicate"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Outcome != outcome || payload.Duplicate != duplicate {
		t.Fatalf("payload = %#v", payload)
	}
}
