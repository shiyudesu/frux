package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

const (
	evaluationVersion = "session-semantic-eval-v1"
	evaluationKind    = "technical_offline"
)

type evaluationSuite struct {
	Version         string             `json:"version"`
	Kind            string             `json:"kind"`
	BuilderVersion  string             `json:"builder_version"`
	LookbackSeconds int                `json:"lookback_seconds"`
	MaxSeeds        int                `json:"max_seeds"`
	Contract        evaluationContract `json:"contract"`
	Cases           []evaluationCase   `json:"cases"`
}

type evaluationContract struct {
	ProviderAlias            string `json:"provider_alias"`
	ModelAlias               string `json:"model_alias"`
	RevisionAlias            string `json:"revision_alias"`
	Dimension                int    `json:"dimension"`
	TextCanonicalizer        string `json:"text_canonicalizer"`
	FrameSamplingPolicy      string `json:"frame_sampling_policy"`
	ImagePreprocessingPolicy string `json:"image_preprocessing_policy"`
	FusionPolicy             string `json:"fusion_policy"`
}

type evaluationCase struct {
	Name               string                `json:"name"`
	Now                time.Time             `json:"now"`
	CurrentVideoID     int64                 `json:"current_video_id"`
	RecentVideoIDs     []int64               `json:"recent_video_ids"`
	MinPositiveSignals int                   `json:"min_positive_signals"`
	MinConfidence      float64               `json:"min_confidence"`
	Budget             int                   `json:"budget"`
	Facts              []evaluationFact      `json:"facts"`
	Vectors            []evaluationVector    `json:"vectors"`
	Candidates         []evaluationCandidate `json:"candidates"`
	Expected           evaluationExpected    `json:"expected"`
}

type evaluationFact struct {
	VideoID               int64              `json:"video_id"`
	EncounteredAgeSeconds int                `json:"encountered_age_seconds"`
	Signals               []evaluationSignal `json:"signals"`
}

type evaluationSignal struct {
	Kind       string `json:"kind"`
	AgeSeconds int    `json:"age_seconds"`
}

type evaluationVector struct {
	VideoID int64   `json:"video_id"`
	Axis    int     `json:"axis"`
	Sign    float64 `json:"sign"`
}

type evaluationCandidate struct {
	VideoID             int64   `json:"video_id"`
	Axis                int     `json:"axis"`
	Sign                float64 `json:"sign"`
	PublishedAgeSeconds int     `json:"published_age_seconds"`
}

type evaluationExpected struct {
	Result          string  `json:"result"`
	ConfidenceBand  string  `json:"confidence_band"`
	Available       bool    `json:"available"`
	OrderedVideoIDs []int64 `json:"ordered_video_ids"`
}

type evaluationReport struct {
	Version            string                 `json:"version"`
	Kind               string                 `json:"kind"`
	BuilderVersion     string                 `json:"builder_version"`
	ContractKey        string                 `json:"contract_key"`
	ExternalModelCalls int                    `json:"external_model_calls"`
	Result             string                 `json:"result"`
	Cases              []evaluationCaseReport `json:"cases"`
}

