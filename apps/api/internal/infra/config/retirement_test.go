package infraconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestActiveRuntimeContainsNoRetiredBrokerConfiguration(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", "..", ".."))
	targets := []string{
		filepath.Join(root, "apps", "api", "cmd"),
		filepath.Join(root, "apps", "api", "internal"),
		filepath.Join(root, "apps", "api", "configs"),
		filepath.Join(root, "apps", "api", "go.mod"),
		filepath.Join(root, "apps", "api", "go.sum"),
		filepath.Join(root, "apps", "docker-compose.yml"),
		filepath.Join(root, "apps", "deploy.yaml"),
	}
	forbidden := []string{
		"github.com/" + "rabbit" + "mq",
		"internal/infra/" + "mq",
		"Rabbit" + "MQConfig",
		"rabbit" + "mq:",
		"rabbit_with_kafka_" + "mirror",
		"kafka_with_rabbit_" + "mirror",
		"shadow_" + "deployment",
		"producer_" + "mode",
		"consumer_" + "mode",
		"cutover_" + "boundary",
	}

	for _, target := range targets {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			assertRetiredBrokerPatternsAbsent(t, target, forbidden)
			continue
		}
		err = filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			switch filepath.Ext(path) {
			case ".go", ".yaml", ".yml":
				assertRetiredBrokerPatternsAbsent(t, path, forbidden)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func assertRetiredBrokerPatternsAbsent(t *testing.T, path string, forbidden []string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(content))
	for _, pattern := range forbidden {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			t.Fatalf("%s contains retired runtime pattern %q", path, pattern)
		}
	}
}
