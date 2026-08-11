package applicationkafkafailure

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"
)

type memoryInspector struct {
	mu       sync.Mutex
	record   domainkafkafailure.RetainedRecord
	fetchErr error
	fetches  int
}

func (i *memoryInspector) List(context.Context) ([]domainkafkafailure.TopicSummary, error) {
	return []domainkafkafailure.TopicSummary{{Topic: i.record.Coordinate.Topic}}, nil
}

func (i *memoryInspector) Inspect(
	context.Context, string, int32, int64, int,
) ([]domainkafkafailure.RecordDiagnostic, error) {
	return []domainkafkafailure.RecordDiagnostic{{
		Coordinate:    i.record.Coordinate,
		PayloadSHA256: i.record.Metadata.PayloadSHA256,
	}}, nil
}

func (i *memoryInspector) Fetch(
	_ context.Context,
	coordinate domainkafkafailure.Coordinate,
) (domainkafkafailure.RetainedRecord, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.fetches++
	if i.fetchErr != nil {
		return domainkafkafailure.RetainedRecord{}, i.fetchErr
	}
	if coordinate != i.record.Coordinate {
		return domainkafkafailure.RetainedRecord{}, domainkafkafailure.ErrRecordNotFound
	}
	result := i.record
	result.Key = append([]byte(nil), i.record.Key...)
	result.Value = append([]byte(nil), i.record.Value...)
	return result, nil
}

type staticRegistry struct {
	route domainkafkafailure.RecoveryRoute
}

func (r staticRegistry) RouteForDLQ(topic string) (domainkafkafailure.RecoveryRoute, error) {
	if topic != r.route.DLQTopic {
		return domainkafkafailure.RecoveryRoute{}, domainkafkafailure.ErrTopicNotAllowed
	}
	return r.route, nil
}

type staticValidator struct {
	eventID string
	schema  int
	err     error
}

func (v staticValidator) Validate(
	string, []byte, []byte,
) (string, int, error) {
	return v.eventID, v.schema, v.err
}

type memoryPublisher struct {
	mu          sync.Mutex
	err         error
	calls       int
	keys        [][]byte
	values      [][]byte
	replayIDs   []string
	block       chan struct{}
	publishSeen chan struct{}
	evidence    domainkafkafailure.ReplayEvidence
	verifyErr   error
	verifyCalls int
}

type possiblyAcknowledgedReplayError struct{}

func (possiblyAcknowledgedReplayError) Error() string {
	return "replay acknowledgement uncertain"
}

func (possiblyAcknowledgedReplayError) MayHaveAcknowledged() bool {
	return true
}

func (p *memoryPublisher) PublishReplay(
	_ context.Context,
	_ domainkafkafailure.RecoveryRoute,
	record domainkafkafailure.RetainedRecord,
	replayID string,
) error {
	p.mu.Lock()
	p.calls++
	p.keys = append(p.keys, append([]byte(nil), record.Key...))
	p.values = append(p.values, append([]byte(nil), record.Value...))
	p.replayIDs = append(p.replayIDs, replayID)
	if p.publishSeen != nil && p.calls == 1 {
		close(p.publishSeen)
	}
	p.mu.Unlock()
	if p.block != nil {
		<-p.block
	}
	return p.err
}

func (p *memoryPublisher) VerifyReplay(
	_ context.Context,
	_ domainkafkafailure.RecoveryRoute,
	_ string,
	_ time.Time,
) (domainkafkafailure.ReplayEvidence, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.verifyCalls++
	return p.evidence, p.verifyErr
}

type memoryLedger struct {
	mu          sync.Mutex
	results     map[string]*domainkafkafailure.ReplayResult
	requestHash map[string]string
	completions []ReplayCompletion
	claimErr    error
	finalizeErr error
}

