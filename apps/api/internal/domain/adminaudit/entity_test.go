package domainadminaudit

import (
	"errors"
	"strings"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
)

func TestNewFactValidatesImmutableAuditFields(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	idempotencyKeyHash, err := DigestIdempotencyKey("key-1")
	if err != nil {
		t.Fatalf("digest idempotency key: %v", err)
	}
	valid := FactInput{
		ActorID: 7, Permission: domainaccount.PermissionContentEnforce,
		Action: ActionContentEnforce, TargetType: TargetVideo, TargetID: "video-42",
		Outcome: OutcomeSuccess, RequestID: "audit-0123456789abcdef0123456789abcdef",
		IdempotencyKeyHash: idempotencyKeyHash,
		Detail: map[string]string{
			"http_method": "POST", "route": "/api/admin/videos/:videoId/enforcement",
			"reason_code": "policy_violation", "previous_status": "published", "new_status": "offline",
		},
		CreatedAt: now,
	}
	fact, err := NewFact(valid)
	if err != nil {
		t.Fatalf("NewFact() error = %v", err)
	}
	if fact.ActorID() != 7 || fact.Permission() != domainaccount.PermissionContentEnforce ||
		fact.Action() != ActionContentEnforce || fact.TargetType() != TargetVideo ||
		fact.TargetID() != "video-42" || fact.Outcome() != OutcomeSuccess ||
		fact.RequestID() != "audit-0123456789abcdef0123456789abcdef" ||
		fact.IdempotencyKeyHash() != idempotencyKeyHash ||
		!fact.CreatedAt().Equal(now) {
		t.Fatalf("unexpected fact: %+v", fact)
	}

	detail := fact.Detail()
	detail["reason_code"] = "mutated"
	if fact.Detail()["reason_code"] != "policy_violation" {
		t.Fatal("detail must be returned as an immutable copy")
	}

	tests := []struct {
		name   string
		change func(*FactInput)
		err    error
	}{
		{name: "invalid actor", change: func(input *FactInput) { input.ActorID = 0 }, err: ErrInvalidActorID},
		{name: "invalid permission", change: func(input *FactInput) { input.Permission = "root" }, err: ErrInvalidPermission},
		{name: "invalid action", change: func(input *FactInput) { input.Action = "custom" }, err: ErrInvalidAction},
		{name: "permission does not match action", change: func(input *FactInput) {
			input.Permission = domainaccount.PermissionAuditRead
		}, err: ErrInvalidPermission},
		{name: "invalid target", change: func(input *FactInput) { input.TargetType = "database" }, err: ErrInvalidTargetType},
		{name: "target does not match action", change: func(input *FactInput) {
			input.TargetType = TargetAuditTrail
		}, err: ErrInvalidTargetType},
		{name: "empty target id", change: func(input *FactInput) { input.TargetID = " " }, err: ErrInvalidTargetID},
		{name: "invalid outcome", change: func(input *FactInput) { input.Outcome = "ignored" }, err: ErrInvalidOutcome},
		{name: "empty request id", change: func(input *FactInput) { input.RequestID = "" }, err: ErrInvalidRequestID},
		{name: "untrusted request id", change: func(input *FactInput) {
			input.RequestID = "caller-controlled"
		}, err: ErrInvalidRequestID},
		{name: "raw idempotency key", change: func(input *FactInput) {
			input.IdempotencyKeyHash = "secret-retry-key"
		}, err: ErrInvalidIdempotencyKeyHash},
		{name: "unsupported detail key", change: func(input *FactInput) {
			input.Detail = map[string]string{"access_token": "secret"}
		}, err: ErrInvalidDetail},
		{name: "sensitive detail value", change: func(input *FactInput) {
			input.Detail = map[string]string{"reason_code": "Bearer secret"}
		}, err: ErrInvalidDetail},
		{name: "free form detail value", change: func(input *FactInput) {
			input.Detail = map[string]string{"reason_code": "contains spaces"}
		}, err: ErrInvalidDetail},
		{name: "misleading success transition", change: func(input *FactInput) {
			input.Detail = map[string]string{
				"http_method": "POST", "route": "/api/admin/videos/:videoId/enforcement",
				"reason_code": "policy_violation", "previous_status": "offline", "new_status": "published",
			}
		}, err: ErrInvalidDetail},
		{name: "denied fact contains mutation result", change: func(input *FactInput) {
			input.Outcome = OutcomeDenied
		}, err: ErrInvalidDetail},
		{name: "detail value too long", change: func(input *FactInput) {
			input.Detail = map[string]string{"reason_code": strings.Repeat("x", MaxDetailValueLength+1)}
		}, err: ErrInvalidDetail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := valid
			input.Detail = cloneDetail(valid.Detail)
			tt.change(&input)
			if _, err := NewFact(input); !errors.Is(err, tt.err) {
				t.Fatalf("NewFact() error = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestNewFactRejectsOversizedDetailWithoutTruncation(t *testing.T) {
	detail := make(map[string]string, MaxDetailEntries+1)
	for index := 0; index < MaxDetailEntries+1; index++ {
		detail["key-"+string(rune('a'+index))] = "value"
	}
	_, err := NewFact(FactInput{
		ActorID: 1, Permission: domainaccount.PermissionAuditRead,
		Action: ActionAuditQuery, TargetType: TargetAuditTrail, TargetID: "events",
		Outcome: OutcomeDenied, RequestID: "audit-fedcba9876543210fedcba9876543210",
		Detail: detail, CreatedAt: time.Now().UTC(),
	})
	if !errors.Is(err, ErrDetailTooLarge) {
		t.Fatalf("NewFact() error = %v, want ErrDetailTooLarge", err)
	}
}

func TestRequestIDAndIdempotencyKeyDigestAreOpaque(t *testing.T) {
	requestID := NewRequestID()
	if !requestIDPattern.MatchString(requestID) {
		t.Fatalf("NewRequestID() = %q", requestID)
	}
	first, err := DigestIdempotencyKey("same-key")
	if err != nil {
		t.Fatalf("digest first key: %v", err)
	}
	second, err := DigestIdempotencyKey("same-key")
	if err != nil {
		t.Fatalf("digest second key: %v", err)
	}
	if first != second || first == "same-key" || !idempotencyKeyHashPattern.MatchString(first) {
		t.Fatalf("unexpected idempotency key digest: first=%q second=%q", first, second)
	}
}

func TestGovernanceAuditSupportsUpdateRollbackAndProtectedReads(t *testing.T) {
	now := time.Date(2026, 8, 6, 14, 0, 0, 0, time.UTC)
	for _, input := range []FactInput{
		{
			ActorID: 1, Permission: domainaccount.PermissionGovernanceExecute,
			Action: ActionGovernanceExecute, TargetType: TargetGovernanceControl,
			TargetID: "feed.preload.enabled", Outcome: OutcomeSuccess,
			RequestID: NewRequestID(), CreatedAt: now,
			Detail: map[string]string{
				"http_method": "PATCH", "route": "/api/admin/governance/controls/:key",
				"operation": "update", "reason_code": "governance_changed",
				"previous_revision": "0", "new_revision": "1",
			},
		},
		{
			ActorID: 1, Permission: domainaccount.PermissionGovernanceExecute,
			Action: ActionGovernanceExecute, TargetType: TargetGovernanceControl,
			TargetID: "feed.preload.enabled", Outcome: OutcomeSuccess,
			RequestID: NewRequestID(), CreatedAt: now,
			Detail: map[string]string{
				"http_method": "POST", "route": "/api/admin/governance/controls/:key/rollback",
				"operation": "rollback", "reason_code": "governance_changed",
				"previous_revision": "2", "new_revision": "3",
			},
		},
		{
			ActorID: 1, Permission: domainaccount.PermissionGovernanceExecute,
			Action: ActionGovernanceExecute, TargetType: TargetGovernanceControl,
			TargetID: "controls", Outcome: OutcomeDenied,
			RequestID: NewRequestID(), CreatedAt: now,
			Detail: map[string]string{
				"http_method": "GET", "route": "/api/admin/governance/controls",
				"reason_code": "permission_denied",
			},
		},
	} {
		if _, err := NewFact(input); err != nil {
			t.Fatalf("valid governance audit rejected: %v detail=%#v", err, input.Detail)
		}
	}
}

func TestDeadLetterReplayAuditCapturesIdentityAndFailure(t *testing.T) {
	base := FactInput{
		ActorID: 9, Permission: domainaccount.PermissionGovernanceExecute,
		Action: ActionDeadLetterReplay, TargetType: TargetDeadLetterMessage,
		TargetID: "action-1", RequestID: NewRequestID(),
		CreatedAt: time.Date(2026, 8, 6, 15, 0, 0, 0, time.UTC),
		Detail: map[string]string{
			"http_method":       "POST",
			"route":             "/api/admin/dead-letter-messages/:messageId/replay",
			"reason_code":       "operator_retry",
			"queue":             "frux.interaction.action_changed.dlq.q2",
			"original_event_id": "action-1",
			"replay_id":         "replay-0123456789abcdef0123456789abcdef",
		},
	}
	base.Outcome = OutcomeSuccess
	if _, err := NewFact(base); err != nil {
		t.Fatalf("valid replay success audit rejected: %v", err)
	}
	base.Outcome = OutcomeFailure
	base.Detail = cloneDetail(base.Detail)
	base.Detail["failure_code"] = "publish_timeout"
	if _, err := NewFact(base); err != nil {
		t.Fatalf("valid replay failure audit rejected: %v", err)
	}
}
