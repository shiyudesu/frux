package infraofflineevaluation

import (
	"os"
	"path/filepath"
	"testing"

	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
)

func TestLoadPerformanceEvidenceIsOptionalAndStrict(t *testing.T) {
	if metrics, err := LoadPerformanceEvidence(&LoadedManifest{Files: map[string]string{}}); err != nil || metrics != nil {
		t.Fatalf("metrics=%#v err=%v", metrics, err)
	}
	path := filepath.Join(t.TempDir(), "performance.csv")
	if err := os.WriteFile(path, []byte("metric,unit,value,sample_count,machine_profile\nexact_latency_p50,ns,1000,20,cpu-a\nembedding_throughput,items_per_second,42.5,20,gpu-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	metrics, err := LoadPerformanceEvidence(&LoadedManifest{Files: map[string]string{domainofflineevaluation.RoleThroughput: path}})
	if err != nil || len(metrics) != 2 || metrics[0].Value != 1000 {
		t.Fatalf("metrics=%#v err=%v", metrics, err)
	}
}
