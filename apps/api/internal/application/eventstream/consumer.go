package applicationeventstream

import (
	"context"
	"errors"
	"strings"
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

type ShadowOnlyHandler interface {
	Handler
	ShadowOnly()
	ExpectedGroup() string
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

type ParityResult string

const (
	ParityMatch    ParityResult = "match"
	ParityMismatch ParityResult = "mismatch"
)

type ParityChecker interface {
	Compare(ctx context.Context, event Event) (ParityResult, error)
}

type ShadowObserver interface {
	ObserveShadow(result string)
}

type ShadowHandler struct {
	expectedGroup string
	maxAge        time.Duration
	parity        ParityChecker
	observer      ShadowObserver
	now           func() time.Time
}

func NewShadowHandler(
	expectedGroup string,
	maxAge time.Duration,
	parity ParityChecker,
	observer ShadowObserver,
) (*ShadowHandler, error) {
	if !strings.Contains(expectedGroup, ".shadow.") || maxAge <= 0 {
		return nil, ErrInvalidOutcome
	}
	return &ShadowHandler{
		expectedGroup: expectedGroup, maxAge: maxAge,
		parity: parity, observer: observer, now: time.Now,
	}, nil
}

func (h *ShadowHandler) Handle(ctx context.Context, event Event) (Outcome, error) {
	if h == nil || event.Metadata.Group != h.expectedGroup ||
		event.EventID == "" || event.EventType == "" ||
		len(event.Metadata.Key) == 0 || event.ProducedAt.IsZero() {
		h.observe("invalid")
		return OutcomeTerminal, nil
	}
	age := h.now().UTC().Sub(event.ProducedAt.UTC())
	if age < -5*time.Minute {
		h.observe("future")
		return OutcomeTerminal, nil
	}
	if age > h.maxAge {
		h.observe("expired")
		return OutcomeTerminal, nil
	}
	if h.parity != nil {
		result, err := h.parity.Compare(ctx, event)
		if err != nil {
			h.observe("parity_unavailable")
			return OutcomeRetryable, err
		}
		switch result {
		case ParityMatch:
			h.observe("parity_match")
		case ParityMismatch:
			h.observe("parity_mismatch")
		default:
			h.observe("invalid")
			return OutcomeTerminal, nil
		}
	} else {
		h.observe("validated")
	}
	return OutcomeDurableSuccess, nil
}

func (*ShadowHandler) ShadowOnly() {}

func (h *ShadowHandler) ExpectedGroup() string {
	if h == nil {
		return ""
	}
	return h.expectedGroup
}

func (h *ShadowHandler) observe(result string) {
	if h != nil && h.observer != nil {
		h.observer.ObserveShadow(result)
	}
}