func (l *memoryLedger) Execute(
	_ context.Context,
	command domainkafkafailure.ReplayCommand,
	operation func() ReplayCompletion,
	reconcile ...ReplayReconciliation,
) (*domainkafkafailure.ReplayResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.results == nil {
		l.results = make(map[string]*domainkafkafailure.ReplayResult)
		l.requestHash = make(map[string]string)
	}
	if l.claimErr != nil {
		return nil, l.claimErr
	}
	key := commandKey(command)
	if existing := l.results[key]; existing != nil {
		if l.requestHash[key] != command.RequestFingerprint {
			return nil, domainkafkafailure.ErrIdempotencyConflict
		}
		result := *existing
		result.Duplicate = true
		if result.Status == domainkafkafailure.StatusPending {
			if len(reconcile) == 0 || reconcile[0] == nil {
				return &result, domainkafkafailure.ErrReplayPending
			}
			pendingCommand := command
			pendingCommand.Coordinate = result.Coordinate
			pendingCommand.ActorID = result.ActorID
			pendingCommand.Reason = result.Reason
			pendingCommand.ReplayID = result.ReplayID
			pendingCommand.RequestedAt = result.RequestedAt
			completion, err := reconcile[0](pendingCommand)
			if err != nil {
				return &result, err
			}
			l.completions = append(l.completions, completion)
			if l.finalizeErr != nil {
				return &result, l.finalizeErr
			}
			reconciled := replayResultFromCompletion(pendingCommand, completion)
			reconciled.Duplicate = true
			reconciled.Reconciled = true
			l.results[key] = reconciled
			copyResult := *reconciled
			return &copyResult, nil
		}
		return &result, nil
	}
	pending := &domainkafkafailure.ReplayResult{
		Coordinate: command.Coordinate, ActorID: command.ActorID,
		ReplayID: command.ReplayID, Reason: command.Reason,
		Status: domainkafkafailure.StatusPending, RequestedAt: command.RequestedAt,
	}
	l.results[key] = pending
	l.requestHash[key] = command.RequestFingerprint
	completion := operation()
	if completion.Status == domainkafkafailure.StatusPending {
		copyResult := *pending
		return &copyResult, domainkafkafailure.ErrReplayPending
	}
	l.completions = append(l.completions, completion)
	if l.finalizeErr != nil {
		copyResult := *pending
		return &copyResult, l.finalizeErr
	}
	result := replayResultFromCompletion(command, completion)
	l.results[key] = result
	copyResult := *result
	return &copyResult, nil
}

func replayResultFromCompletion(
	command domainkafkafailure.ReplayCommand,
	completion ReplayCompletion,
) *domainkafkafailure.ReplayResult {
	return &domainkafkafailure.ReplayResult{
		Coordinate:      command.Coordinate,
		SourceTopic:     completion.SourceTopic,
		SourcePartition: completion.SourcePartition,
		SourceOffset:    completion.SourceOffset,
		ConsumerGroup:   completion.ConsumerGroup,
		ActorID:         command.ActorID,
		ReplayID:        command.ReplayID,
		Reason:          command.Reason,
		Status:          completion.Status,
		FailureCode:     completion.FailureCode,
		RequestedAt:     command.RequestedAt,
		CompletedAt:     completion.CompletedAt,
	}
}

func commandKey(command domainkafkafailure.ReplayCommand) string {
	return strconv.FormatInt(command.ActorID, 10) + "|" + command.IdempotencyFingerprint
}

