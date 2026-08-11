package applicationkafkafailure

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"
)

type Inspector interface {
	List(ctx context.Context) ([]domainkafkafailure.TopicSummary, error)
	Inspect(
		ctx context.Context,
		topic string,
		partition int32,
		offset int64,
		limit int,
	) ([]domainkafkafailure.RecordDiagnostic, error)
	Fetch(
		ctx context.Context,
		coordinate domainkafkafailure.Coordinate,
	) (domainkafkafailure.RetainedRecord, error)
}

type Registry interface {
	RouteForDLQ(topic string) (domainkafkafailure.RecoveryRoute, error)
}

type ContractValidator interface {
	Validate(
		sourceTopic string,
		key, value []byte,
	) (eventID string, schemaVersion int, err error)
}

type Publisher interface {
	PublishReplay(
		ctx context.Context,
		route domainkafkafailure.RecoveryRoute,
		record domainkafkafailure.RetainedRecord,
		replayID string,
	) error
	VerifyReplay(
		ctx context.Context,
		route domainkafkafailure.RecoveryRoute,
		replayID string,
		requestedAt time.Time,
	) (domainkafkafailure.ReplayEvidence, error)
}

type ReplayCompletion struct {
	SourceTopic     string
	SourcePartition int32
	SourceOffset    int64
	ConsumerGroup   string
	EventID         string
	Status          domainkafkafailure.ReplayStatus
	FailureCode     domainkafkafailure.FailureCode
	CompletedAt     time.Time
	AuditFact       *domainadminaudit.Fact
}

type Ledger interface {
	Execute(
		ctx context.Context,
		command domainkafkafailure.ReplayCommand,
		operation func() ReplayCompletion,
		reconcile ...ReplayReconciliation,
	) (*domainkafkafailure.ReplayResult, error)
}

type ReplayReconciliation func(
	command domainkafkafailure.ReplayCommand,
) (ReplayCompletion, error)

type Observer interface {
	ObserveInspection(result string)
	ObserveReplay(group, result string)
	ObserveTopicSummary(summary domainkafkafailure.TopicSummary)
}

type Service struct {
	inspector Inspector
	registry  Registry
	validator ContractValidator
	publisher Publisher
	ledger    Ledger
	observer  Observer
	now       func() time.Time
	newID     func() string
}

type Option func(*Service)

type ReplayRequest struct {
	Topic          string
	Partition      int32
	Offset         int64
	ActorID        int64
	Reason         string
	IdempotencyKey string
}

