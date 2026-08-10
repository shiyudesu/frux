package applicationvideo

import (
	"context"
	"testing"
	"time"

	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type lifecycleTimestampRepository struct {
	domainvideo.Repository
	timestamps []time.Time
}

func (r *lifecycleTimestampRepository) ApplyLifecycleTransition(
	_ context.Context,
	_ int64,
	_ domainvideo.LifecycleTransition,
	at time.Time,
) (bool, error) {
	r.timestamps = append(r.timestamps, at)
	return true, nil
}

func (*lifecycleTimestampRepository) FindByIDAnyStatus(
	context.Context,
	int64,
) (*domainvideo.Video, error) {
	return &domainvideo.Video{
		ID: 7, Status: domainvideo.StatusOffline,
		Visibility: domainvideo.VisibilityPublic,
	}, nil
}

func TestLifecycleTransitionsReceiveDistinctNonzeroOperationTimes(t *testing.T) {
	repository := &lifecycleTimestampRepository{}
	service := New(repository)
	times := []time.Time{
		time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 10, 12, 1, 0, 0, time.UTC),
	}
	service.now = func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	}

	if err := service.SetOffline(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if err := service.SetOffline(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(repository.timestamps) != 2 ||
		repository.timestamps[0].IsZero() ||
		!repository.timestamps[1].After(repository.timestamps[0]) {
		t.Fatalf("lifecycle timestamps=%v", repository.timestamps)
	}
}
