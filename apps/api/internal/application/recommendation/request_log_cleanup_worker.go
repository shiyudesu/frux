package applicationrecommendation

import (
	domainrecommendation "GCFeed/internal/domain/recommendation"
	inframetrics "GCFeed/internal/infra/metrics"
	"context"
	"log"
	"time"
)

const (
	defaultRequestLogCleanupBatchSize  = 100
	defaultRequestLogCleanupInterval   = time.Hour
	defaultRequestLogCleanupMaxBatches = 20
	defaultRequestLogCleanupMaxRuntime = 5 * time.Second
)

var requestLogCleanupScenes = []string{domainrecommendation.RecommendationRequestLogScene}

type RequestLogCleanupPolicyReader interface {
	ListPolicies(ctx context.Context, scene string) ([]*domainrecommendation.Policy, error)
}

type RequestLogCleanupStore interface {
	DeleteRequestLogsForPolicyBefore(ctx context.Context, scene string, policyVersion int, cutoff time.Time, limit int) (int64, error)
}

type ServedCandidateEvidenceCleanupStore interface {
	DeleteServedCandidateEvidenceBefore(ctx context.Context, cutoff time.Time, requestLimit int) (domainrecommendation.ServedCandidateEvidenceCleanupResult, error)
}

// RequestLogCleanupWorker safely drains bounded cleanup batches. It gives
// expired served-candidate evidence its own first pass so policy request-log
// backlog cannot delay attribution cleanup indefinitely. Evidence batches are
// bounded by request identity, while each deletion removes a complete request
// group regardless of its candidate count.
type RequestLogCleanupWorker struct {
	policies    RequestLogCleanupPolicyReader
	store       RequestLogCleanupStore
	now         func() time.Time
	batch       int
	maxBatches  int
	maxRuntime  time.Duration
	interval    time.Duration
	policyStart int
}

func NewRequestLogCleanupWorker(policies RequestLogCleanupPolicyReader, store RequestLogCleanupStore) *RequestLogCleanupWorker {
	return &RequestLogCleanupWorker{
		policies: policies, store: store, now: func() time.Time { return time.Now().UTC() },
		batch: defaultRequestLogCleanupBatchSize, maxBatches: defaultRequestLogCleanupMaxBatches,
		maxRuntime: defaultRequestLogCleanupMaxRuntime, interval: defaultRequestLogCleanupInterval,
	}
}

func (w *RequestLogCleanupWorker) Start(ctx context.Context) error {
	if w == nil || w.policies == nil || w.store == nil {
		return nil
	}
	if _, err := w.DispatchOnce(ctx); err != nil {
		log.Printf("recommendation request-log cleanup failed: %v", err)
	}
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := w.DispatchOnce(ctx); err != nil {
					log.Printf("recommendation request-log cleanup failed: %v", err)
				}
			}
		}
	}()
	return nil
}

func (w *RequestLogCleanupWorker) DispatchOnce(ctx context.Context) (deleted int64, resultErr error) {
	if w == nil || w.policies == nil || w.store == nil {
		return 0, nil
	}
	start := time.Now()
	defer func() {
		inframetrics.ObserveWorkerJob("recommendation_request_log_cleanup", time.Since(start), resultErr)
	}()
	now := w.now().UTC()
	deadline := time.Now().Add(w.maxRuntime)
	if evidenceStore, ok := w.store.(ServedCandidateEvidenceCleanupStore); ok {
		cutoff := domainrecommendation.ServedCandidateEvidenceCleanupCutoff(now)
		for batch := 0; batch < w.maxBatches && time.Now().Before(deadline); batch++ {
			result, err := evidenceStore.DeleteServedCandidateEvidenceBefore(ctx, cutoff, w.batch)
			if err != nil {
				return deleted, err
			}
			deleted += result.CandidateRows
			if result.RequestGroups < w.batch {
				break
			}
		}
	}
	validPolicies := make([]*domainrecommendation.Policy, 0)
	for _, scene := range requestLogCleanupScenes {
		policies, err := w.policies.ListPolicies(ctx, scene)
		if err != nil {
			return deleted, err
		}
		for _, policy := range policies {
			if policy != nil && policy.Version > 0 && policy.Config.RetentionDays > 0 {
				validPolicies = append(validPolicies, policy)
			}
		}
	}
	if len(validPolicies) == 0 {
		return deleted, nil
	}

	startIndex := w.policyStart % len(validPolicies)
	if startIndex < 0 {
		startIndex = 0
	}
	w.policyStart = (startIndex + 1) % len(validPolicies)
	requestLogBatches := w.maxBatches
	for requestLogBatches > 0 && time.Now().Before(deadline) {
		progressed := false
		for index := 0; index < len(validPolicies) && requestLogBatches > 0 && time.Now().Before(deadline); index++ {
			policy := validPolicies[(startIndex+index)%len(validPolicies)]
			count, err := w.store.DeleteRequestLogsForPolicyBefore(
				ctx,
				policy.Scene,
				policy.Version,
				now.AddDate(0, 0, -policy.Config.RetentionDays),
				w.batch,
			)
			if err != nil {
				return deleted, err
			}
			requestLogBatches--
			deleted += count
			if count == int64(w.batch) {
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}
	return deleted, nil
}
