package inframetrics

import (
	"strings"
	"testing"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestSemanticMetricsFoldUnknownLabels(t *testing.T) {
	client := SemanticClientRequestsTotal.WithLabelValues("embed", "internal")
	vector := VideoEmbeddingVectorsTotal.WithLabelValues("semantic", "event", "failed")
	lease := SemanticLeaseTotal.WithLabelValues("lost")
	beforeClient := testutil.ToFloat64(client)
	beforeVector := testutil.ToFloat64(vector)
	beforeLease := testutil.ToFloat64(lease)
	ObserveSemanticClient("video:42", "raw service error", time.Millisecond)
	ObserveSemanticVector("video:42")
	ObserveSemanticLease("lease-owner-42")
	if testutil.ToFloat64(client)-beforeClient != 1 ||
		testutil.ToFloat64(vector)-beforeVector != 1 ||
		testutil.ToFloat64(lease)-beforeLease != 1 {
		t.Fatal("unknown semantic metric labels were not folded")
	}
}

func TestSemanticMetricDescriptorsExcludeHighCardinalityLabels(t *testing.T) {
	descriptions := []string{
		SemanticClientRequestsTotal.WithLabelValues("embed", "success").Desc().String(),
		VideoEmbeddingVectorsTotal.WithLabelValues("semantic", "event", "generated").Desc().String(),
		SemanticJobCount.WithLabelValues(domainembedding.SemanticJobPending).Desc().String(),
		VideoEmbeddingCoverage.WithLabelValues("semantic", "present").Desc().String(),
	}
	for _, description := range descriptions {
		for _, forbidden := range []string{
			"video_id", "text", "url", "error", "attempt", "token",
			"model_key",
		} {
			if strings.Contains(description, forbidden) {
				t.Fatalf("descriptor contains forbidden label %q: %s", forbidden, description)
			}
		}
	}
}

func TestSemanticBacklogMetricsUseClosedStateSet(t *testing.T) {
	now := time.Now().UTC()
	oldest := now.Add(-time.Minute)
	ObserveSemanticBacklog([]domainembedding.SemanticBacklog{
		{State: "arbitrary", Count: 2, OldestAt: &oldest},
	}, now)
	if got := testutil.ToFloat64(
		SemanticJobCount.WithLabelValues(domainembedding.SemanticJobFailed),
	); got != 2 {
		t.Fatalf("folded failed backlog = %v", got)
	}
	if got := testutil.ToFloat64(
		SemanticJobOldestSeconds.WithLabelValues(domainembedding.SemanticJobFailed),
	); got != 60 {
		t.Fatalf("folded failed oldest age = %v", got)
	}
}
