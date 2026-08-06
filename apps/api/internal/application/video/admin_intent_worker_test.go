package applicationvideo

import (
	"context"
	"errors"
	"testing"
	"time"

	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type adminIntentStoreStub struct {
	intent      *domainvideo.AdminTransitionIntent
	availableAt time.Time
	leased      bool
	delivered   bool
	failures    int
}

func (s *adminIntentStoreStub) ClaimAdminTransitionIntents(
	_ context.Context,
	_ string,
	_ int,
	now, _ time.Time,
) ([]*domainvideo.AdminTransitionIntent, error) {
	if s.intent == nil || s.delivered || s.leased || now.Before(s.availableAt) {
		return nil, nil
	}
	s.leased = true
	s.intent.Attempts++
	copyIntent := *s.intent
	return []*domainvideo.AdminTransitionIntent{&copyIntent}, nil
}

func (s *adminIntentStoreStub) MarkAdminTransitionIntentDelivered(
	context.Context,
	int64,
	string,
	time.Time,
) error {
	s.delivered = true
	s.leased = false
	return nil
}

func (s *adminIntentStoreStub) MarkAdminTransitionIntentFailed(
	_ context.Context,
	_ int64,
	_ string,
	availableAt time.Time,
	_ string,
) error {
	s.failures++
	s.availableAt = availableAt
	s.leased = false
	return nil
}

type adminIntentVideoReaderStub struct {
	video *domainvideo.Video
}

func (s adminIntentVideoReaderStub) FindByIDAnyStatus(
	context.Context,
	int64,
) (*domainvideo.Video, error) {
	return s.video, nil
}

type adminIntentCacheStub struct {
	calls int
	err   error
}

func (s *adminIntentCacheStub) InvalidateVideo(context.Context, int64) error {
	s.calls++
	return s.err
}

type adminIntentSideEffectsStub struct {
	calls int
	err   error
}

func (s *adminIntentSideEffectsStub) ApplyAdminTransition(
	context.Context,
	*domainvideo.Video,
) error {
	s.calls++
	return s.err
}

func TestAdminTransitionIntentRetriesAndMarksDeliveredOnlyAfterAllSideEffects(t *testing.T) {
	now := time.Date(2026, 8, 6, 7, 0, 0, 0, time.UTC)
	store := &adminIntentStoreStub{
		intent: &domainvideo.AdminTransitionIntent{
			ID: 1, EventID: "video-admin-transition:1", VideoID: 7,
		},
		availableAt: now,
	}
	cache := &adminIntentCacheStub{err: errors.New("cache unavailable")}
	sideEffects := &adminIntentSideEffectsStub{err: errors.New("media unavailable")}
	worker := NewAdminTransitionIntentWorker(
		store,
		adminIntentVideoReaderStub{video: &domainvideo.Video{
			ID: 7, Status: domainvideo.StatusOffline,
		}},
		cache,
		sideEffects,
	)
	worker.now = func() time.Time { return now }

	delivered, err := worker.DispatchOnce(context.Background())
	if err == nil || delivered != 0 {
		t.Fatalf("first dispatch delivered=%d err=%v", delivered, err)
	}
	if store.delivered || store.failures != 1 || cache.calls != 1 || sideEffects.calls != 1 {
		t.Fatalf(
			"first dispatch delivered=%v failures=%d cache=%d media=%d",
			store.delivered, store.failures, cache.calls, sideEffects.calls,
		)
	}

	now = store.availableAt
	cache.err = nil
	delivered, err = worker.DispatchOnce(context.Background())
	if err == nil || delivered != 0 {
		t.Fatalf("second dispatch delivered=%d err=%v", delivered, err)
	}
	if store.delivered || store.failures != 2 || cache.calls != 2 || sideEffects.calls != 2 {
		t.Fatalf(
			"second dispatch delivered=%v failures=%d cache=%d media=%d",
			store.delivered, store.failures, cache.calls, sideEffects.calls,
		)
	}

	now = store.availableAt
	sideEffects.err = nil
	delivered, err = worker.DispatchOnce(context.Background())
	if err != nil || delivered != 1 {
		t.Fatalf("retry delivered=%d err=%v", delivered, err)
	}
	if !store.delivered || cache.calls != 3 || sideEffects.calls != 3 {
		t.Fatalf(
			"retry delivered=%v cache=%d media=%d",
			store.delivered, cache.calls, sideEffects.calls,
		)
	}
}
