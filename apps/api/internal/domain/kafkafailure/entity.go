package domainkafkafailure

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	MaxTopicLength          = 249
	MaxIdempotencyKeyLength = 128
	MaxReadLimit            = 100
	MaxFailureCodeLength    = 64
	MaxGroupLength          = 128
	MaxEventIDLength        = 256
	MaxReplayIDLength       = 71
	MaxJSONFields           = 20
)

type ReplayReason string

const (
	ReasonOperatorRetry    ReplayReason = "operator_retry"
	ReasonIncidentRecovery ReplayReason = "incident_recovery"
	ReasonPostFixReplay    ReplayReason = "post_fix_replay"
	ReasonRetentionRescue  ReplayReason = "retention_rescue"
)

type ReplayStatus string

const (
	StatusPending   ReplayStatus = "pending"
	StatusSucceeded ReplayStatus = "succeeded"
	StatusFailed    ReplayStatus = "failed"
)

type FailureCode string

const (
	FailureNone               FailureCode = ""
	FailurePublishTimeout     FailureCode = "publish_timeout"
	FailurePublishRejected    FailureCode = "publish_rejected"
	FailurePublishUnavailable FailureCode = "publish_unavailable"
	FailureRecordMissing      FailureCode = "record_missing"
	FailureRecordExpired      FailureCode = "record_expired"
	FailureInvalidProvenance  FailureCode = "invalid_provenance"
	FailureInspectionFailed   FailureCode = "inspection_failed"
	FailurePublicationAbsent  FailureCode = "publication_absent"
)

type ReplayEvidenceStatus string

const (
	ReplayEvidenceFound  ReplayEvidenceStatus = "found"
	ReplayEvidenceAbsent ReplayEvidenceStatus = "absent"
)

type Coordinate struct {
	Topic     string
	Partition int32
	Offset    int64
}

type RecoveryRoute struct {
	DLQTopic      string
	ConsumerGroup string
	SourceTopic   string
	ReplayTopic   string
	ReplayTier    int
	MaxAttempt    int
	Retention     time.Duration
}

type RecoveryMetadata struct {
	SourceTopic       string
	SourcePartition   int32
	SourceOffset      int64
	EventID           string
	SchemaVersion     int
	ConsumerGroup     string
	Attempt           int
	Tier              int
	FailureClass      string
	FirstFailureAt    time.Time
	LatestFailureAt   time.Time
	NotBefore         time.Time
	PayloadSHA256     string
	ReplayID          string
	ConsumedTopic     string
	ConsumedPartition int32
	ConsumedOffset    int64
	KeySHA256         string
	MetadataCode      string
	NonReplayable     bool
}

type RetainedRecord struct {
	Coordinate Coordinate
	Timestamp  time.Time
	Key        []byte
	Value      []byte
	Metadata   RecoveryMetadata
}

type TopicSummary struct {
	Topic            string
	ConsumerGroup    string
	Retention        time.Duration
	PartitionCount   int
	RetainedEstimate int64
	EndOffset        int64
	EndOffsetGrowth  int64
	RecentIngress    int64
	OldestRecordAt   time.Time
	OldestAge        time.Duration
	Partitions       []PartitionSummary
}

type PartitionSummary struct {
	Partition           int32
	RetainedStartOffset int64
	EndOffset           int64
	RetainedEstimate    int64
	EndOffsetGrowth     int64
	RecentIngress       int64
	OldestRecordAt      time.Time
	OldestAge           time.Duration
}

type RecordDiagnostic struct {
	Coordinate        Coordinate
	Timestamp         time.Time
	SourceTopic       string
	SourcePartition   int32
	SourceOffset      int64
	ConsumerGroup     string
	EventID           string
	ReplayID          string
	SchemaVersion     int
	FailureClass      string
	Attempt           int
	FirstFailureAt    time.Time
	LatestFailureAt   time.Time
	NotBefore         time.Time
	ConsumedTopic     string
	ConsumedPartition int32
	ConsumedOffset    int64
	MetadataCode      string
	Replayable        bool
	KeyBytes          int
	KeySHA256         string
	PayloadBytes      int
	PayloadSHA256     string
	ContentType       string
	JSONValid         bool
	JSONFields        []string
}

type ReplayCommand struct {
	Coordinate             Coordinate
	ActorID                int64
	Reason                 ReplayReason
	IdempotencyFingerprint string
	RequestFingerprint     string
	ReplayID               string
	RequestedAt            time.Time
}

type ReplayResult struct {
	Coordinate      Coordinate
	SourceTopic     string
	SourcePartition int32
	SourceOffset    int64
	ConsumerGroup   string
	ActorID         int64
	ReplayID        string
	Reason          ReplayReason
	Status          ReplayStatus
	FailureCode     FailureCode
	RequestedAt     time.Time
	CompletedAt     time.Time
	Duplicate       bool
	Reconciled      bool
}

