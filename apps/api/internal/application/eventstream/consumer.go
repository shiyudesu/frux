package applicationeventstream

import (
	"context"
	"errors"
	"time"
)

type Outcome string

const (
	OutcomeDurableSuccess Outcome = "durable_success"
	OutcomeTerminal       Outcome = "terminal"
	OutcomeRetryable      Outcome = "retryable"
)

var ErrInvalidOutcome = errors.New("invalid event handler outcome")

type Header struct {
	Key   string
	Value []byte
}

type RecordMetadata struct {
	Topic     string
	Group     string
	Partition int32
	Offset    int64
	Timestamp time.Time
	Key       []byte
	Headers   []Header
}

type Event struct {
	Metadata      RecordMetadata
	EventID       string
	EventType     string
	SchemaVersion int
	OccurredAt    time.Time
	ProducedAt    time.Time
	Producer      string
	CorrelationID string
	Payload       any
}

type Handler interface {
	Handle(ctx context.Context, event Event) (Outcome, error)
}

func CommitEligible(outcome Outcome) bool {
	return outcome == OutcomeDurableSuccess || outcome == OutcomeTerminal
}

func ValidOutcome(outcome Outcome) bool {
	switch outcome {
	case OutcomeDurableSuccess, OutcomeTerminal, OutcomeRetryable:
		return true
	default:
		return false
	}
}
