package applicationacceptance

import "strings"

const BillableAcknowledgementEnvironment = "FRUX_ACCEPTANCE_ALLOW_BILLABLE"

type ExecutionDecision struct {
	Mode                Mode `json:"mode"`
	ExecuteRequested    bool `json:"execute_requested"`
	EnvironmentApproved bool `json:"environment_approved"`
	Confirmed           bool `json:"confirmed"`
}

func DecideExecution(execute bool, environmentValue string) ExecutionDecision {
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
