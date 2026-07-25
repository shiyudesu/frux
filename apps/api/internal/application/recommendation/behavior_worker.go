package applicationrecommendation

import (
	applicationexposure "GCFeed/internal/application/exposure"
	"context"
)

type BehaviorEventSource interface {
	ConsumeViewEventRecorded(ctx context.Context, handler func(context.Context, *applicationexposure.ViewEventRecordedEvent) error) error
}

type BehaviorEventRepository interface {
	ApplyBehaviorEvent(ctx context.Context, event *applicationexposure.ViewEventRecordedEvent) (bool, error)
}

type BehaviorEventWorker struct {
	repo   BehaviorEventRepository
	source BehaviorEventSource
}

func NewBehaviorEventWorker(repo BehaviorEventRepository, source BehaviorEventSource) *BehaviorEventWorker {
	return &BehaviorEventWorker{repo: repo, source: source}
}

func (w *BehaviorEventWorker) Start(ctx context.Context) error {
	return w.source.ConsumeViewEventRecorded(ctx, w.Handle)
}

func (w *BehaviorEventWorker) Handle(ctx context.Context, event *applicationexposure.ViewEventRecordedEvent) error {
	if event == nil || event.EventID == "" {
		return nil
	}
	_, err := w.repo.ApplyBehaviorEvent(ctx, event)
	return err
}
