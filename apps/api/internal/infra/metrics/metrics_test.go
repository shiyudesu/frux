package inframetrics

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestHTTPMiddlewareUsesNormalizedRoute(t *testing.T) {
	h := server.New(server.WithDisablePrintRoute(true))
	h.Use(HTTPMiddleware())
	h.GET("/items/:id", func(_ context.Context, c *app.RequestContext) {
		c.Status(http.StatusNoContent)
	})

	normalized := HTTPRequestsTotal.WithLabelValues(http.MethodGet, "/items/:id", "204")
	raw := HTTPRequestsTotal.WithLabelValues(http.MethodGet, "/items/42", "204")
	normalizedBefore := testutil.ToFloat64(normalized)
	rawBefore := testutil.ToFloat64(raw)

	resp := ut.PerformRequest(h.Engine, http.MethodGet, "/items/42", nil)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, resp.Code)
	}
	if got := testutil.ToFloat64(normalized) - normalizedBefore; got != 1 {
		t.Fatalf("expected normalized route counter delta 1, got %v", got)
	}
	if got := testutil.ToFloat64(raw) - rawBefore; got != 0 {
		t.Fatalf("expected raw route counter delta 0, got %v", got)
	}
}

func TestObserveProfileWorkerTracksOutcomes(t *testing.T) {
	updated := ProfileWorkerEventsTotal.WithLabelValues("updated")
	duplicate := ProfileWorkerEventsTotal.WithLabelValues("duplicate")
	failure := ProfileWorkerEventsTotal.WithLabelValues("failure")
	updatedBefore, duplicateBefore, failureBefore := testutil.ToFloat64(updated), testutil.ToFloat64(duplicate), testutil.ToFloat64(failure)
	ObserveProfileWorker(time.Now().Add(-time.Second), false, nil)
	ObserveProfileWorker(time.Now(), true, nil)
	ObserveProfileWorker(time.Now(), false, errors.New("failure"))
	if testutil.ToFloat64(updated)-updatedBefore != 1 || testutil.ToFloat64(duplicate)-duplicateBefore != 1 || testutil.ToFloat64(failure)-failureBefore != 1 {
		t.Fatal("profile worker outcomes were not recorded")
	}
}

func TestRecommendationMetricsUseBoundedLabels(t *testing.T) {
	recall := RecommendationRecallCandidatesTotal.WithLabelValues("fresh")
	providerPool := RecommendationCandidatePoolCandidatesTotal.WithLabelValues("provider_returned", "fresh")
	unknownPool := RecommendationCandidatePoolCandidatesTotal.WithLabelValues("unknown", "unknown")
	quotaSelected := RecommendationQuotaMergeCandidatesTotal.WithLabelValues("reservation", "fresh", "reserved", "none")
	unknownQuota := RecommendationQuotaMergeCandidatesTotal.WithLabelValues("unknown", "unknown", "unknown", "unknown")
	policyRejection := RecommendationPolicyRejectionsTotal.WithLabelValues("pre_rank_pool")
	degraded := RecommendationDegradedRequestsTotal.WithLabelValues("unknown", "unknown")
	snapshot := RecommendationSnapshotOperationsTotal.WithLabelValues("write_failure")
	maintenance := RecommendationSnapshotOperationsTotal.WithLabelValues("maintenance_failure")
	outcome := RecommendationOutcomesTotal.WithLabelValues("complete")
	invalidAttribution := RecommendationInvalidAttributionsTotal.WithLabelValues("follow")
	logFailure := RecommendationRequestLogFailuresTotal.WithLabelValues("storage")
	deliveryFailure := RecommendationDeliveryFailuresTotal
	before := []float64{
		testutil.ToFloat64(recall), testutil.ToFloat64(providerPool), testutil.ToFloat64(unknownPool),
		testutil.ToFloat64(quotaSelected), testutil.ToFloat64(unknownQuota), testutil.ToFloat64(policyRejection),
		testutil.ToFloat64(degraded), testutil.ToFloat64(snapshot), testutil.ToFloat64(maintenance),
		testutil.ToFloat64(outcome), testutil.ToFloat64(invalidAttribution), testutil.ToFloat64(logFailure), testutil.ToFloat64(deliveryFailure),
	}
	ObserveRecommendationRecall("fresh", 2)
	ObserveRecommendationCandidatePool("provider_returned", "fresh", 3)
	ObserveRecommendationCandidatePool("user-42", "video-99", 5)
	ObserveRecommendationQuotaMerge("reservation", "fresh", "reserved", "none", 4)
	ObserveRecommendationQuotaMerge("request-42", "video-99", "map[payload:true]", "raw provider error", 6)
	ObserveRecommendationQuotaMergeDuration("success", time.Millisecond)
	ObserveRecommendationQuotaMergeDuration("request-42", time.Millisecond)
	ObserveRecommendationQuotaMergeSelectedPoolSize(500)
	ObserveRecommendationPolicyRejection("pre_rank_pool")
	ObserveRecommendationDegraded("unbounded-provider", "unbounded-reason")
	ObserveRecommendationSnapshot("write_failure")
	ObserveRecommendationSnapshot("maintenance_failure")
	ObserveRecommendationOutcome("complete")
	ObserveRecommendationInvalidAttribution("follow")
	ObserveRecommendationRequestLogFailure("storage")
	ObserveRecommendationDeliveryFailure()
	ObserveRecommendationPolicy("recommend", 7)
	if testutil.ToFloat64(recall)-before[0] != 2 || testutil.ToFloat64(providerPool)-before[1] != 3 ||
		testutil.ToFloat64(unknownPool)-before[2] != 5 || testutil.ToFloat64(quotaSelected)-before[3] != 4 ||
		testutil.ToFloat64(unknownQuota)-before[4] != 6 || testutil.ToFloat64(policyRejection)-before[5] != 1 ||
		testutil.ToFloat64(degraded)-before[6] != 1 || testutil.ToFloat64(snapshot)-before[7] != 1 ||
		testutil.ToFloat64(maintenance)-before[8] != 1 || testutil.ToFloat64(outcome)-before[9] != 1 ||
		testutil.ToFloat64(invalidAttribution)-before[10] != 1 || testutil.ToFloat64(logFailure)-before[11] != 1 ||
		testutil.ToFloat64(deliveryFailure)-before[12] != 1 {
		t.Fatal("recommendation metric observations were not recorded")
	}
	if testutil.CollectAndCount(RecommendationQuotaMergeDuration) != 2 || testutil.CollectAndCount(RecommendationQuotaMergeSelectedPoolSize) != 1 {
		t.Fatal("quota histograms were not registered with bounded labels")
	}
	if version := testutil.ToFloat64(RecommendationActivePolicyVersion.WithLabelValues("recommend")); version != 7 {
		t.Fatalf("expected policy version 7, got %v", version)
	}
}
