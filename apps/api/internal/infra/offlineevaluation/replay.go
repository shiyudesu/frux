package infraofflineevaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	applicationofflineevaluation "github.com/shiyudesu/frux/internal/application/offlineevaluation"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

const maxReplayInputBytes = 64 << 20

type namedPolicyFile struct {
	Name          string                                   `json:"name"`
	Configuration domainrecommendation.PolicyConfiguration `json:"configuration"`
}

type replayBundleFile struct {
	Version string           `json:"version"`
	Scope   string           `json:"scope"`
	Cases   []replayCaseFile `json:"cases"`
}

type replayCaseFile struct {
	Name          string                `json:"name"`
	Candidates    []replayCandidateFile `json:"candidates"`
	ExpectedOrder []int64               `json:"expected_order"`
}

type replayCandidateFile struct {
	VideoID         int64              `json:"video_id"`
	AuthorKey       string             `json:"author_key"`
	PublishedAt     string             `json:"published_at"`
	RecallProviders []string           `json:"recall_providers"`
	ScoreComponents map[string]float64 `json:"score_components"`
}

type LoadedReplayBundle struct {
	SHA256 string
	Bundle applicationofflineevaluation.ReplayBundle
}

func LoadNamedPolicy(path string) (applicationofflineevaluation.NamedPolicy, error) {
	content, err := readBoundedJSONFile(path, 1<<20)
	if err != nil {
		return applicationofflineevaluation.NamedPolicy{}, err
	}
	var wire namedPolicyFile
	if err := decodeStrictJSON(content, &wire); err != nil {
		return applicationofflineevaluation.NamedPolicy{}, &InputError{Code: FailureSchema, Role: "policy"}
	}
	name := normalizedDatasetToken(wire.Name, 64)
	if name == "" || strings.ToLower(name) != name {
		return applicationofflineevaluation.NamedPolicy{}, &InputError{Code: FailureValue, Role: "policy"}
	}
	normalized, err := domainrecommendation.ValidatePolicyConfiguration(wire.Configuration)
	if err != nil {
		return applicationofflineevaluation.NamedPolicy{}, &InputError{Code: FailureValue, Role: "policy"}
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return applicationofflineevaluation.NamedPolicy{}, err
	}
	inputHash := sha256.Sum256(content)
	normalizedHash := sha256.Sum256(canonical)
	return applicationofflineevaluation.NamedPolicy{
		Name: name, InputSHA256: hex.EncodeToString(inputHash[:]),
		NormalizedSHA256: hex.EncodeToString(normalizedHash[:]), Config: normalized,
	}, nil
}

func LoadReplayBundle(path string) (*LoadedReplayBundle, error) {
	content, err := readBoundedJSONFile(path, maxReplayInputBytes)
	if err != nil {
		return nil, err
	}
	var wire replayBundleFile
	if err := decodeStrictJSON(content, &wire); err != nil || wire.Version != applicationofflineevaluation.ReplayVersion ||
		(wire.Scope != applicationofflineevaluation.ReplayScopeFull && wire.Scope != applicationofflineevaluation.ReplayScopeSubset) ||
		len(wire.Cases) == 0 || len(wire.Cases) > 1000 {
		return nil, &InputError{Code: FailureSchema, Role: "replay"}
	}
	bundle := applicationofflineevaluation.ReplayBundle{Version: wire.Version, Scope: wire.Scope, Cases: make([]applicationofflineevaluation.ReplayCase, 0, len(wire.Cases))}
	caseNames := make(map[string]struct{}, len(wire.Cases))
	for _, sourceCase := range wire.Cases {
		name := normalizedDatasetToken(sourceCase.Name, 128)
		if name == "" || len(sourceCase.Candidates) == 0 || len(sourceCase.Candidates) > 500 || len(sourceCase.ExpectedOrder) != len(sourceCase.Candidates) {
			return nil, &InputError{Code: FailureValue, Role: "replay"}
		}
		if _, duplicate := caseNames[name]; duplicate {
			return nil, &InputError{Code: FailureDuplicate, Role: "replay"}
		}
		caseNames[name] = struct{}{}
		converted := applicationofflineevaluation.ReplayCase{Name: name, ExpectedOrder: append([]int64(nil), sourceCase.ExpectedOrder...)}
		seenVideos := make(map[int64]struct{}, len(sourceCase.Candidates))
		for _, sourceCandidate := range sourceCase.Candidates {
			publishedAt, timeErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(sourceCandidate.PublishedAt))
			if sourceCandidate.VideoID <= 0 || timeErr != nil || publishedAt.Location() != time.UTC ||
				publishedAt.Format(time.RFC3339Nano) != sourceCandidate.PublishedAt ||
				normalizedDatasetToken(sourceCandidate.AuthorKey, 128) == "" ||
				len(sourceCandidate.RecallProviders) == 0 || len(sourceCandidate.RecallProviders) > 16 {
				return nil, &InputError{Code: FailureValue, Role: "replay"}
			}
			if _, duplicate := seenVideos[sourceCandidate.VideoID]; duplicate {
				return nil, &InputError{Code: FailureDuplicate, Role: "replay"}
			}
			seenVideos[sourceCandidate.VideoID] = struct{}{}
			providers := make([]string, len(sourceCandidate.RecallProviders))
			for index, provider := range sourceCandidate.RecallProviders {
				provider = strings.ToLower(normalizedDatasetToken(provider, 64))
				if provider == "" || (index > 0 && provider <= providers[index-1]) {
					return nil, &InputError{Code: FailureValue, Role: "replay"}
				}
				providers[index] = provider
			}
			for _, value := range sourceCandidate.ScoreComponents {
				if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
					return nil, &InputError{Code: FailureValue, Role: "replay"}
				}
			}
			converted.Candidates = append(converted.Candidates, applicationofflineevaluation.ReplayCandidate{
				VideoID: sourceCandidate.VideoID, AuthorKey: sourceCandidate.AuthorKey,
				PublishedAt: publishedAt.UTC(), RecallProviders: providers,
				ScoreComponents: cloneFloatMap(sourceCandidate.ScoreComponents),
			})
		}
		expected := append([]int64(nil), sourceCase.ExpectedOrder...)
		sortedExpected := append([]int64(nil), expected...)
		sort.Slice(sortedExpected, func(i, j int) bool { return sortedExpected[i] < sortedExpected[j] })
		candidateIDs := make([]int64, 0, len(seenVideos))
		for videoID := range seenVideos {
			candidateIDs = append(candidateIDs, videoID)
		}
		sort.Slice(candidateIDs, func(i, j int) bool { return candidateIDs[i] < candidateIDs[j] })
		if !equalInt64Slice(sortedExpected, candidateIDs) {
			return nil, &InputError{Code: FailureValue, Role: "replay"}
		}
		bundle.Cases = append(bundle.Cases, converted)
	}
	hash := sha256.Sum256(content)
	return &LoadedReplayBundle{SHA256: hex.EncodeToString(hash[:]), Bundle: bundle}, nil
}

func readBoundedJSONFile(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(strings.TrimSpace(path))
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > maximum {
		return nil, &InputError{Code: FailureFile}
	}
	content, err := os.ReadFile(path)
	if err != nil || int64(len(content)) > maximum {
		return nil, &InputError{Code: FailureFile}
	}
	return content, nil
}

func decodeStrictJSON(content []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func cloneFloatMap(values map[string]float64) map[string]float64 {
	cloned := make(map[string]float64, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func equalInt64Slice(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
