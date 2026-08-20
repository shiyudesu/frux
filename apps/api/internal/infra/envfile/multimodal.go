package envfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

const multimodalEnvFilename = ".env.multimodal"

type MultimodalScope uint8

const (
	MultimodalFruxRuntime MultimodalScope = iota + 1
	MultimodalTongyiAdapter
)

var ErrInvalidMultimodalEnv = errors.New("invalid multimodal environment file")

var fruxMultimodalVariables = map[string]struct{}{
	"FRUX_MULTIMODAL_PROFILE":     {},
	"FRUX_MULTIMODAL_ENDPOINT":    {},
	"FRUX_MULTIMODAL_HMAC_SECRET": {},
}

var adapterMultimodalVariables = map[string]struct{}{
	"FRUX_MULTIMODAL_PROFILE":              {},
	"FRUX_MULTIMODAL_PROVIDER_LISTEN_ADDR": {},
	"FRUX_MULTIMODAL_HMAC_SECRET":          {},
	"DASHSCOPE_MULTIMODAL_ENDPOINT":        {},
	"DASHSCOPE_API_KEY":                    {},
	"FRUX_TONGYI_UPSTREAM_TIMEOUT":         {},
	"FRUX_TONGYI_MAX_REQUEST_BYTES":        {},
	"FRUX_TONGYI_MAX_RESPONSE_BYTES":       {},
	"FRUX_TONGYI_SHUTDOWN_TIMEOUT":         {},
}

func LoadMultimodal(scope MultimodalScope) error {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", ErrInvalidMultimodalEnv)
	}
	return loadMultimodal(scope, workingDirectory)
}

func loadMultimodal(scope MultimodalScope, workingDirectory string) error {
	allowed, err := multimodalVariables(scope)
	if err != nil {
		return err
	}
	path, found, err := findMultimodalEnvFile(workingDirectory)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	values, err := godotenv.Read(path)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, ErrInvalidMultimodalEnv)
	}
	for name := range allowed {
		if _, exists := os.LookupEnv(name); exists {
			continue
		}
		value, exists := values[name]
		if !exists {
			continue
		}
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("set %s: %w", name, ErrInvalidMultimodalEnv)
		}
	}
	return nil
}

func multimodalVariables(scope MultimodalScope) (map[string]struct{}, error) {
	switch scope {
	case MultimodalFruxRuntime:
		return fruxMultimodalVariables, nil
	case MultimodalTongyiAdapter:
		return adapterMultimodalVariables, nil
	default:
		return nil, ErrInvalidMultimodalEnv
	}
}

func findMultimodalEnvFile(workingDirectory string) (string, bool, error) {
	workingDirectory, err := filepath.Abs(workingDirectory)
	if err != nil {
		return "", false, fmt.Errorf("resolve multimodal environment path: %w", ErrInvalidMultimodalEnv)
	}
	directories := make([]string, 0, 4)
	repositoryRoot := ""
	current := filepath.Clean(workingDirectory)
	for {
		directories = append(directories, current)
		if _, statErr := os.Stat(filepath.Join(current, ".git")); statErr == nil {
			repositoryRoot = current
			break
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", false, fmt.Errorf("inspect repository root: %w", ErrInvalidMultimodalEnv)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if repositoryRoot == "" {
		directories = directories[:1]
	}
	candidates := make([]string, 0, len(directories)+1)
	for _, directory := range directories {
		candidates = append(candidates, filepath.Join(directory, multimodalEnvFilename))
	}
	if repositoryRoot != "" {
		candidates = append(candidates, filepath.Join(repositoryRoot, "apps", multimodalEnvFilename))
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, duplicate := seen[candidate]; duplicate {
			continue
		}
		seen[candidate] = struct{}{}
		info, statErr := os.Stat(candidate)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil || !info.Mode().IsRegular() {
			return "", false, fmt.Errorf("inspect %s: %w", candidate, ErrInvalidMultimodalEnv)
		}
		return candidate, true, nil
	}
	return "", false, nil
}
