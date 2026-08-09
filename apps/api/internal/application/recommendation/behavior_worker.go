package applicationrecommendation

import (
	"context"
	"errors"
	"fmt"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	"strings"
	"time"
)

var (
	ErrBehaviorEventConflict = errors.New("behavior event payload conflict")
	ErrTerminalBehaviorEvent = errors.New("terminal behavior event")
)

type terminalBehaviorEventError struct{ cause error }

func (e terminalBehaviorEventError) Error() string {
	return fmt.Sprintf("%v: %v", ErrTerminalBehaviorEvent, e.cause)
}

func (e terminalBehaviorEventError) Unwrap() error  { return e.cause }
func (e terminalBehaviorEventError) Terminal() bool { return true }

func IsTerminalBehaviorEventError(err error) bool {
	var terminal interface{ Terminal() bool }
	return errors.As(err, &terminal) && terminal.Terminal()
}

type BehaviorEventSource interface {
	ConsumeViewEventRecorded(ctx context.Context, handler func(context.Context, *applicationexposure.ViewEventRecordedEvent) error) error
}

type BehaviorEventRepository interface {
	ApplyBehaviorEvent(ctx context.Context, event *applicationexposure.ViewEventRecordedEvent) (bool, error)
}
type OutcomeAttributionRepository interface {
	VerifyAndSaveOutcome(ctx context.Context, outcome *domainrecommendation.Outcome, followedTargetUserID int64) (recorded bool, attributed bool, err error)
}

type BehaviorEventWorker struct {
	repo      BehaviorEventRepository
	source    BehaviorEventSource
	projector *ProfileProjector
	observer  BehaviorConsumerObserver
}

type BehaviorConsumerObserver interface {
	ObserveViewConsumption(result string)
}

type BehaviorWorkerOption func(*BehaviorEventWorker)

func WithBehaviorConsumerObserver(observer BehaviorConsumerObserver) BehaviorWorkerOption {
	return func(worker *BehaviorEventWorker) {
		worker.observer = observer
	}
}

func NewBehaviorEventWorker(
	repo BehaviorEventRepository,
	source BehaviorEventSource,
	options ...BehaviorWorkerOption,
) *BehaviorEventWorker {
	worker := &BehaviorEventWorker{repo: repo, source: source}
	if projectionRepo, ok := repo.(ProfileProjectionRepository); ok {
		worker.projector = NewProfileProjector(projectionRepo)
	}
	for _, option := range options {
		option(worker)
	}
	return worker
}

func (w *BehaviorEventWorker) Start(ctx context.Context) error {
	if w.source == nil {
		return nil
	}
	if err := w.source.ConsumeViewEventRecorded(ctx, w.Handle); err != nil {
		return err
	}
	return nil
}

func (w *BehaviorEventWorker) Handle(ctx context.Context, event *applicationexposure.ViewEventRecordedEvent) error {
	if event == nil || event.EventID == "" {
		w.observeViewConsumption("terminal")
		return terminalBehaviorEventError{cause: ErrBehaviorEventConflict}
	}
	stored, err := w.repo.ApplyBehaviorEvent(ctx, event)
	if err != nil {
		inframetrics.ObserveProfileWorker(event.OccurredAt, false, err)
		if errors.Is(err, ErrBehaviorEventConflict) {
			w.observeViewConsumption("terminal")
			return terminalBehaviorEventError{cause: err}
		}
		w.observeViewConsumption("retryable")
		return err
	}
	if stored {
		w.observeViewConsumption("applied")
	} else {
		w.observeViewConsumption("duplicate")
	}
	if _, durable := w.repo.(BehaviorProfileOutboxStore); durable {
		// The raw fact and its projection handoff committed together. Profile
		// feature gaps and attribution evidence propagation now retry through
		// the leased outbox rather than redelivering this MQ message.
		inframetrics.ObserveProfileWorker(event.OccurredAt, !stored, nil)
		return nil
	}
	if outcomes, ok := w.repo.(OutcomeAttributionRepository); ok &&
		event.RequestID != "" &&
		strings.ToLower(strings.TrimSpace(event.Scene)) == domainrecommendation.RecommendationRequestLogScene {
		outcome, outcomeErr := domainrecommendation.NewOutcomeWithRecordedAt(
			domainrecommendation.ViewOutcomeID(event.UserID, event.EventID),
			event.RequestID,
			event.UserID,
			event.VideoID,
			event.EventType,
			event.OccurredAt,
			recordedAtForOutcome(event.RecordedAt),
		)
		if outcomeErr != nil {
			inframetrics.ObserveProfileWorker(event.OccurredAt, false, outcomeErr)
			return outcomeErr
		}
		recorded, attributed, saveErr := outcomes.VerifyAndSaveOutcome(ctx, outcome, 0)
		if saveErr != nil {
			inframetrics.ObserveProfileWorker(event.OccurredAt, false, saveErr)
			return saveErr
		}
		if !attributed {
			inframetrics.ObserveRecommendationInvalidAttribution(outcome.OutcomeType)
		} else if recorded {
			inframetrics.ObserveRecommendationOutcome(outcome.OutcomeType)
		}
	}
	if w.projector == nil {
		inframetrics.ObserveProfileWorker(event.OccurredAt, !stored, nil)
		return nil
	}
	applied, err := w.projector.ApplyView(ctx, event)
	inframetrics.ObserveProfileWorker(event.OccurredAt, !stored && !applied && err == nil, err)
	return err
}

func (w *BehaviorEventWorker) observeViewConsumption(result string) {
	if w != nil && w.observer != nil {
		w.observer.ObserveViewConsumption(result)
	}
}

func recordedAtForOutcome(recordedAt time.Time) time.Time {
	if recordedAt.IsZero() {
		return time.Now().UTC()
	}
	return recordedAt.UTC()
}
