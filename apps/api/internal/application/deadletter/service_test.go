package applicationdeadletter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domaindeadletter "github.com/shiyudesu/frux/internal/domain/deadletter"
)

type replayTestBroker struct {
	claim ReplayClaim
	err   error
}

func (b replayTestBroker) ClaimDeadLetter(context.Context, string, string) (ReplayClaim, error) {
	return b.claim, b.err
}

type replayTestClaim struct {
	metadata   domaindeadletter.ReplayMetadata
	publishErr error
	ackErr     error
	events     []string
	nacked     bool
}

func (c *replayTestClaim) Metadata() domaindeadletter.ReplayMetadata { return c.metadata }
func (c *replayTestClaim) Publish(context.Context, string) error {
	c.events = append(c.events, "publish")
	return c.publishErr
}
func (c *replayTestClaim) Ack() error {
	c.events = append(c.events, "ack")
	return c.ackErr
}
func (c *replayTestClaim) Nack() error {
	c.events = append(c.events, "nack")
	c.nacked = true
	return nil
}

type replayTestAudit struct {
	facts []*domainadminaudit.Fact
	err   error
	hook  func()
}

func (a *replayTestAudit) Append(_ context.Context, fact *domainadminaudit.Fact) error {
	a.facts = append(a.facts, fact)
	if a.hook != nil {
		a.hook()
	}
	return a.err
}

func TestReplayPublishesAndAuditsBeforeAcknowledging(t *testing.T) {
	claim := &replayTestClaim{metadata: domaindeadletter.ReplayMetadata{
		Queue:     "frux.interaction.action_changed.dlq.q2",
		MessageID: "action-1", OriginalEventID: "action-1",
		Exchange: "frux.interaction", RoutingKey: "interaction.action_changed",
	}}
	audit := &replayTestAudit{hook: func() {
		claim.events = append(claim.events, "audit")
	}}
	service := New(nil, replayTestBroker{claim: claim}, audit,
		WithReplayIDGenerator(func() string { return "replay-0123456789abcdef0123456789abcdef" }),
		WithClock(func() time.Time { return time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC) }),
	)
	result, err := service.Replay(context.Background(), ReplayRequest{
		Queue: claim.metadata.Queue, MessageID: claim.metadata.MessageID,
		ActorID: 7, Reason: "operator_retry",
	})
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if result.OriginalEventID != "action-1" ||
		result.ReplayID != "replay-0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected replay result: %+v", result)
	}
	if len(claim.events) != 3 ||
		claim.events[0] != "publish" ||
		claim.events[1] != "audit" ||
		claim.events[2] != "ack" {
		t.Fatalf("unexpected replay order: %#v", claim.events)
	}
	if len(audit.facts) != 1 || audit.facts[0].Outcome() != domainadminaudit.OutcomeSuccess ||
		audit.facts[0].Detail()["original_event_id"] != "action-1" {
		t.Fatalf("unexpected replay audit: %#v", audit.facts)
	}
}

func TestReplayFailureLeavesDeadLetterAvailableAndAuditsFailure(t *testing.T) {
	for _, test := range []struct {
		name         string
		publishErr   error
		expectedCode string
	}{
		{name: "publisher confirm timeout", publishErr: context.DeadlineExceeded, expectedCode: "publish_timeout"},
		{name: "publisher nack", publishErr: domaindeadletter.ErrReplayUnconfirmed, expectedCode: "publish_unconfirmed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			claim := &replayTestClaim{
				metadata: domaindeadletter.ReplayMetadata{
					Queue:     "frux.interaction.action_changed.dlq.q2",
					MessageID: "action-1", OriginalEventID: "action-1",
				},
				publishErr: test.publishErr,
			}
			audit := &replayTestAudit{}
			service := New(nil, replayTestBroker{claim: claim}, audit,
				WithReplayIDGenerator(func() string { return "replay-0123456789abcdef0123456789abcdef" }),
			)
			if _, err := service.Replay(context.Background(), ReplayRequest{
				Queue: claim.metadata.Queue, MessageID: "action-1",
				ActorID: 7, Reason: "operator_retry",
			}); !errors.Is(err, domaindeadletter.ErrReplayFailed) {
				t.Fatalf("Replay() error = %v", err)
			}
			if !claim.nacked || claim.events[len(claim.events)-1] != "nack" {
				t.Fatalf("failed replay did not requeue claim: %#v", claim.events)
			}
			if len(audit.facts) != 1 ||
				audit.facts[0].Outcome() != domainadminaudit.OutcomeFailure ||
				audit.facts[0].Detail()["failure_code"] != test.expectedCode {
				t.Fatalf("unexpected failure audit: %#v", audit.facts)
			}
		})
	}
}