type evaluationCaseReport struct {
	Name            string  `json:"name"`
	Result          string  `json:"result"`
	Confidence      float64 `json:"confidence"`
	ConfidenceBand  string  `json:"confidence_band"`
	Available       bool    `json:"available"`
	EligibleCount   int     `json:"eligible_count"`
	PositiveCount   int     `json:"positive_count"`
	NegativeCount   int     `json:"negative_count"`
	CompatibleCount int     `json:"compatible_count"`
	ExcludedCount   int     `json:"excluded_count"`
	OrderedVideoIDs []int64 `json:"ordered_video_ids,omitempty"`
	Passed          bool    `json:"passed"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "session semantic evaluation failed")
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("session-semantic-eval", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	fixturePath := flags.String("fixture", "", "versioned session semantic fixture path")
	reportPath := flags.String("report", "", "optional JSON report path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return errors.New("invalid evaluation options")
	}
	path := strings.TrimSpace(*fixturePath)
	if path == "" {
		var err error
		path, err = discoverFixturePath()
		if err != nil {
			return err
		}
	}
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil || len(content) > 1<<20 {
		return errors.New("invalid evaluation fixture")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var suite evaluationSuite
	if err := decoder.Decode(&suite); err != nil {
		return errors.New("invalid evaluation fixture")
	}
	report, evaluationErr := evaluateSuite(context.Background(), suite)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if _, err := output.Write(encoded); err != nil {
		return err
	}
	if strings.TrimSpace(*reportPath) != "" {
		if err := writeEvaluationReport(filepath.Clean(*reportPath), encoded); err != nil {
			return err
		}
	}
	return evaluationErr
}

func writeEvaluationReport(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func discoverFixturePath() (string, error) {
	for _, candidate := range []string{
		"testdata/session-semantic-v1.json",
		"../../testdata/session-semantic-v1.json",
		"apps/api/testdata/session-semantic-v1.json",
	} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", errors.New("session semantic fixture not found")
}

func evaluateSuite(ctx context.Context, suite evaluationSuite) (evaluationReport, error) {
	report := evaluationReport{
		Version: evaluationVersion, Kind: evaluationKind,
		BuilderVersion: suite.BuilderVersion, ExternalModelCalls: 0, Result: "failed",
		Cases: []evaluationCaseReport{},
	}
	contract, err := domainembedding.NewMultimodalContractIdentity(
		suite.Contract.ProviderAlias, suite.Contract.ModelAlias, suite.Contract.RevisionAlias,
		suite.Contract.Dimension, suite.Contract.TextCanonicalizer,
		suite.Contract.FrameSamplingPolicy, suite.Contract.ImagePreprocessingPolicy,
		suite.Contract.FusionPolicy,
	)
	if err != nil || suite.Version != evaluationVersion || suite.Kind != evaluationKind ||
		suite.BuilderVersion != domainrecommendation.SessionSemanticBuilderV1 ||
		suite.LookbackSeconds < domainrecommendation.MinSessionSemanticLookbackSeconds ||
		suite.LookbackSeconds > domainrecommendation.MaxSessionSemanticLookbackSeconds ||
		suite.MaxSeeds < 1 || suite.MaxSeeds > domainrecommendation.MaxSessionSemanticSeeds ||
		len(suite.Cases) == 0 || len(suite.Cases) > 100 {
		return report, errors.New("invalid evaluation suite")
	}
	report.ContractKey = contract.Key()
	allPassed := true
	for _, fixture := range suite.Cases {
		caseReport, caseErr := evaluateCase(ctx, suite, contract, fixture)
		if caseErr != nil {
			allPassed = false
		}
		report.Cases = append(report.Cases, caseReport)
	}
	if allPassed {
		report.Result = "success"
		return report, nil
	}
	return report, errors.New("evaluation expectation failed")
}

func evaluateCase(
	ctx context.Context,
	suite evaluationSuite,
	contract domainembedding.MultimodalContractIdentity,
	fixture evaluationCase,
) (evaluationCaseReport, error) {
	result := evaluationCaseReport{Name: strings.TrimSpace(fixture.Name), OrderedVideoIDs: []int64{}}
	if result.Name == "" || fixture.Now.IsZero() || fixture.Budget < 1 ||
		fixture.Budget > domainrecommendation.MaxRecallBudget {
		return result, errors.New("invalid evaluation case")
	}
	recommendationContext, err := domainrecommendation.NewRecommendationContext(domainrecommendation.RecommendationContextInput{
		RequestID: "eval-" + result.Name, SessionID: "technical-offline",
		CurrentVideoID: fixture.CurrentVideoID, RecentVideoIDs: fixture.RecentVideoIDs,
		NetworkClass:  domainrecommendation.NetworkClassUnknown,
		ViewportClass: domainrecommendation.ViewportClassUnknown,
	})
	if err != nil {
		return result, err
	}
	facts, err := evaluationFacts(fixture)
	if err != nil {
		return result, err
	}
	vectors, err := evaluationVectors(contract, fixture.Vectors)
	if err != nil {
		return result, err
	}
	builder, err := applicationrecommendation.NewSessionSemanticBuilder(
		evaluationFactSource{facts: facts}, evaluationVectorSource{vectors: vectors},
		applicationrecommendation.WithSessionSemanticRuntimeLimits(
			suite.MaxSeeds, time.Duration(suite.LookbackSeconds)*time.Second,
		),
	)
	if err != nil {
		return result, err
	}
	exact, err := newEvaluationExact(contract, fixture.Now, fixture.Candidates)
	if err != nil {
		return result, err
	}
	provider, err := applicationrecommendation.NewSemanticSessionProvider(builder, exact, contract)
	if err != nil {
		return result, err
	}
	config := domainrecommendation.InitialRecommendationPolicyConfiguration()
	config.FeatureWeights = map[string]float64{domainrecommendation.FeatureSemanticSimilarity: 1}
	config.RecallBudgets = map[string]int{domainrecommendation.RecallProviderSemanticSession: fixture.Budget}
	config.ProviderDeadlinesMS = map[string]int{domainrecommendation.RecallProviderSemanticSession: 500}
	config.SessionSemantic = &domainrecommendation.SessionSemanticPolicyConfiguration{
		BuilderVersion: suite.BuilderVersion, ContractKey: contract.Key(),
		LookbackSeconds: suite.LookbackSeconds, MaxSeeds: suite.MaxSeeds,
		MinPositiveSignals: fixture.MinPositiveSignals, MinConfidence: fixture.MinConfidence,
	}
	policy, err := domainrecommendation.NewPolicy("recommend", 1, false, config, fixture.Now)
	if err != nil {
		return result, err
	}
	candidates, evidence, providerErr := provider.RecallWithSessionSemanticEvidence(ctx, applicationrecommendation.RecallRequest{
		UserID: 1, Scene: "recommend", Context: recommendationContext,
		Budget: fixture.Budget, Now: fixture.Now, Policy: policy,
	})
	if providerErr != nil || evidence == nil {
		return result, errors.New("evaluation provider failed")
	}
	result.Result = string(evidence.Result)
	result.Confidence = evidence.Confidence
	result.ConfidenceBand = string(evidence.ConfidenceBand)
	result.Available = len(candidates) > 0
	result.EligibleCount = evidence.EligibleCount
	result.PositiveCount = evidence.PositiveCount
	result.NegativeCount = evidence.NegativeCount
	result.CompatibleCount = evidence.CompatibleCount
	result.ExcludedCount = evidence.ExcludedCount
	for _, candidate := range candidates {
		result.OrderedVideoIDs = append(result.OrderedVideoIDs, candidate.VideoID)
	}
	result.Passed = result.Result == fixture.Expected.Result &&
		result.ConfidenceBand == fixture.Expected.ConfidenceBand &&
		result.Available == fixture.Expected.Available &&
		equalInt64s(result.OrderedVideoIDs, fixture.Expected.OrderedVideoIDs)
	if !result.Passed {
		return result, errors.New("evaluation expectation failed")
	}
	return result, nil
}

func evaluationFacts(fixture evaluationCase) ([]applicationrecommendation.SessionSemanticFact, error) {
	facts := make([]applicationrecommendation.SessionSemanticFact, 0, len(fixture.Facts))
	for _, value := range fixture.Facts {
		if value.VideoID <= 0 || value.EncounteredAgeSeconds < 0 {
			return nil, errors.New("invalid evaluation fact")
		}
		fact := applicationrecommendation.SessionSemanticFact{
			VideoID:       value.VideoID,
			EncounteredAt: fixture.Now.Add(-time.Duration(value.EncounteredAgeSeconds) * time.Second),
			Signals:       make([]domainrecommendation.SessionSemanticSignal, 0, len(value.Signals)),
		}
		for _, signal := range value.Signals {
			kind := domainrecommendation.SessionSemanticSignalKind(strings.ToLower(strings.TrimSpace(signal.Kind)))
			if !domainrecommendation.ValidSessionSemanticSignalKind(kind) || signal.AgeSeconds < 0 {
				return nil, errors.New("invalid evaluation signal")
			}
			fact.Signals = append(fact.Signals, domainrecommendation.SessionSemanticSignal{
				VideoID: value.VideoID, Kind: kind,
				OccurredAt: fixture.Now.Add(-time.Duration(signal.AgeSeconds) * time.Second),
			})
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func evaluationVectors(
	contract domainembedding.MultimodalContractIdentity,
	values []evaluationVector,
) (map[int64]*domainembedding.MultimodalVectorFact, error) {
	vectors := make(map[int64]*domainembedding.MultimodalVectorFact, len(values))
	for _, value := range values {
		vector, err := evaluationUnitVector(contract.Dimension, value.Axis, value.Sign)
		if err != nil || value.VideoID <= 0 {
			return nil, errors.New("invalid evaluation vector")
		}
		vectors[value.VideoID] = &domainembedding.MultimodalVectorFact{
			VideoID:  value.VideoID,
			Identity: domainembedding.MultimodalVectorIdentity{Contract: contract},
			Values:   vector,
		}
	}
	return vectors, nil
}

type evaluationFactSource struct {
	facts []applicationrecommendation.SessionSemanticFact
}

func (s evaluationFactSource) LoadSessionSemanticFacts(
	context.Context, int64, []int64, time.Time, time.Time,
) ([]applicationrecommendation.SessionSemanticFact, error) {
	return append([]applicationrecommendation.SessionSemanticFact(nil), s.facts...), nil
}

type evaluationVectorSource struct {
	vectors map[int64]*domainembedding.MultimodalVectorFact
}

func (s evaluationVectorSource) LoadSessionSemanticVectors(
	_ context.Context,
	ids []int64,
	_ domainembedding.MultimodalContractIdentity,
) (map[int64]*domainembedding.MultimodalVectorFact, error) {
	result := make(map[int64]*domainembedding.MultimodalVectorFact, len(ids))
	for _, id := range ids {
		if vector := s.vectors[id]; vector != nil {
			result[id] = vector.Clone()
		}
	}
	return result, nil
}

type evaluationExact struct {
	contract   domainembedding.MultimodalContractIdentity
	candidates []evaluationExactCandidate
}

type evaluationExactCandidate struct {
	videoID     int64
	vector      []float64
	publishedAt time.Time
}

func newEvaluationExact(
	contract domainembedding.MultimodalContractIdentity,
	now time.Time,
	values []evaluationCandidate,
) (*evaluationExact, error) {
	candidates := make([]evaluationExactCandidate, 0, len(values))
	for _, value := range values {
		vector, err := evaluationUnitVector(contract.Dimension, value.Axis, value.Sign)
		if err != nil || value.VideoID <= 0 || value.PublishedAgeSeconds < 0 {
			return nil, errors.New("invalid evaluation candidate")
		}
		candidates = append(candidates, evaluationExactCandidate{
			videoID: value.VideoID, vector: vector,
			publishedAt: now.Add(-time.Duration(value.PublishedAgeSeconds) * time.Second),
		})
	}
	return &evaluationExact{contract: contract, candidates: candidates}, nil
}

func (e *evaluationExact) ExactMultimodalSearch(
	_ context.Context,
	contract domainembedding.MultimodalContractIdentity,
	query []float64,
	exclusions []int64,
	limit int,
) ([]domainembedding.MultimodalExactCandidate, error) {
	if e == nil || !contract.Equal(e.contract) || limit <= 0 {
		return nil, errors.New("invalid evaluation exact query")
	}
	excluded := make(map[int64]struct{}, len(exclusions))
	for _, id := range exclusions {
		excluded[id] = struct{}{}
	}
	result := make([]domainembedding.MultimodalExactCandidate, 0, len(e.candidates))
	for _, candidate := range e.candidates {
		if _, skip := excluded[candidate.videoID]; skip {
			continue
		}
		similarity, err := domainembedding.CosineSimilarity(query, candidate.vector)
		if err != nil || similarity <= 0 || math.IsNaN(similarity) || math.IsInf(similarity, 0) {
			continue
		}
		result = append(result, domainembedding.MultimodalExactCandidate{
			VideoID: candidate.videoID, Similarity: similarity, PublishedAt: candidate.publishedAt,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Similarity != result[j].Similarity {
			return result[i].Similarity > result[j].Similarity
		}
		if !result[i].PublishedAt.Equal(result[j].PublishedAt) {
			return result[i].PublishedAt.After(result[j].PublishedAt)
		}
		return result[i].VideoID > result[j].VideoID
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func evaluationUnitVector(dimension, axis int, sign float64) ([]float64, error) {
	if dimension < domainembedding.MinMultimodalDimension || axis < 0 || axis >= dimension ||
		(sign != 1 && sign != -1) {
		return nil, errors.New("invalid unit vector")
	}
	vector := make([]float64, dimension)
	vector[axis] = sign
	return vector, nil
}

func equalInt64s(left, right []int64) bool {
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
