package applicationrecommendation

import (
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	"context"
	"strings"
	"time"
)

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
}

func NewBehaviorEventWorker(repo BehaviorEventRepository, source BehaviorEventSource) *BehaviorEventWorker {
	worker := &BehaviorEventWorker{repo: repo, source: source}
	if projectionRepo, ok := repo.(ProfileProjectionRepository); ok {
		worker.projector = NewProfileProjector(projectionRepo)
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
		return nil
	}
	stored, err := w.repo.ApplyBehaviorEvent(ctx, event)
	if err != nil {
		inframetrics.ObserveProfileWorker(event.OccurredAt, false, err)
		return err
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

func recordedAtForOutcome(recordedAt time.Time) time.Time {
	if recordedAt.IsZero() {
		return time.Now().UTC()
	}
	return recordedAt.UTC()
}
