package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	applicationadminaudit "github.com/shiyudesu/frux/internal/application/adminaudit"
	applicationkafkafailure "github.com/shiyudesu/frux/internal/application/kafkafailure"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpkafkafailure "github.com/shiyudesu/frux/internal/interfaces/http/kafkafailure"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type apiKafkaFailureInspector struct {
	route  domainkafkafailure.RecoveryRoute
	record domainkafkafailure.RetainedRecord
}

func (i apiKafkaFailureInspector) List(context.Context) ([]domainkafkafailure.TopicSummary, error) {
	return []domainkafkafailure.TopicSummary{{
		Topic: i.route.DLQTopic, ConsumerGroup: i.route.ConsumerGroup,
		Retention: i.route.Retention, PartitionCount: 1,
		RetainedEstimate: 2, EndOffset: 43, EndOffsetGrowth: 1, RecentIngress: 1,
		OldestRecordAt: i.record.Timestamp, OldestAge: time.Hour,
		Partitions: []domainkafkafailure.PartitionSummary{{
			Partition: 2, RetainedStartOffset: 41, EndOffset: 43,
			RetainedEstimate: 2, EndOffsetGrowth: 1, RecentIngress: 1,
			OldestRecordAt: i.record.Timestamp, OldestAge: time.Hour,
		}},
	}}, nil
}

func (i apiKafkaFailureInspector) Inspect(
	_ context.Context,
	topic string,
	partition int32,
	offset int64,
	_ int,
) ([]domainkafkafailure.RecordDiagnostic, error) {
	if topic != i.route.DLQTopic {
		return nil, domainkafkafailure.ErrTopicNotAllowed
	}
	if partition != 2 {
		return nil, domainkafkafailure.ErrInvalidPartition
	}
	if offset < 41 || offset > 42 {
		return nil, domainkafkafailure.ErrRecordNotFound
	}
	return []domainkafkafailure.RecordDiagnostic{{
		Coordinate: domainkafkafailure.Coordinate{
			Topic: topic, Partition: partition, Offset: offset,
		},
		Timestamp:       i.record.Timestamp,
		SourceTopic:     i.record.Metadata.SourceTopic,
		SourcePartition: i.record.Metadata.SourcePartition,
		SourceOffset:    i.record.Metadata.SourceOffset,
		ConsumerGroup:   i.record.Metadata.ConsumerGroup,
		EventID:         i.record.Metadata.EventID,
		SchemaVersion:   i.record.Metadata.SchemaVersion,
		FailureClass:    i.record.Metadata.FailureClass,
		Attempt:         i.record.Metadata.Attempt,
		FirstFailureAt:  i.record.Metadata.FirstFailureAt,
		LatestFailureAt: i.record.Metadata.LatestFailureAt,
		NotBefore:       i.record.Metadata.NotBefore,
		KeyBytes:        len(i.record.Key), KeySHA256: "redacted-key-hash",
		PayloadBytes:  len(i.record.Value),
		PayloadSHA256: i.record.Metadata.PayloadSHA256,
		ContentType:   "application/json", JSONValid: true,
		JSONFields: []string{"event_id"},
	}}, nil
}

func (i apiKafkaFailureInspector) Fetch(
	_ context.Context,
	coordinate domainkafkafailure.Coordinate,
) (domainkafkafailure.RetainedRecord, error) {
	if coordinate.Topic != i.route.DLQTopic || coordinate.Partition != 2 {
		return domainkafkafailure.RetainedRecord{}, domainkafkafailure.ErrRecordNotFound
	}
	if coordinate.Offset < 41 || coordinate.Offset > 43 {
		return domainkafkafailure.RetainedRecord{}, domainkafkafailure.ErrRecordNotFound
	}
	record := i.record
	record.Coordinate = coordinate
	return record, nil
}

type apiKafkaFailureRegistry struct {
	route domainkafkafailure.RecoveryRoute
}

func (r apiKafkaFailureRegistry) RouteForDLQ(
	topic string,
) (domainkafkafailure.RecoveryRoute, error) {
	if topic != r.route.DLQTopic {
		return domainkafkafailure.RecoveryRoute{}, domainkafkafailure.ErrTopicNotAllowed
	}
	return r.route, nil
}