func TestReplayAuditFailurePreventsDeadLetterAck(t *testing.T) {
	claim := &replayTestClaim{metadata: domaindeadletter.ReplayMetadata{
		Queue:     "frux.interaction.action_changed.dlq.q2",
		MessageID: "action-1", OriginalEventID: "action-1",
	}}
	service := New(nil, replayTestBroker{claim: claim}, &replayTestAudit{err: errors.New("postgres unavailable")},
		WithReplayIDGenerator(func() string { return "replay-0123456789abcdef0123456789abcdef" }),
	)
	if _, err := service.Replay(context.Background(), ReplayRequest{
		Queue: claim.metadata.Queue, MessageID: "action-1",
		ActorID: 7, Reason: "operator_retry",
	}); !errors.Is(err, domaindeadletter.ErrReplayAuditFailed) {
		t.Fatalf("Replay() error = %v", err)
	}
	if !claim.nacked {
		t.Fatal("audit failure acknowledged the dead-letter message")
	}
}

func TestReplayAuditHashesUnsafeOrOversizedOriginalEventID(t *testing.T) {
	originalID := "订单/" + strings.Repeat("é", domainadminaudit.MaxTargetIDLength)
	claim := &replayTestClaim{metadata: domaindeadletter.ReplayMetadata{
		Queue:     "frux.interaction.action_changed.dlq.q2",
		MessageID: "message-1", OriginalEventID: originalID,
	}}
	audit := &replayTestAudit{}
	service := New(nil, replayTestBroker{claim: claim}, audit,
		WithReplayIDGenerator(func() string { return "replay-0123456789abcdef0123456789abcdef" }),
	)
	if _, err := service.Replay(context.Background(), ReplayRequest{
		Queue: claim.metadata.Queue, MessageID: claim.metadata.MessageID,
		ActorID: 7, Reason: "operator_retry",
	}); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(audit.facts) != 1 {
		t.Fatalf("audit facts = %d, want 1", len(audit.facts))
	}
	auditID := audit.facts[0].Detail()["original_event_id"]
	if len(auditID) != len("sha256:")+64 || auditID == originalID ||
		audit.facts[0].TargetID() != auditID {
		t.Fatalf("unsafe event ID was not safely hashed: %q", auditID)
	}
}

func TestReplayDoesNotPublishBeforeSuccessAuditFactCanBeConstructed(t *testing.T) {
	claim := &replayTestClaim{metadata: domaindeadletter.ReplayMetadata{
		Queue:     "frux.interaction.action_changed.dlq.q2",
		MessageID: "action-1", OriginalEventID: "action-1",
	}}
	service := New(nil, replayTestBroker{claim: claim}, &replayTestAudit{},
		WithReplayIDGenerator(func() string { return "replay-0123456789abcdef0123456789abcdef" }),
		WithClock(func() time.Time { return time.Time{} }),
	)
	if _, err := service.Replay(context.Background(), ReplayRequest{
		Queue: claim.metadata.Queue, MessageID: claim.metadata.MessageID,
		ActorID: 7, Reason: "operator_retry",
	}); !errors.Is(err, domaindeadletter.ErrReplayAuditFailed) {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(claim.events) != 1 || claim.events[0] != "nack" {
		t.Fatalf("replay published before audit fact construction: %#v", claim.events)
	}
}
