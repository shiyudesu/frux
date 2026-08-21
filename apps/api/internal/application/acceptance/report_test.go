package applicationacceptance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewReportIsVersionedAndPlansBoundedCalls(t *testing.T) {
	report := NewReport("run-1", ModeValidation, time.Unix(1, 0), true)
	if report.Version != ReportVersionV1 || report.Kind != ReportKindTechnicalAcceptance ||
		report.PlannedModelCalls != 3 || len(report.Stages) != len(ExecutionStages)+1 {
		t.Fatalf("report=%#v", report)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, secretName := range []string{"password", "bearer", "api_key", "hmac_secret", "raw_vector"} {
		if strings.Contains(string(encoded), secretName) {
			t.Fatalf("report schema exposed secret field %q: %s", secretName, encoded)
		}
	}
}

func TestDecideExecutionRequiresBothGates(t *testing.T) {
	for _, test := range []struct {
		execute bool
		env     string
		want    bool
	}{
		{},
		{execute: true},
		{env: "true"},
		{execute: true, env: "TRUE", want: true},
	} {
		decision := DecideExecution(test.execute, test.env)
		if decision.Confirmed != test.want || (decision.Mode == ModeExecution) != test.want {
			t.Fatalf("decision=%#v want=%v", decision, test.want)
		}
	}
}
