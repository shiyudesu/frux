package applicationrecommendation

import (
	domainrecommendation "GCFeed/internal/domain/recommendation"
	"context"
	"errors"
	"testing"
	"time"
)

type memoryRequestLogRepository struct {
	logs    []*domainrecommendation.RecommendationRequestLog
	deleted int64
}

func (r *memoryRequestLogRepository) SaveRequestLog(_ context.Context, log *domainrecommendation.RecommendationRequestLog) (*domainrecommendation.RecommendationRequestLog, bool, error) {
	r.logs = append(r.logs, log)
	return log, false, nil
}

func (r *memoryRequestLogRepository) DeleteRequestLogsBefore(_ context.Context, _ time.Time, _ int) (int64, error) {
	r.deleted++
	return r.deleted, nil
}

func TestRequestLogServiceSamplesAndCleansUp(t *testing.T) {
	control, err := domainrecommendation.NewRequestLogControl(domainrecommendation.MaxSamplingRatePPM, 14)
	if err != nil {
		t.Fatalf("new control: %v", err)
	}
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	repo := &memoryRequestLogRepository{}
	service := NewRequestLogService(repo, control, func() time.Time { return now })
	log, replayed, err := service.Record(context.Background(), domainrecommendation.RequestLogInput{
		RequestID: "request", UserID: 5, Scene: domainrecommendation.RecommendationRequestLogScene, PolicyVersion: 1,
		Candidates: []domainrecommendation.LoggedCandidate{{VideoID: 9}},
	})
	if err != nil || replayed || log == nil || len(repo.logs) != 1 {
		t.Fatalf("sampled log = %#v replayed=%v err=%v", log, replayed, err)
	}
	if _, err := service.Cleanup(context.Background(), 100); err != nil || repo.deleted != 1 {
		t.Fatalf("cleanup err=%v deleted=%d", err, repo.deleted)
	}
	if _, _, err := service.Record(context.Background(), domainrecommendation.RequestLogInput{
		RequestID: "other-scene", UserID: 5, Scene: "feed", PolicyVersion: 1,
		Candidates: []domainrecommendation.LoggedCandidate{{VideoID: 9}},
	}); !errors.Is(err, domainrecommendation.ErrInvalidRequestLog) || len(repo.logs) != 1 {
		t.Fatalf("non-recommend request log was accepted: err=%v logs=%#v", err, repo.logs)
	}
}
