package applicationeventstream

import (
	"context"
	"errors"
	"strings"
	"sync"
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
	ParityPending  ParityResult = "pending"
)

type ParityChecker interface {
	Compare(ctx context.Context, event Event) (ParityResult, error)
}

type ShadowObserver interface {
	ObserveShadow(result string)
}

type ShadowHandler struct {
	expectedGroup  string
	maxAge         time.Duration
	parity         ParityChecker
	observer       ShadowObserver
	now            func() time.Time
	pendingMu      sync.Mutex
	pendingRetries map[string]int
	maxRetries     int
	retryDelay     time.Duration
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
		pendingRetries: make(map[string]int),
		maxRetries:     3, retryDelay: time.Second,
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
			h.clearPending(event.EventID)
			h.observe("parity_match")
		case ParityMismatch:
			h.clearPending(event.EventID)
			h.observe("parity_mismatch")
		case ParityPending:
			if h.retryPending(event.EventID) {
				h.observe("parity_pending")
				return OutcomeRetryable, PendingParityError{Delay: h.retryDelay}
			}
			h.observe("parity_pending_exhausted")
		default:
			h.observe("invalid")
			return OutcomeTerminal, nil
		}
	} else {
		h.observe("validated")
	}
	return OutcomeDurableSuccess, nil
}

type PendingParityError struct {
	Delay time.Duration
}

func (e PendingParityError) Error() string {
	return "shadow parity fact is pending"
}

func (e PendingParityError) RetryAfter() time.Duration {
	return e.Delay
}

func (h *ShadowHandler) retryPending(eventID string) bool {
	h.pendingMu.Lock()
	defer h.pendingMu.Unlock()
	retries := h.pendingRetries[eventID]
	if retries >= h.maxRetries {
		delete(h.pendingRetries, eventID)
		return false
	}
	h.pendingRetries[eventID] = retries + 1
	return true
}

func (h *ShadowHandler) clearPending(eventID string) {
	h.pendingMu.Lock()
	delete(h.pendingRetries, eventID)
	h.pendingMu.Unlock()
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