type ReplayEvidence struct {
	Status           ReplayEvidenceStatus
	DestinationTopic string
	ReplayID         string
	SourceTopic      string
	SourcePartition  int32
	SourceOffset     int64
	ConsumerGroup    string
	EventID          string
	SchemaVersion    int
	PayloadSHA256    string
	KeySHA256        string
	RecordedAt       time.Time
}

var kafkaTopicPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,247}[A-Za-z0-9])?$`)
var replayIDPattern = regexp.MustCompile(`^replay-[a-f0-9]{32}$`)
var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func NormalizeCoordinate(topic string, partition int32, offset int64) (Coordinate, error) {
	topic = strings.TrimSpace(topic)
	if topic == "" || len(topic) > MaxTopicLength || !kafkaTopicPattern.MatchString(topic) {
		return Coordinate{}, ErrInvalidTopic
	}
	if partition < 0 {
		return Coordinate{}, ErrInvalidPartition
	}
	if offset < 0 {
		return Coordinate{}, ErrInvalidOffset
	}
	return Coordinate{Topic: topic, Partition: partition, Offset: offset}, nil
}

func NormalizeLimit(limit, defaultLimit int) (int, error) {
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > MaxReadLimit {
		return 0, ErrInvalidLimit
	}
	return limit, nil
}

func NormalizeReason(reason string) (ReplayReason, error) {
	normalized := ReplayReason(strings.ToLower(strings.TrimSpace(reason)))
	switch normalized {
	case ReasonOperatorRetry, ReasonIncidentRecovery, ReasonPostFixReplay, ReasonRetentionRescue:
		return normalized, nil
	default:
		return "", ErrInvalidReason
	}
}

func NewReplayCommand(
	topic string,
	partition int32,
	offset int64,
	actorID int64,
	reason string,
	idempotencyKey string,
	replayID string,
	requestedAt time.Time,
) (ReplayCommand, error) {
	coordinate, err := NormalizeCoordinate(topic, partition, offset)
	if err != nil {
		return ReplayCommand{}, err
	}
	if actorID <= 0 {
		return ReplayCommand{}, ErrInvalidActor
	}
	normalizedReason, err := NormalizeReason(reason)
	if err != nil {
		return ReplayCommand{}, err
	}
	idempotencyFingerprint, err := DigestIdempotencyKey(idempotencyKey)
	if err != nil {
		return ReplayCommand{}, err
	}
	if !replayIDPattern.MatchString(strings.TrimSpace(replayID)) {
		return ReplayCommand{}, ErrInvalidReplayID
	}
	requestedAt = requestedAt.UTC()
	if requestedAt.IsZero() {
		return ReplayCommand{}, ErrReplayPersistence
	}
	return ReplayCommand{
		Coordinate: coordinate, ActorID: actorID, Reason: normalizedReason,
		IdempotencyFingerprint: idempotencyFingerprint,
		RequestFingerprint:     ReplayRequestFingerprint(coordinate, normalizedReason),
		ReplayID:               strings.TrimSpace(replayID), RequestedAt: requestedAt,
	}, nil
}

func DigestIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrIdempotencyKeyRequired
	}
	if len(value) > MaxIdempotencyKeyLength {
		return "", ErrIdempotencyKeyTooLong
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ReplayRequestFingerprint(coordinate Coordinate, reason ReplayReason) string {
	normalized := strings.Join([]string{
		coordinate.Topic,
		strconv.FormatInt(int64(coordinate.Partition), 10),
		strconv.FormatInt(coordinate.Offset, 10),
		string(reason),
	}, "\n")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func ValidateRoute(route RecoveryRoute) error {
	dlq, err := NormalizeCoordinate(route.DLQTopic, 0, 0)
	if err != nil || dlq.Topic != route.DLQTopic {
		return ErrInvalidProvenance
	}
	source, err := NormalizeCoordinate(route.SourceTopic, 0, 0)
	if err != nil || source.Topic != route.SourceTopic {
		return ErrInvalidProvenance
	}
	replay, err := NormalizeCoordinate(route.ReplayTopic, 0, 0)
	if err != nil || replay.Topic != route.ReplayTopic {
		return ErrInvalidProvenance
	}
	if strings.TrimSpace(route.ConsumerGroup) == "" ||
		len(route.ConsumerGroup) > MaxGroupLength ||
		route.MaxAttempt < 1 || route.ReplayTier < 0 || route.Retention <= 0 {
		return ErrInvalidProvenance
	}
	return nil
}

func ValidateRecord(route RecoveryRoute, record RetainedRecord) error {
	if ValidateRoute(route) != nil ||
		record.Coordinate.Topic != route.DLQTopic ||
		record.Coordinate.Partition < 0 || record.Coordinate.Offset < 0 ||
		record.Metadata.ConsumerGroup != route.ConsumerGroup ||
		record.Metadata.NonReplayable ||
		record.Metadata.FailureClass == "recovery_metadata_invalid" ||
		record.Metadata.SourceTopic != route.SourceTopic ||
		record.Metadata.SourcePartition < 0 || record.Metadata.SourceOffset < 0 ||
		record.Metadata.Attempt < 1 || record.Metadata.Attempt > route.MaxAttempt ||
		record.Metadata.Tier != 0 ||
		strings.TrimSpace(record.Metadata.FailureClass) == "" ||
		strings.TrimSpace(record.Metadata.EventID) == "" ||
		len(record.Metadata.EventID) > MaxEventIDLength ||
		record.Metadata.SchemaVersion < 0 ||
		!sha256Pattern.MatchString(record.Metadata.PayloadSHA256) ||
		record.Metadata.PayloadSHA256 != PayloadSHA256(record.Value) ||
		record.Metadata.FirstFailureAt.IsZero() ||
		record.Metadata.LatestFailureAt.IsZero() ||
		record.Metadata.NotBefore.IsZero() ||
		record.Metadata.FirstFailureAt.After(record.Metadata.LatestFailureAt) ||
		record.Metadata.NotBefore.Before(record.Metadata.LatestFailureAt) {
		return ErrInvalidProvenance
	}
	if record.Metadata.ReplayID != "" && !replayIDPattern.MatchString(record.Metadata.ReplayID) {
		return ErrInvalidProvenance
	}
	return nil
}

func PayloadSHA256(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func ValidStatus(status ReplayStatus) bool {
	return status == StatusPending || status == StatusSucceeded || status == StatusFailed
}

func ValidFailureCode(code FailureCode) bool {
	switch code {
	case FailureNone, FailurePublishTimeout, FailurePublishRejected,
		FailurePublishUnavailable, FailureRecordMissing, FailureRecordExpired,
		FailureInvalidProvenance, FailureInspectionFailed, FailurePublicationAbsent:
		return true
	default:
		return false
	}
}

func (c Coordinate) String() string {
	return fmt.Sprintf("%s:%d:%d", c.Topic, c.Partition, c.Offset)
}

func ErrorForFailure(code FailureCode) error {
	switch code {
	case FailurePublishTimeout:
		return ErrReplayPublishTimeout
	case FailurePublishRejected:
		return ErrReplayPublishRejected
	case FailurePublishUnavailable:
		return ErrReplayPublishUnavailable
	case FailureRecordMissing:
		return ErrRecordNotFound
	case FailureRecordExpired:
		return ErrRecordExpired
	case FailureInvalidProvenance:
		return ErrInvalidProvenance
	case FailureInspectionFailed:
		return ErrInspectionUnavailable
	case FailurePublicationAbsent:
		return ErrReplayPublicationAbsent
	default:
		return nil
	}
}

func ValidateReplayEvidence(
	route RecoveryRoute,
	record RetainedRecord,
	replayID string,
	evidence ReplayEvidence,
) error {
	if ValidateRoute(route) != nil ||
		ValidateRecord(route, record) != nil ||
		!replayIDPattern.MatchString(replayID) ||
		evidence.Status != ReplayEvidenceFound ||
		evidence.DestinationTopic != route.ReplayTopic ||
		evidence.ReplayID != replayID ||
		evidence.SourceTopic != record.Metadata.SourceTopic ||
		evidence.SourcePartition != record.Metadata.SourcePartition ||
		evidence.SourceOffset != record.Metadata.SourceOffset ||
		evidence.ConsumerGroup != record.Metadata.ConsumerGroup ||
		evidence.EventID != record.Metadata.EventID ||
		evidence.SchemaVersion != record.Metadata.SchemaVersion ||
		evidence.PayloadSHA256 != record.Metadata.PayloadSHA256 ||
		evidence.PayloadSHA256 != PayloadSHA256(record.Value) ||
		evidence.KeySHA256 != PayloadSHA256(record.Key) ||
		evidence.RecordedAt.IsZero() {
		return ErrInvalidProvenance
	}
	return nil
}

func ValidateReplayAbsence(
	route RecoveryRoute,
	replayID string,
	evidence ReplayEvidence,
) error {
	if ValidateRoute(route) != nil ||
		!replayIDPattern.MatchString(replayID) ||
		evidence.Status != ReplayEvidenceAbsent ||
		evidence.DestinationTopic != route.ReplayTopic ||
		evidence.ReplayID != replayID {
		return ErrInvalidProvenance
	}
	return nil
}