type apiKafkaFailureValidator struct {
	eventID string
	schema  int
}

func (v apiKafkaFailureValidator) Validate(
	string, []byte, []byte,
) (string, int, error) {
	return v.eventID, v.schema, nil
}

type apiKafkaFailurePublisher struct {
	mu          sync.Mutex
	calls       int
	failOffset  int64
	evidence    domainkafkafailure.ReplayEvidence
	verifyCalls int
}

func (p *apiKafkaFailurePublisher) PublishReplay(
	_ context.Context,
	_ domainkafkafailure.RecoveryRoute,
	record domainkafkafailure.RetainedRecord,
	_ string,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if record.Coordinate.Offset == p.failOffset {
		return domainkafkafailure.ErrReplayPublishRejected
	}
	return nil
}

func (p *apiKafkaFailurePublisher) VerifyReplay(
	context.Context,
	domainkafkafailure.RecoveryRoute,
	string,
	time.Time,
) (domainkafkafailure.ReplayEvidence, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.verifyCalls++
	if p.evidence.Status == "" {
		return domainkafkafailure.ReplayEvidence{},
			domainkafkafailure.ErrReplayEvidenceUnavailable
	}
	return p.evidence, nil
}

type apiKafkaFailureLedger struct {
	mu               sync.Mutex
	results          map[string]*domainkafkafailure.ReplayResult
	requestHash      map[string]string
	facts            []*domainadminaudit.Fact
	finalizeFailures int
}

func (l *apiKafkaFailureLedger) Execute(
	_ context.Context,
	command domainkafkafailure.ReplayCommand,
	operation func() applicationkafkafailure.ReplayCompletion,
	reconcile ...applicationkafkafailure.ReplayReconciliation,
) (*domainkafkafailure.ReplayResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.results == nil {
		l.results = make(map[string]*domainkafkafailure.ReplayResult)
		l.requestHash = make(map[string]string)
	}
	key := strconv.FormatInt(command.ActorID, 10) + "|" + command.IdempotencyFingerprint
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
			if completion.AuditFact == nil {
				return &result, domainkafkafailure.ErrReplayPersistence
			}
			l.facts = append(l.facts, completion.AuditFact)
			reconciled := apiReplayResult(pendingCommand, completion)
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
	if completion.AuditFact == nil {
		return nil, domainkafkafailure.ErrReplayPersistence
	}
	if l.finalizeFailures > 0 {
		l.finalizeFailures--
		copyResult := *pending
		return &copyResult, errors.New("forced replay finalization failure")
	}
	l.facts = append(l.facts, completion.AuditFact)
	result := apiReplayResult(command, completion)
	l.results[key] = result
	copyResult := *result
	return &copyResult, nil
}

