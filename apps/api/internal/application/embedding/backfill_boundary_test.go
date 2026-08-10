package applicationembedding

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSemanticIntegrationHasNoHistoricalBackfillEntryPoint(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", "..", "..", ".."))
	for _, relative := range []string{
		"apps/api/internal/domain/embedding",
		"apps/api/internal/application/embedding",
		"apps/api/internal/infra/persistence/embedding",
	} {
		err := filepath.WalkDir(
			filepath.Join(root, relative),
			func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".go") ||
					strings.HasSuffix(path, "_test.go") {
					return nil
				}
				content, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				for _, forbidden := range []string{
					"Backfill", "DryRun", "Reembed", "Checkpoint", "HistoricalScan",
				} {
					if strings.Contains(string(content), forbidden) {
						t.Fatalf("%s contains backfill-only symbol %q", path, forbidden)
					}
				}
				return nil
			},
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "apps", "api", "cmd"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if strings.Contains(name, "semantic") && strings.Contains(name, "backfill") {
			t.Fatalf("unexpected semantic backfill command %q", entry.Name())
		}
	}
}
