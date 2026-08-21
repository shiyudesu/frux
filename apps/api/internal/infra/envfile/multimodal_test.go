package envfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMultimodalFruxRuntimeFindsRepositoryFileAndFiltersSecrets(t *testing.T) {
	root, workingDirectory := multimodalTestRepository(t)
	writeMultimodalTestEnv(t, filepath.Join(root, "apps", multimodalEnvFilename), `
FRUX_MULTIMODAL_PROFILE=tongyi-embedding-vision-flash
FRUX_MULTIMODAL_ENDPOINT=http://127.0.0.1:8099
FRUX_MULTIMODAL_HMAC_SECRET="file-secret-value-123456789012345"
DASHSCOPE_API_KEY=must-not-enter-frux-runtime
`)
	restoreMultimodalEnvironment(t)
	if err := os.Setenv("FRUX_MULTIMODAL_PROFILE", "existing-profile"); err != nil {
		t.Fatal(err)
	}
	if err := loadMultimodal(MultimodalFruxRuntime, workingDirectory); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("FRUX_MULTIMODAL_PROFILE") != "existing-profile" ||
		os.Getenv("FRUX_MULTIMODAL_ENDPOINT") != "http://127.0.0.1:8099" ||
		os.Getenv("FRUX_MULTIMODAL_HMAC_SECRET") != "file-secret-value-123456789012345" {
		t.Fatal("Frux runtime variables were not loaded with environment precedence")
	}
	if _, exists := os.LookupEnv("DASHSCOPE_API_KEY"); exists {
		t.Fatal("DashScope API key leaked into Frux runtime scope")
	}
}

func TestLoadMultimodalTongyiAdapterLoadsAdapterSecrets(t *testing.T) {
	root, workingDirectory := multimodalTestRepository(t)
	writeMultimodalTestEnv(t, filepath.Join(root, multimodalEnvFilename), `
FRUX_MULTIMODAL_PROFILE=tongyi-embedding-vision-flash-2026-03-06
DASHSCOPE_API_KEY='adapter-secret'
FRUX_TONGYI_UPSTREAM_TIMEOUT=5s
`)
	restoreMultimodalEnvironment(t)
	if err := loadMultimodal(MultimodalTongyiAdapter, workingDirectory); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("DASHSCOPE_API_KEY") != "adapter-secret" ||
		os.Getenv("FRUX_TONGYI_UPSTREAM_TIMEOUT") != "5s" {
		t.Fatal("adapter variables were not loaded")
	}
}

func TestLoadMultimodalRejectsMalformedFile(t *testing.T) {
	root, workingDirectory := multimodalTestRepository(t)
	writeMultimodalTestEnv(t, filepath.Join(root, multimodalEnvFilename), `BROKEN='value`)
	restoreMultimodalEnvironment(t)
	if err := loadMultimodal(MultimodalTongyiAdapter, workingDirectory); !errors.Is(err, ErrInvalidMultimodalEnv) {
		t.Fatalf("error=%v", err)
	}
}

func TestLoadMultimodalIgnoresMissingFileOutsideRepository(t *testing.T) {
	restoreMultimodalEnvironment(t)
	if err := loadMultimodal(MultimodalFruxRuntime, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func multimodalTestRepository(t testing.TB) (string, string) {
	t.Helper()
	root := t.TempDir()
	workingDirectory := filepath.Join(root, "apps", "api")
	for _, directory := range []string{filepath.Join(root, ".git"), workingDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root, workingDirectory
}

func writeMultimodalTestEnv(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func restoreMultimodalEnvironment(t testing.TB) {
	t.Helper()
	names := make(map[string]struct{}, len(fruxMultimodalVariables)+len(adapterMultimodalVariables))
	for name := range fruxMultimodalVariables {
		names[name] = struct{}{}
	}
	for name := range adapterMultimodalVariables {
		names[name] = struct{}{}
	}
	for name := range names {
		value, exists := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
		capturedName, capturedValue, capturedExists := name, value, exists
		t.Cleanup(func() {
			if capturedExists {
				_ = os.Setenv(capturedName, capturedValue)
			} else {
				_ = os.Unsetenv(capturedName)
			}
		})
	}
}
