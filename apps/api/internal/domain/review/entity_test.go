package domainreview

import (
	"errors"
	"testing"
	"time"
)

func TestMachineResultNormalizesAndBoundsEvidence(t *testing.T) {
	result, err := NewMachineResult(MachineResultInput{
		CaseID: 1, VideoID: 2, ReviewVersion: 1, ResultID: "result-1",
		Provider: " Provider-A ", ModelVersion: "model-v1", PolicyVersion: 3,
		Signals: []MachineSignal{{
			Label: " Graphic Violence ", Confidence: 0.75,
			EvidenceRefs: []string{" frame://001 "},
		}},
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("NewMachineResult() error = %v", err)
	}
	if result.Provider != "provider-a" || result.Signals[0].Label != LabelGraphicViolence ||
		result.Signals[0].EvidenceRefs[0] != "frame://001" || result.PayloadHash == "" {
		t.Fatalf("normalized result = %#v", result)
	}

	_, err = NewMachineResult(MachineResultInput{
		CaseID: 1, VideoID: 2, ReviewVersion: 1, ResultID: "result-2",
		Provider: "provider", ModelVersion: "v1", PolicyVersion: 1,
		Signals: []MachineSignal{{Label: LabelSafe, Confidence: 1.1}},
	})
	if !errors.Is(err, ErrInvalidConfidence) {
		t.Fatalf("invalid confidence error = %v", err)
	}
}

func TestPolicyRejectHumanApprovePrecedenceAndUnknownLabels(t *testing.T) {
	human := 0.4
	reject := 0.8
	policy, err := NewPolicy(2, true, PolicyConfiguration{
		DefaultOutcome: OutcomeApprove,
		Rules: []LabelRule{
			{Label: LabelHate, HumanThreshold: &human, RejectThreshold: &reject},
			{Label: LabelSafe},
		},
	}, time.Now())
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	tests := []struct {
		name    string
		signals []MachineSignal
		want    string
	}{
		{"approve", []MachineSignal{{Label: LabelSafe, Confidence: 0.99}}, OutcomeApprove},
		{"human band", []MachineSignal{{Label: LabelHate, Confidence: 0.5}}, OutcomeHuman},
		{"reject wins", []MachineSignal{{Label: "unknown-model-label", Confidence: 1}, {Label: LabelHate, Confidence: 0.9}}, OutcomeReject},
		{"unknown is conservative", []MachineSignal{{Label: "new-provider-label", Confidence: 0.01}}, OutcomeHuman},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, routeErr := policy.Route(test.signals)
			if routeErr != nil || got != test.want {
				t.Fatalf("Route() = %q, %v; want %q", got, routeErr, test.want)
			}
		})
	}
}

func TestPolicyDerivesBoundedHumanPriority(t *testing.T) {
	human := 0.2
	policy, err := NewPolicy(2, true, PolicyConfiguration{
		DefaultOutcome: OutcomeApprove,
		Rules: []LabelRule{
			{Label: LabelHate, HumanThreshold: &human},
			{Label: LabelSafe},
		},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	_, low, err := policy.RouteWithPriority([]MachineSignal{{Label: LabelHate, Confidence: 0.21}})
	if err != nil {
		t.Fatal(err)
	}
	outcome, high, err := policy.RouteWithPriority([]MachineSignal{{Label: LabelHate, Confidence: 0.91}})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != OutcomeHuman || low == 0 || high <= low || !ValidPriority(low) || !ValidPriority(high) {
		t.Fatalf("human priorities low=%d high=%d outcome=%q", low, high, outcome)
	}
	_, unknown, err := policy.RouteWithPriority([]MachineSignal{{Label: "new-label", Confidence: 1}})
	if err != nil || unknown != 100 {
		t.Fatalf("unknown priority = %d err=%v", unknown, err)
	}
}

func TestPolicyReturnsTheSignalThatTriggeredRejection(t *testing.T) {
	sexualReject := 0.9
	hateReject := 0.8
	policy, err := NewPolicy(3, true, PolicyConfiguration{
		DefaultOutcome: OutcomeHuman,
		Rules: []LabelRule{
			{Label: LabelSexualContent, RejectThreshold: &sexualReject},
			{Label: LabelHate, RejectThreshold: &hateReject},
		},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	outcome, _, reason, err := policy.RouteWithPriorityAndReason([]MachineSignal{
		{Label: LabelSexualContent, Confidence: 0.2},
		{Label: LabelHate, Confidence: 0.95},
	})
	if err != nil || outcome != OutcomeReject || reason != LabelHate {
		t.Fatalf("route outcome=%q reason=%q err=%v", outcome, reason, err)
	}
}

func TestPolicyRejectsUnknownRulesAndInvalidThresholds(t *testing.T) {
	threshold := 1.2
	_, err := NewPolicy(1, true, PolicyConfiguration{
		DefaultOutcome: OutcomeApprove,
		Rules:          []LabelRule{{Label: "provider_magic", HumanThreshold: &threshold}},
	}, time.Now())
	if !errors.Is(err, ErrUnknownPolicyLabel) {
		t.Fatalf("unknown label error = %v", err)
	}

	_, err = NewPolicy(1, true, PolicyConfiguration{
		DefaultOutcome: OutcomeApprove,
		Rules:          []LabelRule{{Label: LabelSpam, HumanThreshold: &threshold}},
	}, time.Now())
	if !errors.Is(err, ErrInvalidPolicyThreshold) {
		t.Fatalf("invalid threshold error = %v", err)
	}
}

func TestInitialPolicyRoutesAllEvidenceToHuman(t *testing.T) {
	policy, err := InitialPolicy(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, signals := range [][]MachineSignal{
		{{Label: LabelSafe, Confidence: 1}},
		{{Label: LabelHate, Confidence: 1}},
		{{Label: "provider_new_label", Confidence: 1}},
	} {
		outcome, routeErr := policy.Route(signals)
		if routeErr != nil || outcome != OutcomeHuman {
			t.Fatalf("initial Route(%#v) = %q, %v", signals, outcome, routeErr)
		}
	}
}
