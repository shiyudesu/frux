package applicationexposure

import (
	domainexposure "GCFeed/internal/domain/exposure"
	"context"
	"errors"
	"time"
)

var ErrSaveExposureFailed = errors.New("failed to save exposure")

type Service struct {
	repo domainexposure.Repository
	now  func() time.Time
}

type RecordViewEventResult struct {
	Event    *domainexposure.ViewEvent
	Exposure *domainexposure.Exposure
	Replayed bool
}

type Option func(*Service)

func New(repo domainexposure.Repository, options ...Option) *Service {
	service := &Service{repo: repo, now: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		option(service)
	}
	return service
}

func WithNow(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// RecordViewEvent 写入观看行为，并在 exposed 事件时同步维护曝光聚合索引。
func (s *Service) RecordViewEvent(ctx context.Context, input domainexposure.NewViewEventInput) (*RecordViewEventResult, error) {
	event, err := domainexposure.NewViewEvent(input)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if event.ClientEnvelope {
		stored, err := s.repo.FindViewEventByIdentity(ctx, event.UserID, event.EventID)
		if err != nil {
			return nil, ErrSaveExposureFailed
		}
		if stored != nil {
			if !stored.Event.SameNormalizedPayload(event) {
				return nil, domainexposure.ErrEventIDConflict
			}
			return recordViewEventResult(stored), nil
		}
		if event.OccurredAt.Before(now.Add(-domainexposure.MaxPastOccurrenceSkew)) ||
			event.OccurredAt.After(now.Add(domainexposure.MaxFutureOccurrenceSkew)) {
			return nil, domainexposure.ErrOccurredAtOutOfRange
		}
	} else {
		event.OccurredAt = now
		event.PositionMs = event.WatchMs
	}

	saved, err := s.repo.SaveViewEvent(ctx, event)
	if err != nil {
		if errors.Is(err, domainexposure.ErrVideoNotFound) || errors.Is(err, domainexposure.ErrEventIDConflict) {
			return nil, err
		}
		return nil, ErrSaveExposureFailed
	}

	return recordViewEventResult(saved), nil
}

func recordViewEventResult(saved *domainexposure.SaveViewEventResult) *RecordViewEventResult {
	return &RecordViewEventResult{
		Event:    saved.Event,
		Exposure: saved.Exposure,
		Replayed: saved.Replayed,
	}
}
