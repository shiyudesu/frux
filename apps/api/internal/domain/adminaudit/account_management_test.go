package domainadminaudit

import (
	"errors"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
)

func TestAccountManagementAuditFactsAreStrict(t *testing.T) {
	now := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	tests := []FactInput{
		{
			ActorID: 1, Permission: domainaccount.PermissionAccountManage,
			Action: ActionAccountFreeze, TargetType: TargetUserAccount, TargetID: "42",
			Outcome: OutcomeSuccess, RequestID: NewRequestID(), CreatedAt: now,
			Detail: map[string]string{
				"http_method": "POST", "route": "/api/admin/accounts/:userId/freeze",
				"reason_code":     domainaccount.AccountReasonAbuse,
				"previous_status": "normal", "new_status": "frozen",
				"previous_version": "2", "new_version": "3", "revoked_session_count": "4",
			},
		},
		{
			ActorID: 1, Permission: domainaccount.PermissionAccountManage,
			Action: ActionAccountUnfreeze, TargetType: TargetUserAccount, TargetID: "42",
			Outcome: OutcomeSuccess, RequestID: NewRequestID(), CreatedAt: now,
			Detail: map[string]string{
				"http_method": "POST", "route": "/api/admin/accounts/:userId/unfreeze",
				"reason_code":     domainaccount.AccountReasonAppealApproved,
				"previous_status": "frozen", "new_status": "normal",
				"previous_version": "3", "new_version": "4", "revoked_session_count": "0",
			},
		},
		{
			ActorID: 1, Permission: domainaccount.PermissionAccountManage,
			Action: ActionAccountSessionsRevoke, TargetType: TargetUserAccount, TargetID: "42",
			Outcome: OutcomeSuccess, RequestID: NewRequestID(), CreatedAt: now,
			Detail: map[string]string{
				"http_method": "POST", "route": "/api/admin/accounts/:userId/sessions/revoke",
				"reason_code":     domainaccount.AccountReasonSecurityResponse,
				"previous_status": "normal", "new_status": "normal",
				"previous_version": "4", "new_version": "5", "revoked_session_count": "2",
			},
		},
	}
	for _, input := range tests {
		if _, err := NewFact(input); err != nil {
			t.Fatalf("valid account audit rejected: %v input=%+v", err, input)
		}
	}

	invalid := tests[0]
	invalid.Detail = cloneDetail(invalid.Detail)
	invalid.Detail["new_version"] = "4"
	if _, err := NewFact(invalid); !errors.Is(err, ErrInvalidDetail) {
		t.Fatalf("version jump error = %v", err)
	}
	invalid = tests[0]
	invalid.Detail = cloneDetail(invalid.Detail)
	invalid.Detail["account"] = "alice"
	if _, err := NewFact(invalid); !errors.Is(err, ErrInvalidDetail) {
		t.Fatalf("private account detail error = %v", err)
	}
	invalid = tests[0]
	invalid.Detail = cloneDetail(invalid.Detail)
	invalid.Detail["reason_code"] = domainaccount.AccountReasonAppealApproved
	if _, err := NewFact(invalid); !errors.Is(err, ErrInvalidDetail) {
		t.Fatalf("wrong reason error = %v", err)
	}
	invalid = tests[1]
	invalid.Detail = cloneDetail(invalid.Detail)
	invalid.Detail["revoked_session_count"] = "1"
	if _, err := NewFact(invalid); !errors.Is(err, ErrInvalidDetail) {
		t.Fatalf("unfreeze session count error = %v", err)
	}
}

func TestAccountManagementDeniedAuditUsesClosedSchema(t *testing.T) {
	input := FactInput{
		ActorID: 8, Permission: domainaccount.PermissionAccountManage,
		Action: ActionAccountFreeze, TargetType: TargetUserAccount, TargetID: "42",
		Outcome: OutcomeDenied, RequestID: NewRequestID(), CreatedAt: time.Now().UTC(),
		Detail: map[string]string{
			"http_method": "POST", "route": "/api/admin/accounts/:userId/freeze",
			"reason_code": "permission_denied",
		},
	}
	if _, err := NewFact(input); err != nil {
		t.Fatalf("valid denied fact rejected: %v", err)
	}
	input.Detail["previous_status"] = "normal"
	if _, err := NewFact(input); !errors.Is(err, ErrInvalidDetail) {
		t.Fatalf("denied mutation detail error = %v", err)
	}
}
