package applicationacceptance

import (
	"testing"
	"time"
)

func TestSessionSemanticReportIsVersionedAndZeroCall(t *testing.T) {
	report := NewSessionSemanticReport("session-run", ModeExecution, time.Unix(1, 0), true)
	if report.Version != SessionSemanticReportVersion || report.Kind != SessionSemanticReportKind ||
		report.ExternalModelCalls != 0 || len(report.Stages) != len(SessionSemanticExecutionStages)+1 ||
		report.Cleanup == nil || !report.Cleanup.Requested {
		t.Fatalf("report=%#v", report)
	}
}

func TestSessionSemanticMutationRequiresBothGates(t *testing.T) {
	for _, test := range []struct {
		execute bool
		env     string
		want    bool
	}{
		{},
		{execute: true},
		{env: "true"},
		{execute: true, env: " TRUE ", want: true},
	} {
		decision := DecideSessionSemanticMutation(test.execute, test.env)
		if decision.Confirmed != test.want || (test.want && decision.Mode != ModeExecution) ||
			(!test.want && decision.Mode != ModeValidation) {
			t.Fatalf("decision=%#v", decision)
		}
	}
}