func New(
	inspector Inspector,
	registry Registry,
	validator ContractValidator,
	publisher Publisher,
	ledger Ledger,
	options ...Option,
) *Service {
	service := &Service{
		inspector: inspector, registry: registry, validator: validator,
		publisher: publisher, ledger: ledger,
		now:   func() time.Time { return time.Now().UTC() },
		newID: newReplayID,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func WithClock(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

func WithReplayIDGenerator(generator func() string) Option {
	return func(service *Service) {
		if generator != nil {
			service.newID = generator
		}
	}
}

func WithObserver(observer Observer) Option {
	return func(service *Service) {
		service.observer = observer
	}
}

func (s *Service) List(ctx context.Context) ([]domainkafkafailure.TopicSummary, error) {
	if s == nil || s.inspector == nil {
		return nil, domainkafkafailure.ErrInspectionUnavailable
	}
	items, err := s.inspector.List(ctx)
	if err != nil {
		s.observeInspection("failed")
		return nil, safeInspectionError(err)
	}
	s.observeInspection("succeeded")
	for _, item := range items {
		s.observerSummary(item)
	}
	return items, nil
}

func (s *Service) Inspect(
	ctx context.Context,
	topic string,
	partition int32,
	offset int64,
	limit int,
) ([]domainkafkafailure.RecordDiagnostic, error) {
	coordinate, err := domainkafkafailure.NormalizeCoordinate(topic, partition, offset)
	if err != nil {
		return nil, err
	}
	limit, err = domainkafkafailure.NormalizeLimit(limit, 20)
	if err != nil {
		return nil, err
	}
	if s == nil || s.inspector == nil || s.registry == nil {
		return nil, domainkafkafailure.ErrInspectionUnavailable
	}
	if _, err := s.registry.RouteForDLQ(coordinate.Topic); err != nil {
		return nil, domainkafkafailure.ErrTopicNotAllowed
	}
	items, err := s.inspector.Inspect(
		ctx, coordinate.Topic, coordinate.Partition, coordinate.Offset, limit,
	)
	if err != nil {
		s.observeInspection("failed")
		return nil, safeInspectionError(err)
	}
	s.observeInspection("succeeded")
	return items, nil
}

func (s *Service) Replay(
	ctx context.Context,
	request ReplayRequest,
) (*domainkafkafailure.ReplayResult, error) {
	if s == nil || s.registry == nil || s.inspector == nil ||
		s.validator == nil || s.publisher == nil || s.ledger == nil ||
		s.newID == nil {
		return nil, domainkafkafailure.ErrReplayPublishUnavailable
	}
	coordinate, err := domainkafkafailure.NormalizeCoordinate(
		request.Topic, request.Partition, request.Offset,
	)
	if err != nil {
		return nil, err
	}
	route, err := s.registry.RouteForDLQ(coordinate.Topic)
	if err != nil || domainkafkafailure.ValidateRoute(route) != nil ||
		route.DLQTopic != coordinate.Topic {
		return nil, domainkafkafailure.ErrTopicNotAllowed
	}
	command, err := domainkafkafailure.NewReplayCommand(
		coordinate.Topic, coordinate.Partition, coordinate.Offset,
		request.ActorID, request.Reason, request.IdempotencyKey,
		s.newID(), s.now(),
	)
	if err != nil {
		return nil, err
	}

	result, err := s.ledger.Execute(ctx, command, func() ReplayCompletion {
		return s.executeReplay(ctx, route, command)
	}, func(pendingCommand domainkafkafailure.ReplayCommand) (ReplayCompletion, error) {
		return s.reconcileReplay(ctx, route, pendingCommand)
	})
	if err != nil {
		if errors.Is(err, domainkafkafailure.ErrIdempotencyConflict) {
			return nil, domainkafkafailure.ErrIdempotencyConflict
		}
		if result != nil && result.Status == domainkafkafailure.StatusPending {
			metricResult := "pending"
			if result.Duplicate {
				metricResult = "duplicate_pending"
			}
			s.observeReplay(route.ConsumerGroup, metricResult)
		}
		if errors.Is(err, domainkafkafailure.ErrReplayPending) {
			return result, domainkafkafailure.ErrReplayPending
		}
		if errors.Is(err, domainkafkafailure.ErrReplayEvidenceExpired) {
			return result, domainkafkafailure.ErrReplayEvidenceExpired
		}
		if errors.Is(err, domainkafkafailure.ErrReplayEvidenceUnavailable) {
			return result, domainkafkafailure.ErrReplayEvidenceUnavailable
		}
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return result, err
		}
		return result, errors.Join(domainkafkafailure.ErrReplayPersistence, err)
	}

	if result == nil {
		return nil, domainkafkafailure.ErrReplayPersistence
	}
	metricResult := string(result.Status)
	if result.Reconciled {
		metricResult = "reconciled_" + metricResult
	} else if result.Duplicate {
		metricResult = "duplicate_" + metricResult
	}
	s.observeReplay(route.ConsumerGroup, metricResult)
	if result.Status == domainkafkafailure.StatusFailed {
		return result, domainkafkafailure.ErrorForFailure(result.FailureCode)
	}
	if result.Status != domainkafkafailure.StatusSucceeded {
		return result, domainkafkafailure.ErrReplayPending
	}
	return result, nil
}

func (s *Service) reconcileReplay(
	ctx context.Context,
	route domainkafkafailure.RecoveryRoute,
	command domainkafkafailure.ReplayCommand,
) (ReplayCompletion, error) {
	evidence, err := s.publisher.VerifyReplay(
		ctx, route, command.ReplayID, command.RequestedAt,
	)
	if err != nil {
		return ReplayCompletion{}, err
	}
	record, err := s.inspector.Fetch(ctx, command.Coordinate)
	if err != nil {
		return ReplayCompletion{}, domainkafkafailure.ErrReplayEvidenceUnavailable
	}
	if record.Coordinate != command.Coordinate ||
		domainkafkafailure.ValidateRecord(route, record) != nil {
		return ReplayCompletion{}, domainkafkafailure.ErrReplayEvidenceUnavailable
	}
	eventID, schemaVersion, err := s.validator.Validate(
		route.SourceTopic, record.Key, record.Value,
	)
	if err != nil || eventID != record.Metadata.EventID ||
		schemaVersion != record.Metadata.SchemaVersion {
		return ReplayCompletion{}, domainkafkafailure.ErrReplayEvidenceUnavailable
	}
	switch evidence.Status {
	case domainkafkafailure.ReplayEvidenceFound:
		if domainkafkafailure.ValidateReplayEvidence(
			route, record, command.ReplayID, evidence,
		) != nil {
			return ReplayCompletion{}, domainkafkafailure.ErrReplayEvidenceUnavailable
		}
		completion := ReplayCompletion{
			SourceTopic:     record.Metadata.SourceTopic,
			SourcePartition: record.Metadata.SourcePartition,
			SourceOffset:    record.Metadata.SourceOffset,
			ConsumerGroup:   record.Metadata.ConsumerGroup,
			EventID:         record.Metadata.EventID,
			Status:          domainkafkafailure.StatusSucceeded,
			CompletedAt:     s.now(),
		}
		completion.AuditFact, err = buildReplayAudit(
			command, completion, domainadminaudit.OutcomeSuccess,
		)
		if err != nil {
			return ReplayCompletion{}, domainkafkafailure.ErrReplayPersistence
		}
		return completion, nil
	case domainkafkafailure.ReplayEvidenceAbsent:
		if domainkafkafailure.ValidateReplayAbsence(
			route, command.ReplayID, evidence,
		) != nil {
			return ReplayCompletion{}, domainkafkafailure.ErrReplayEvidenceUnavailable
		}
		return s.failedCompletion(
			command, route, record, domainkafkafailure.ErrReplayPublicationAbsent,
		), nil
	default:
		return ReplayCompletion{}, domainkafkafailure.ErrReplayEvidenceUnavailable
	}
}

func (s *Service) executeReplay(
	ctx context.Context,
	route domainkafkafailure.RecoveryRoute,
	command domainkafkafailure.ReplayCommand,
) ReplayCompletion {
	record, err := s.inspector.Fetch(ctx, command.Coordinate)
	if err != nil {
		return s.failedCompletion(command, route, domainkafkafailure.RetainedRecord{}, err)
	}
	if record.Coordinate != command.Coordinate ||
		domainkafkafailure.ValidateRecord(route, record) != nil {
		return s.failedCompletion(
			command, route, record, domainkafkafailure.ErrInvalidProvenance,
		)
	}
	eventID, schemaVersion, err := s.validator.Validate(
		route.SourceTopic, record.Key, record.Value,
	)
	if err != nil || eventID != record.Metadata.EventID ||
		schemaVersion != record.Metadata.SchemaVersion {
		return s.failedCompletion(
			command, route, record, domainkafkafailure.ErrInvalidProvenance,
		)
	}
	if err := s.publisher.PublishReplay(ctx, route, record, command.ReplayID); err != nil {
		if errors.Is(err, domainkafkafailure.ErrReplayPublishUncertain) ||
			applicationeventstream.MayHaveTransportAcknowledgement(err) {
			return ReplayCompletion{Status: domainkafkafailure.StatusPending}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = domainkafkafailure.ErrReplayPublishTimeout
		}
		return s.failedCompletion(command, route, record, err)
	}
	completedAt := s.now()
	completion := ReplayCompletion{
		SourceTopic:     record.Metadata.SourceTopic,
		SourcePartition: record.Metadata.SourcePartition,
		SourceOffset:    record.Metadata.SourceOffset,
		ConsumerGroup:   record.Metadata.ConsumerGroup,
		EventID:         record.Metadata.EventID,
		Status:          domainkafkafailure.StatusSucceeded,
		CompletedAt:     completedAt,
	}
	completion.AuditFact, err = buildReplayAudit(
		command, completion, domainadminaudit.OutcomeSuccess,
	)
	if err != nil {
		return s.failedCompletion(
			command, route, record, domainkafkafailure.ErrReplayPersistence,
		)
	}
	return completion
}

func (s *Service) failedCompletion(
	command domainkafkafailure.ReplayCommand,
	route domainkafkafailure.RecoveryRoute,
	record domainkafkafailure.RetainedRecord,
	cause error,
) ReplayCompletion {
	completion := ReplayCompletion{
		SourceTopic:   route.SourceTopic,
		ConsumerGroup: route.ConsumerGroup,
		EventID:       "unavailable",
		Status:        domainkafkafailure.StatusFailed,
		FailureCode:   failureCode(cause),
		CompletedAt:   s.now(),
	}
	if record.Metadata.SourceTopic != "" &&
		!errors.Is(cause, domainkafkafailure.ErrInvalidProvenance) {
		completion.SourceTopic = record.Metadata.SourceTopic
		completion.SourcePartition = record.Metadata.SourcePartition
		completion.SourceOffset = record.Metadata.SourceOffset
		completion.ConsumerGroup = record.Metadata.ConsumerGroup
		completion.EventID = auditIdentifier(record.Metadata.EventID)
	}
	completion.AuditFact, _ = buildReplayAudit(
		command, completion, domainadminaudit.OutcomeFailure,
	)
	return completion
}

func buildReplayAudit(
	command domainkafkafailure.ReplayCommand,
	completion ReplayCompletion,
	outcome domainadminaudit.Outcome,
) (*domainadminaudit.Fact, error) {
	detail := map[string]string{
		"http_method":       "POST",
		"route":             "/api/admin/kafka-dead-letters/:topic/records/:partition/:offset/replay",
		"reason_code":       string(command.Reason),
		"topic":             command.Coordinate.Topic,
		"partition":         strconv.FormatInt(int64(command.Coordinate.Partition), 10),
		"offset":            strconv.FormatInt(command.Coordinate.Offset, 10),
		"source_topic":      completion.SourceTopic,
		"source_partition":  strconv.FormatInt(int64(completion.SourcePartition), 10),
		"source_offset":     strconv.FormatInt(completion.SourceOffset, 10),
		"consumer_group":    completion.ConsumerGroup,
		"original_event_id": auditIdentifier(completion.EventID),
		"replay_id":         command.ReplayID,
	}
	if outcome == domainadminaudit.OutcomeFailure {
		detail["failure_code"] = string(completion.FailureCode)
	}
	return domainadminaudit.NewFact(domainadminaudit.FactInput{
		ActorID:            command.ActorID,
		Permission:         domainaccount.PermissionGovernanceExecute,
		Action:             domainadminaudit.ActionKafkaDeadLetterReplay,
		TargetType:         domainadminaudit.TargetKafkaDeadLetterRecord,
		TargetID:           auditIdentifier(command.Coordinate.String()),
		Outcome:            outcome,
		RequestID:          domainadminaudit.NewRequestID(),
		IdempotencyKeyHash: command.IdempotencyFingerprint,
		Detail:             detail,
		CreatedAt:          completion.CompletedAt,
	})
}

func auditIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && len(value) <= domainadminaudit.MaxTargetIDLength {
		valid := true
		for _, current := range value {
			if !((current >= 'a' && current <= 'z') ||
				(current >= 'A' && current <= 'Z') ||
				(current >= '0' && current <= '9') ||
				current == '.' || current == '_' || current == ':' ||
				current == '/' || current == '-') {
				valid = false
				break
			}
		}
		if valid {
			return value
		}
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func failureCode(err error) domainkafkafailure.FailureCode {
	switch {
	case errors.Is(err, domainkafkafailure.ErrRecordNotFound):
		return domainkafkafailure.FailureRecordMissing
	case errors.Is(err, domainkafkafailure.ErrRecordExpired):
		return domainkafkafailure.FailureRecordExpired
	case errors.Is(err, domainkafkafailure.ErrInvalidProvenance):
		return domainkafkafailure.FailureInvalidProvenance
	case errors.Is(err, domainkafkafailure.ErrReplayPublishTimeout):
		return domainkafkafailure.FailurePublishTimeout
	case errors.Is(err, domainkafkafailure.ErrReplayPublishRejected):
		return domainkafkafailure.FailurePublishRejected
	case errors.Is(err, domainkafkafailure.ErrReplayPublishUnavailable):
		return domainkafkafailure.FailurePublishUnavailable
	case errors.Is(err, domainkafkafailure.ErrReplayPublicationAbsent):
		return domainkafkafailure.FailurePublicationAbsent
	default:
		return domainkafkafailure.FailureInspectionFailed
	}
}

func safeInspectionError(err error) error {
	switch {
	case errors.Is(err, domainkafkafailure.ErrTopicNotAllowed),
		errors.Is(err, domainkafkafailure.ErrInvalidPartition),
		errors.Is(err, domainkafkafailure.ErrInvalidOffset),
		errors.Is(err, domainkafkafailure.ErrInvalidLimit),
		errors.Is(err, domainkafkafailure.ErrRecordNotFound),
		errors.Is(err, domainkafkafailure.ErrRecordExpired),
		errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return domainkafkafailure.ErrInspectionUnavailable
	}
}

func (s *Service) observeInspection(result string) {
	if s.observer != nil {
		s.observer.ObserveInspection(result)
	}
}

func (s *Service) observeReplay(group, result string) {
	if s.observer != nil {
		s.observer.ObserveReplay(group, result)
	}
}

func (s *Service) observerSummary(summary domainkafkafailure.TopicSummary) {
	if s.observer != nil {
		s.observer.ObserveTopicSummary(summary)
	}
}

func newReplayID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return ""
	}
	return "replay-" + hex.EncodeToString(value)
}