func TestReplayPublishesUnchangedRecordAndBuildsKafkaAudit(t *testing.T) {
	service, inspector, publisher, ledger := replayFixture(t)
	result, err := service.Replay(context.Background(), ReplayRequest{
		Topic:     inspector.record.Coordinate.Topic,
		Partition: inspector.record.Coordinate.Partition,
		Offset:    inspector.record.Coordinate.Offset,
		ActorID:   9, Reason: "operator_retry", IdempotencyKey: "request-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domainkafkafailure.StatusSucceeded ||
		result.ReplayID == "" || publisher.calls != 1 ||
		string(publisher.keys[0]) != string(inspector.record.Key) ||
		string(publisher.values[0]) != string(inspector.record.Value) {
		t.Fatalf("result=%+v calls=%d", result, publisher.calls)
	}
	if len(ledger.completions) != 1 || ledger.completions[0].AuditFact == nil ||
		ledger.completions[0].AuditFact.Action() != domainadminaudit.ActionKafkaDeadLetterReplay ||
		ledger.completions[0].AuditFact.TargetType() != domainadminaudit.TargetKafkaDeadLetterRecord ||
		ledger.completions[0].AuditFact.Outcome() != domainadminaudit.OutcomeSuccess {
		t.Fatalf("unexpected completion audit: %+v", ledger.completions)
	}
}

func TestReplayPersistsBoundedFailureResults(t *testing.T) {
	tests := []struct {
		name       string
		fetchErr   error
		publishErr error
		mutate     func(*domainkafkafailure.RetainedRecord)
		wantError  error
		wantCode   domainkafkafailure.FailureCode
	}{
		{name: "timeout", publishErr: context.DeadlineExceeded, wantError: domainkafkafailure.ErrReplayPublishTimeout, wantCode: domainkafkafailure.FailurePublishTimeout},
		{name: "rejected", publishErr: domainkafkafailure.ErrReplayPublishRejected, wantError: domainkafkafailure.ErrReplayPublishRejected, wantCode: domainkafkafailure.FailurePublishRejected},
		{name: "missing", fetchErr: domainkafkafailure.ErrRecordNotFound, wantError: domainkafkafailure.ErrRecordNotFound, wantCode: domainkafkafailure.FailureRecordMissing},
		{name: "expired", fetchErr: domainkafkafailure.ErrRecordExpired, wantError: domainkafkafailure.ErrRecordExpired, wantCode: domainkafkafailure.FailureRecordExpired},
		{name: "inspection timeout", fetchErr: context.DeadlineExceeded, wantError: domainkafkafailure.ErrInspectionUnavailable, wantCode: domainkafkafailure.FailureInspectionFailed},
		{
			name: "invalid provenance",
			mutate: func(record *domainkafkafailure.RetainedRecord) {
				record.Metadata.ConsumerGroup = "other"
			},
			wantError: domainkafkafailure.ErrInvalidProvenance,
			wantCode:  domainkafkafailure.FailureInvalidProvenance,
		},
		{
			name: "non-replayable quarantine",
			mutate: func(record *domainkafkafailure.RetainedRecord) {
				record.Metadata.NonReplayable = true
				record.Metadata.FailureClass = "recovery_metadata_invalid"
			},
			wantError: domainkafkafailure.ErrInvalidProvenance,
			wantCode:  domainkafkafailure.FailureInvalidProvenance,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, inspector, publisher, ledger := replayFixture(t)
			inspector.fetchErr = test.fetchErr
			publisher.err = test.publishErr
			if test.mutate != nil {
				test.mutate(&inspector.record)
			}
			result, err := service.Replay(context.Background(), ReplayRequest{
				Topic:     inspector.record.Coordinate.Topic,
				Partition: inspector.record.Coordinate.Partition,
				Offset:    inspector.record.Coordinate.Offset,
				ActorID:   9, Reason: "incident_recovery", IdempotencyKey: "failure-key",
			})
			if !errors.Is(err, test.wantError) || result == nil ||
				result.Status != domainkafkafailure.StatusFailed ||
				result.FailureCode != test.wantCode ||
				len(ledger.completions) != 1 ||
				ledger.completions[0].AuditFact == nil ||
				ledger.completions[0].AuditFact.Outcome() != domainadminaudit.OutcomeFailure {
				t.Fatalf("result=%+v err=%v completions=%+v", result, err, ledger.completions)
			}
		})
	}
}

func TestReplayRejectsNonReplayableQuarantineBeforePublishing(t *testing.T) {
	service, inspector, publisher, _ := replayFixture(t)
	inspector.record.Metadata.NonReplayable = true
	inspector.record.Metadata.FailureClass = "recovery_metadata_invalid"
	result, err := service.Replay(context.Background(), ReplayRequest{
		Topic:     inspector.record.Coordinate.Topic,
		Partition: inspector.record.Coordinate.Partition,
		Offset:    inspector.record.Coordinate.Offset,
		ActorID:   9, Reason: "incident_recovery", IdempotencyKey: "quarantine",
	})
	if result == nil ||
		result.FailureCode != domainkafkafailure.FailureInvalidProvenance ||
		!errors.Is(err, domainkafkafailure.ErrInvalidProvenance) ||
		publisher.calls != 0 {
		t.Fatalf("result=%+v err=%v publishes=%d", result, err, publisher.calls)
	}
}

func TestReplayIdempotencyConcurrencyConflictAndLaterIntentionalReplay(t *testing.T) {
	service, inspector, publisher, _ := replayFixture(t)
	publisher.block = make(chan struct{})
	publisher.publishSeen = make(chan struct{})
	request := ReplayRequest{
		Topic:     inspector.record.Coordinate.Topic,
		Partition: inspector.record.Coordinate.Partition,
		Offset:    inspector.record.Coordinate.Offset,
		ActorID:   9, Reason: "operator_retry", IdempotencyKey: "same-key",
	}

	results := make(chan *domainkafkafailure.ReplayResult, 2)
	errs := make(chan error, 2)
	go func() {
		result, err := service.Replay(context.Background(), request)
		results <- result
		errs <- err
	}()
	<-publisher.publishSeen
	go func() {
		result, err := service.Replay(context.Background(), request)
		results <- result
		errs <- err
	}()
	close(publisher.block)
	first, second := <-results, <-results
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if publisher.calls != 1 || first.ReplayID != second.ReplayID ||
		first.Duplicate == second.Duplicate {
		t.Fatalf("calls=%d first=%+v second=%+v", publisher.calls, first, second)
	}

	conflict := request
	conflict.Reason = "post_fix_replay"
	if _, err := service.Replay(context.Background(), conflict); !errors.Is(err, domainkafkafailure.ErrIdempotencyConflict) {
		t.Fatalf("conflict error=%v", err)
	}
	intentional := request
	intentional.IdempotencyKey = "new-key"
	if _, err := service.Replay(context.Background(), intentional); err != nil {
		t.Fatal(err)
	}
	if publisher.calls != 2 {
		t.Fatalf("later intentional replay calls=%d", publisher.calls)
	}
}

func TestReplayDoesNotPublishWhenPendingClaimCommitFails(t *testing.T) {
	service, inspector, publisher, ledger := replayFixture(t)
	ledger.claimErr = errors.New("pending commit failed")
	result, err := service.Replay(context.Background(), ReplayRequest{
		Topic:     inspector.record.Coordinate.Topic,
		Partition: inspector.record.Coordinate.Partition,
		Offset:    inspector.record.Coordinate.Offset,
		ActorID:   9, Reason: "operator_retry", IdempotencyKey: "claim-failure",
	})
	if result != nil || !errors.Is(err, domainkafkafailure.ErrReplayPersistence) ||
		publisher.calls != 0 || inspector.fetches != 0 {
		t.Fatalf(
			"result=%+v err=%v publishes=%d fetches=%d",
			result, err, publisher.calls, inspector.fetches,
		)
	}
}

func TestReplayUncertainPublicationStaysPendingAndReconcilesWithoutRepublishing(t *testing.T) {
	service, inspector, publisher, ledger := replayFixture(t)
	publisher.err = possiblyAcknowledgedReplayError{}
	request := ReplayRequest{
		Topic:     inspector.record.Coordinate.Topic,
		Partition: inspector.record.Coordinate.Partition,
		Offset:    inspector.record.Coordinate.Offset,
		ActorID:   9, Reason: "operator_retry", IdempotencyKey: "uncertain-publish",
	}

	pending, err := service.Replay(context.Background(), request)
	if pending == nil || pending.Status != domainkafkafailure.StatusPending ||
		!errors.Is(err, domainkafkafailure.ErrReplayPending) ||
		publisher.calls != 1 || len(ledger.completions) != 0 {
		t.Fatalf(
			"pending=%+v err=%v publishes=%d completions=%d",
			pending, err, publisher.calls, len(ledger.completions),
		)
	}
	replayID := pending.ReplayID

	publisher.err = nil
	reconciled, err := service.Replay(context.Background(), request)
	if err != nil || reconciled == nil ||
		reconciled.Status != domainkafkafailure.StatusSucceeded ||
		!reconciled.Duplicate || !reconciled.Reconciled ||
		reconciled.ReplayID != replayID ||
		publisher.calls != 1 || publisher.verifyCalls != 1 {
		t.Fatalf(
			"reconciled=%+v err=%v publishes=%d verifies=%d",
			reconciled, err, publisher.calls, publisher.verifyCalls,
		)
	}
}

func TestReplayUncertainPublicationFinalizesOnlyAfterProvenAbsence(t *testing.T) {
	service, inspector, publisher, ledger := replayFixture(t)
	publisher.err = domainkafkafailure.ErrReplayPublishUncertain
	request := ReplayRequest{
		Topic:     inspector.record.Coordinate.Topic,
		Partition: inspector.record.Coordinate.Partition,
		Offset:    inspector.record.Coordinate.Offset,
		ActorID:   9, Reason: "operator_retry", IdempotencyKey: "uncertain-absent",
	}
	pending, err := service.Replay(context.Background(), request)
	if pending == nil || pending.Status != domainkafkafailure.StatusPending ||
		!errors.Is(err, domainkafkafailure.ErrReplayPending) {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}

	publisher.err = nil
	publisher.evidence = domainkafkafailure.ReplayEvidence{
		Status:           domainkafkafailure.ReplayEvidenceAbsent,
		DestinationTopic: "frux.feed.video-published.retry-5s.v1",
		ReplayID:         pending.ReplayID,
	}
	failed, err := service.Replay(context.Background(), request)
	if failed == nil || failed.Status != domainkafkafailure.StatusFailed ||
		failed.FailureCode != domainkafkafailure.FailurePublicationAbsent ||
		!errors.Is(err, domainkafkafailure.ErrReplayPublicationAbsent) ||
		failed.ReplayID != pending.ReplayID ||
		publisher.calls != 1 || publisher.verifyCalls != 1 ||
		len(ledger.completions) != 1 {
		t.Fatalf(
			"failed=%+v err=%v publishes=%d verifies=%d completions=%d",
			failed, err, publisher.calls, publisher.verifyCalls, len(ledger.completions),
		)
	}
}

func TestReplayUncertainPublicationKeepsPendingWhenEvidenceUnavailable(t *testing.T) {
	service, inspector, publisher, ledger := replayFixture(t)
	publisher.err = possiblyAcknowledgedReplayError{}
	request := ReplayRequest{
		Topic:     inspector.record.Coordinate.Topic,
		Partition: inspector.record.Coordinate.Partition,
		Offset:    inspector.record.Coordinate.Offset,
		ActorID:   9, Reason: "operator_retry", IdempotencyKey: "uncertain-unavailable",
	}
	pending, err := service.Replay(context.Background(), request)
	if pending == nil || !errors.Is(err, domainkafkafailure.ErrReplayPending) {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}

	publisher.err = nil
	publisher.verifyErr = domainkafkafailure.ErrReplayEvidenceUnavailable
	repeated, err := service.Replay(context.Background(), request)
	if repeated == nil || repeated.Status != domainkafkafailure.StatusPending ||
		repeated.ReplayID != pending.ReplayID || !repeated.Duplicate ||
		!errors.Is(err, domainkafkafailure.ErrReplayEvidenceUnavailable) ||
		publisher.calls != 1 || publisher.verifyCalls != 1 ||
		len(ledger.completions) != 0 {
		t.Fatalf(
			"repeated=%+v err=%v publishes=%d verifies=%d completions=%d",
			repeated, err, publisher.calls, publisher.verifyCalls, len(ledger.completions),
		)
	}
}

func TestReplayUncertainPublicationKeepsPendingWhenReconciliationIsCanceled(t *testing.T) {
	service, inspector, publisher, ledger := replayFixture(t)
	publisher.err = possiblyAcknowledgedReplayError{}
	request := ReplayRequest{
		Topic:     inspector.record.Coordinate.Topic,
		Partition: inspector.record.Coordinate.Partition,
		Offset:    inspector.record.Coordinate.Offset,
		ActorID:   9, Reason: "operator_retry", IdempotencyKey: "uncertain-canceled",
	}
	pending, err := service.Replay(context.Background(), request)
	if pending == nil || !errors.Is(err, domainkafkafailure.ErrReplayPending) {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}

	publisher.err = nil
	publisher.verifyErr = context.Canceled
	repeated, err := service.Replay(context.Background(), request)
	if repeated == nil || repeated.Status != domainkafkafailure.StatusPending ||
		!repeated.Duplicate || !errors.Is(err, context.Canceled) ||
		publisher.calls != 1 || publisher.verifyCalls != 1 ||
		len(ledger.completions) != 0 {
		t.Fatalf(
			"repeated=%+v err=%v publishes=%d verifies=%d completions=%d",
			repeated,
			err,
			publisher.calls,
			publisher.verifyCalls,
			len(ledger.completions),
		)
	}
}

func TestReplayAcknowledgedThenRepeatedRequestReconcilesWithoutRepublishing(t *testing.T) {
	service, inspector, publisher, ledger := replayFixture(t)
	ledger.finalizeErr = errors.New("finalization commit failed")
	request := ReplayRequest{
		Topic:     inspector.record.Coordinate.Topic,
		Partition: inspector.record.Coordinate.Partition,
		Offset:    inspector.record.Coordinate.Offset,
		ActorID:   9, Reason: "operator_retry", IdempotencyKey: "unknown-outcome",
	}
	result, err := service.Replay(context.Background(), request)
	if result == nil || result.Status != domainkafkafailure.StatusPending ||
		!errors.Is(err, domainkafkafailure.ErrReplayPersistence) ||
		publisher.calls != 1 {
		t.Fatalf("result=%+v err=%v publishes=%d", result, err, publisher.calls)
	}

	ledger.finalizeErr = nil
	repeated, err := service.Replay(context.Background(), request)
	if err != nil || repeated == nil ||
		repeated.Status != domainkafkafailure.StatusSucceeded ||
		!repeated.Duplicate || !repeated.Reconciled ||
		publisher.calls != 1 || publisher.verifyCalls != 1 {
		t.Fatalf(
			"repeated=%+v err=%v publishes=%d verifies=%d",
			repeated, err, publisher.calls, publisher.verifyCalls,
		)
	}
	if len(ledger.completions) != 2 ||
		ledger.completions[1].AuditFact == nil ||
		ledger.completions[1].AuditFact.Outcome() != domainadminaudit.OutcomeSuccess {
		t.Fatalf("reconciliation audit=%+v", ledger.completions)
	}
}

func TestReplayReconciliationFinalizesProvenAbsenceAndAllowsNewReplay(t *testing.T) {
	service, inspector, publisher, ledger := replayFixture(t)
	ledger.finalizeErr = errors.New("finalization commit failed")
	request := ReplayRequest{
		Topic:     inspector.record.Coordinate.Topic,
		Partition: inspector.record.Coordinate.Partition,
		Offset:    inspector.record.Coordinate.Offset,
		ActorID:   9, Reason: "operator_retry", IdempotencyKey: "absent-outcome",
	}
	if _, err := service.Replay(context.Background(), request); err == nil {
		t.Fatal("expected initial finalization failure")
	}
	ledger.finalizeErr = nil
	publisher.evidence = domainkafkafailure.ReplayEvidence{
		Status:           domainkafkafailure.ReplayEvidenceAbsent,
		DestinationTopic: "frux.feed.video-published.retry-5s.v1",
		ReplayID:         "replay-0123456789abcdef0123456789abcdef",
	}
	result, err := service.Replay(context.Background(), request)
	if result == nil || result.Status != domainkafkafailure.StatusFailed ||
		result.FailureCode != domainkafkafailure.FailurePublicationAbsent ||
		!errors.Is(err, domainkafkafailure.ErrReplayPublicationAbsent) ||
		publisher.calls != 1 {
		t.Fatalf("result=%+v err=%v publishes=%d", result, err, publisher.calls)
	}
	if len(ledger.completions) != 2 ||
		ledger.completions[1].AuditFact == nil ||
		ledger.completions[1].AuditFact.Outcome() != domainadminaudit.OutcomeFailure {
		t.Fatalf("absence audit=%+v", ledger.completions)
	}

	request.IdempotencyKey = "new-replay-after-absence"
	if _, err := service.Replay(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if publisher.calls != 2 {
		t.Fatalf("new replay publishes=%d", publisher.calls)
	}
}

func TestReplayReconciliationLeavesExpiredEvidencePending(t *testing.T) {
	service, inspector, publisher, ledger := replayFixture(t)
	ledger.finalizeErr = errors.New("finalization commit failed")
	request := ReplayRequest{
		Topic:     inspector.record.Coordinate.Topic,
		Partition: inspector.record.Coordinate.Partition,
		Offset:    inspector.record.Coordinate.Offset,
		ActorID:   9, Reason: "operator_retry", IdempotencyKey: "expired-evidence",
	}
	if _, err := service.Replay(context.Background(), request); err == nil {
		t.Fatal("expected initial finalization failure")
	}
	ledger.finalizeErr = nil
	publisher.verifyErr = domainkafkafailure.ErrReplayEvidenceExpired
	result, err := service.Replay(context.Background(), request)
	if result == nil || result.Status != domainkafkafailure.StatusPending ||
		!errors.Is(err, domainkafkafailure.ErrReplayEvidenceExpired) ||
		publisher.calls != 1 || publisher.verifyCalls != 1 {
		t.Fatalf(
			"result=%+v err=%v publishes=%d verifies=%d",
			result, err, publisher.calls, publisher.verifyCalls,
		)
	}
}

func replayFixture(
	t *testing.T,
) (*Service, *memoryInspector, *memoryPublisher, *memoryLedger) {
	t.Helper()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	value := []byte(`{"event_id":"event-video-42"}`)
	route := domainkafkafailure.RecoveryRoute{
		DLQTopic:      "frux.feed.video-published.dlq.v1",
		ConsumerGroup: "feed_video_published_active",
		SourceTopic:   "frux.video.published.v1",
		ReplayTopic:   "frux.feed.video-published.retry-5s.v1",
		ReplayTier:    1, MaxAttempt: 6, Retention: 30 * 24 * time.Hour,
	}
	inspector := &memoryInspector{record: domainkafkafailure.RetainedRecord{
		Coordinate: domainkafkafailure.Coordinate{
			Topic: route.DLQTopic, Partition: 2, Offset: 41,
		},
		Timestamp: now.Add(-time.Hour),
		Key:       []byte("video:42"), Value: value,
		Metadata: domainkafkafailure.RecoveryMetadata{
			SourceTopic: route.SourceTopic, SourcePartition: 1, SourceOffset: 29,
			EventID: "event-video-42", SchemaVersion: 1,
			ConsumerGroup: route.ConsumerGroup, Attempt: 2, Tier: 0,
			FailureClass:   "terminal_domain",
			FirstFailureAt: now.Add(-time.Hour), LatestFailureAt: now.Add(-time.Minute),
			NotBefore: now, PayloadSHA256: domainkafkafailure.PayloadSHA256(value),
		},
	}}
	publisher := &memoryPublisher{evidence: domainkafkafailure.ReplayEvidence{
		Status:           domainkafkafailure.ReplayEvidenceFound,
		DestinationTopic: route.ReplayTopic,
		ReplayID:         "replay-0123456789abcdef0123456789abcdef",
		SourceTopic:      route.SourceTopic,
		SourcePartition:  1,
		SourceOffset:     29,
		ConsumerGroup:    route.ConsumerGroup,
		EventID:          "event-video-42",
		SchemaVersion:    1,
		PayloadSHA256:    domainkafkafailure.PayloadSHA256(value),
		KeySHA256:        domainkafkafailure.PayloadSHA256([]byte("video:42")),
		RecordedAt:       now,
	}}
	ledger := &memoryLedger{}
	var sequence atomic.Int64
	service := New(
		inspector, staticRegistry{route: route},
		staticValidator{eventID: "event-video-42", schema: 1},
		publisher, ledger,
		WithClock(func() time.Time { return now }),
		WithReplayIDGenerator(func() string {
			if sequence.Add(1) == 1 {
				return "replay-0123456789abcdef0123456789abcdef"
			}
			return "replay-fedcba9876543210fedcba9876543210"
		}),
	)
	return service, inspector, publisher, ledger
}