func apiReplayResult(
	command domainkafkafailure.ReplayCommand,
	completion applicationkafkafailure.ReplayCompletion,
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

type apiKafkaDeniedRecorder struct {
	recorded chan applicationadminaudit.BuildInput
}

func (r *apiKafkaDeniedRecorder) RecordDeniedAttempt(
	_ context.Context,
	input applicationadminaudit.BuildInput,
) {
	r.recorded <- input
}

func (*apiKafkaDeniedRecorder) RecordDeniedAttemptDropped() {}

func TestKafkaDeadLetterAdminAPIFlow(t *testing.T) {
	now := time.Date(2026, 8, 10, 17, 0, 0, 0, time.UTC)
	value := []byte(`{"event_id":"event-video-42","secret":"must-not-leak"}`)
	route := domainkafkafailure.RecoveryRoute{
		DLQTopic:      "frux.feed.video-published.dlq.v1",
		ConsumerGroup: "feed_video_published_active",
		SourceTopic:   "frux.video.published.v1",
		ReplayTopic:   "frux.video.published.v1",
		MaxAttempt:    6, Retention: 30 * 24 * time.Hour,
	}
	inspector := apiKafkaFailureInspector{
		route: route,
		record: domainkafkafailure.RetainedRecord{
			Coordinate: domainkafkafailure.Coordinate{
				Topic: route.DLQTopic, Partition: 2, Offset: 41,
			},
			Timestamp: now.Add(-time.Hour),
			Key:       []byte("secret-key"), Value: value,
			Metadata: domainkafkafailure.RecoveryMetadata{
				SourceTopic: route.SourceTopic, SourcePartition: 1, SourceOffset: 29,
				EventID: "event-video-42", SchemaVersion: 1,
				ConsumerGroup: route.ConsumerGroup, Attempt: 2,
				FailureClass:   "terminal_domain",
				FirstFailureAt: now.Add(-time.Hour), LatestFailureAt: now.Add(-time.Minute),
				NotBefore: now, PayloadSHA256: domainkafkafailure.PayloadSHA256(value),
			},
		},
	}
	publisher := &apiKafkaFailurePublisher{failOffset: 42}
	ledger := &apiKafkaFailureLedger{}
	var replaySequence int
	service := applicationkafkafailure.New(
		inspector, apiKafkaFailureRegistry{route: route},
		apiKafkaFailureValidator{eventID: "event-video-42", schema: 1},
		publisher, ledger,
		applicationkafkafailure.WithClock(func() time.Time { return now }),
		applicationkafkafailure.WithReplayIDGenerator(func() string {
			replaySequence++
			if replaySequence == 1 {
				return "replay-0123456789abcdef0123456789abcdef"
			}
			return "replay-fedcba9876543210fedcba9876543210"
		}),
	)
	handler := interfaceshttpkafkafailure.New(service)
	jwtManager, err := infrajwt.NewManager("kafka-dead-letter-secret", "15m")
	if err != nil {
		t.Fatal(err)
	}
	principals := &adminAuthorizationReader{principals: map[int64]*domainaccount.AdminPrincipal{
		10: domainaccount.RestoreAdminPrincipal(10, domainaccount.StatusNormal, domainaccount.RoleOperator),
		11: domainaccount.RestoreAdminPrincipal(11, domainaccount.StatusNormal, domainaccount.RoleReviewer),
	}}
	denied := &apiKafkaDeniedRecorder{recorded: make(chan applicationadminaudit.BuildInput, 4)}
	permission := func(target string) app.HandlerFunc {
		return interfaceshttpmiddleware.NewRequireAdminPermission(
			principals, domainaccount.PermissionGovernanceExecute,
			interfaceshttpmiddleware.WithDeniedAttemptAudit(
				denied,
				domainadminaudit.ActionKafkaDeadLetterReplay,
				domainadminaudit.TargetKafkaDeadLetterRecord,
				target,
			),
		)
	}
	router := server.New(server.WithDisablePrintRoute(true))
	admin := router.Group("/api/admin", interfaceshttpmiddleware.NewAdminJWTAuth(jwtManager))
	admin.GET(
		"/kafka-dead-letters",
		permission("topics"),
		handler.List,
	)
	admin.GET(
		"/kafka-dead-letters/:topic/records",
		permission("records"),
		handler.Inspect,
	)
	admin.POST(
		"/kafka-dead-letters/:topic/records/:partition/:offset/replay",
		permission("record"),
		handler.Replay,
	)

	operatorToken := signAdminAuthorizationToken(t, jwtManager, 10, domainaccount.RoleUser)
	reviewerToken := signAdminAuthorizationToken(t, jwtManager, 11, domainaccount.RoleAdmin)
	forbidden := performKafkaFailureRequest(
		router, http.MethodGet, "/api/admin/kafka-dead-letters",
		reviewerToken, "", nil,
	)
	requireAdminAuthorizationError(
		t, forbidden, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied,
	)
	if bytes.Contains(forbidden.Body.Bytes(), []byte(route.DLQTopic)) {
		t.Fatal("permission denial leaked Kafka topic")
	}
	select {
	case attempt := <-denied.recorded:
		if attempt.ActorID != 11 ||
			attempt.Action != domainadminaudit.ActionKafkaDeadLetterReplay ||
			attempt.TargetType != domainadminaudit.TargetKafkaDeadLetterRecord {
			t.Fatalf("denied audit attribution=%+v", attempt)
		}
	case <-time.After(time.Second):
		t.Fatal("denied Kafka inspection was not audited")
	}

	list := performKafkaFailureRequest(
		router, http.MethodGet, "/api/admin/kafka-dead-letters",
		operatorToken, "", nil,
	)
	if list.Code != http.StatusOK ||
		!bytes.Contains(list.Body.Bytes(), []byte(route.DLQTopic)) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	inspect := performKafkaFailureRequest(
		router, http.MethodGet,
		"/api/admin/kafka-dead-letters/"+route.DLQTopic+"/records?partition=2&offset=41&limit=1",
		operatorToken, "", nil,
	)
	if inspect.Code != http.StatusOK ||
		!bytes.Contains(inspect.Body.Bytes(), []byte(`"payload_sha256"`)) ||
		bytes.Contains(inspect.Body.Bytes(), []byte("must-not-leak")) ||
		bytes.Contains(inspect.Body.Bytes(), []byte("secret-key")) ||
		bytes.Contains(inspect.Body.Bytes(), []byte(`"payload":`)) {
		t.Fatalf("redacted inspect status=%d body=%s", inspect.Code, inspect.Body.String())
	}
	invalid := performKafkaFailureRequest(
		router, http.MethodGet,
		"/api/admin/kafka-dead-letters/frux.video.published.v1/records?partition=-1&offset=0",
		operatorToken, "", nil,
	)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid coordinate status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	notAllowed := performKafkaFailureRequest(
		router, http.MethodGet,
		"/api/admin/kafka-dead-letters/frux.video.published.v1/records?partition=0&offset=0",
		operatorToken, "", nil,
	)
	if notAllowed.Code != http.StatusBadRequest {
		t.Fatalf("topic allowlist status=%d body=%s", notAllowed.Code, notAllowed.Body.String())
	}

	replayPath := "/api/admin/kafka-dead-letters/" + route.DLQTopic + "/records/2/41/replay"
	missingKey := performKafkaFailureRequest(
		router, http.MethodPost, replayPath, operatorToken, "",
		map[string]any{"reason": "operator_retry"},
	)
	if missingKey.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency status=%d body=%s", missingKey.Code, missingKey.Body.String())
	}
	unknownField := performKafkaFailureRequest(
		router, http.MethodPost, replayPath, operatorToken, "unknown-field-key",
		map[string]any{"reason": "operator_retry", "payload": "forbidden"},
	)
	if unknownField.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", unknownField.Code, unknownField.Body.String())
	}
	longKey := performKafkaFailureRequest(
		router, http.MethodPost, replayPath, operatorToken, strings.Repeat("x", 129),
		map[string]any{"reason": "operator_retry"},
	)
	if longKey.Code != http.StatusBadRequest {
		t.Fatalf("long idempotency key status=%d body=%s", longKey.Code, longKey.Body.String())
	}
	success := performKafkaFailureRequest(
		router, http.MethodPost, replayPath, operatorToken, "request-key",
		map[string]any{"reason": "operator_retry"},
	)
	if success.Code != http.StatusOK ||
		!bytes.Contains(success.Body.Bytes(), []byte(`"status":"succeeded"`)) ||
		bytes.Contains(success.Body.Bytes(), []byte("request-key")) {
		t.Fatalf("replay status=%d body=%s", success.Code, success.Body.String())
	}
	repeated := performKafkaFailureRequest(
		router, http.MethodPost, replayPath, operatorToken, "request-key",
		map[string]any{"reason": "operator_retry"},
	)
	if repeated.Code != http.StatusOK ||
		!bytes.Contains(repeated.Body.Bytes(), []byte(`"duplicate":true`)) ||
		publisher.calls != 1 {
		t.Fatalf("repeat status=%d calls=%d body=%s", repeated.Code, publisher.calls, repeated.Body.String())
	}
	conflict := performKafkaFailureRequest(
		router, http.MethodPost, replayPath, operatorToken, "request-key",
		map[string]any{"reason": "post_fix_replay"},
	)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	failed := performKafkaFailureRequest(
		router, http.MethodPost,
		"/api/admin/kafka-dead-letters/"+route.DLQTopic+"/records/2/42/replay",
		operatorToken, "failure-key",
		map[string]any{"reason": "incident_recovery"},
	)
	if failed.Code != http.StatusServiceUnavailable {
		t.Fatalf("failure status=%d body=%s", failed.Code, failed.Body.String())
	}

	ledger.mu.Lock()
	ledger.finalizeFailures = 1
	ledger.mu.Unlock()
	reconcilePath := "/api/admin/kafka-dead-letters/" + route.DLQTopic + "/records/2/43/replay"
	pending := performKafkaFailureRequest(
		router, http.MethodPost, reconcilePath,
		operatorToken, "reconcile-key",
		map[string]any{"reason": "operator_retry"},
	)
	if pending.Code != http.StatusServiceUnavailable {
		t.Fatalf("pending status=%d body=%s", pending.Code, pending.Body.String())
	}
	publisher.mu.Lock()
	publisher.evidence = domainkafkafailure.ReplayEvidence{
		Status:           domainkafkafailure.ReplayEvidenceFound,
		DestinationTopic: route.ReplayTopic,
		ReplayID:         "replay-fedcba9876543210fedcba9876543210",
		SourceTopic:      route.SourceTopic,
		SourcePartition:  1,
		SourceOffset:     29,
		ConsumerGroup:    route.ConsumerGroup,
		EventID:          "event-video-42",
		SchemaVersion:    1,
		PayloadSHA256:    domainkafkafailure.PayloadSHA256(value),
		KeySHA256:        domainkafkafailure.PayloadSHA256([]byte("secret-key")),
		RecordedAt:       now,
	}
	publisher.mu.Unlock()
	reconciled := performKafkaFailureRequest(
		router, http.MethodPost, reconcilePath,
		operatorToken, "reconcile-key",
		map[string]any{"reason": "operator_retry"},
	)
	if reconciled.Code != http.StatusOK ||
		!bytes.Contains(reconciled.Body.Bytes(), []byte(`"reconciled":true`)) {
		t.Fatalf("reconciled status=%d body=%s", reconciled.Code, reconciled.Body.String())
	}
	publisher.mu.Lock()
	if publisher.calls != 3 || publisher.verifyCalls != 1 {
		t.Fatalf(
			"reconciliation publishes=%d verifies=%d",
			publisher.calls, publisher.verifyCalls,
		)
	}
	publisher.mu.Unlock()

	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if len(ledger.facts) != 3 ||
		ledger.facts[0].ActorID() != 10 ||
		ledger.facts[0].Action() != domainadminaudit.ActionKafkaDeadLetterReplay ||
		ledger.facts[0].Outcome() != domainadminaudit.OutcomeSuccess ||
		ledger.facts[1].Outcome() != domainadminaudit.OutcomeFailure ||
		ledger.facts[2].Outcome() != domainadminaudit.OutcomeSuccess ||
		ledger.facts[0].IdempotencyKeyHash() == "" ||
		ledger.facts[0].IdempotencyKeyHash() == "request-key" {
		t.Fatalf("Kafka replay audit facts=%+v", ledger.facts)
	}
}

func performKafkaFailureRequest(
	router *server.Hertz,
	method, path, token, idempotencyKey string,
	body map[string]any,
) *ut.ResponseRecorder {
	headers := []ut.Header{{Key: "Authorization", Value: "Bearer " + token}}
	if idempotencyKey != "" {
		headers = append(headers, ut.Header{Key: "Idempotency-Key", Value: idempotencyKey})
	}
	var requestBody *ut.Body
	if body != nil {
		payload, _ := json.Marshal(body)
		headers = append(headers, ut.Header{Key: "Content-Type", Value: "application/json"})
		requestBody = &ut.Body{Body: bytes.NewReader(payload), Len: len(payload)}
	}
	return ut.PerformRequest(router.Engine, method, path, requestBody, headers...)
}
