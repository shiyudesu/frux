package applicationinteraction

import (
	domaininteraction "GCFeed/internal/domain/interaction"
	inframetrics "GCFeed/internal/infra/metrics"
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrTerminalActionEvent = errors.New("terminal action event")

type ActionEventConsumer interface {
	ConsumeActionChanged(ctx context.Context, handler func(context.Context, *ActionChangedEvent) error) error
}

type ActionWorker struct {
	repo     domaininteraction.AcceptedActionEventRepository
	consumer ActionEventConsumer
}

func NewActionWorker(repo domaininteraction.AcceptedActionEventRepository, consumer ActionEventConsumer) *ActionWorker {
	return &ActionWorker{
		repo:     repo,
		consumer: consumer,
	}
}

func (w *ActionWorker) Start(ctx context.Context) error {
	if w == nil || w.consumer == nil {
		return nil
	}
	return w.consumer.ConsumeActionChanged(ctx, w.HandleActionChanged)
}

func (w *ActionWorker) HandleActionChanged(ctx context.Context, event *ActionChangedEvent) (resultErr error) {
	start := time.Now()
	defer func() {
		inframetrics.ObserveWorkerJob("interaction_action_changed", time.Since(start), resultErr)
	}()

	if event == nil {
		return terminalActionEvent(domaininteraction.ErrInvalidActionEvent)
	}
	accepted, err := acceptedActionEvent(event)
	if err != nil {
		return terminalActionEvent(err)
	}
	if err := w.repo.PersistAcceptedActionEvent(ctx, accepted); err != nil {
		if errors.Is(err, domaininteraction.ErrVideoNotFound) ||
			errors.Is(err, domaininteraction.ErrActionEventConflict) {
			return terminalActionEvent(err)
		}
		return err
	}
	return nil
}

func IsTerminalActionEventError(err error) bool {
	return errors.Is(err, ErrTerminalActionEvent)
}

func terminalActionEvent(err error) error {
	return fmt.Errorf("%w: %w", ErrTerminalActionEvent, err)
}

func acceptedActionEvent(event *ActionChangedEvent) (*domaininteraction.AcceptedActionEvent, error) {
	if event == nil {
		return nil, domaininteraction.ErrInvalidActionEvent
	}
	return domaininteraction.NewAcceptedActionEvent(
		event.EventID,
		event.UserID,
		event.VideoID,
		event.ActionType,
		event.Active,
		event.IdempotencyKey,
		event.Version,
		event.OccurredAt,
	)
}
