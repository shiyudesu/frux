package shadowevaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

const FixtureSchemaV1 = "session-semantic-shadow-fixture/v1"
const ReportSchemaV1 = "session-semantic-shadow-report/v1"
const ToolVersionV1 = "session-semantic-shadow-eval/v1"
const maxFixtureBytes = 4 << 20
const maxCases = 1000
const maxCandidateIDLength = 128

var ErrInvalidFixture = errors.New("invalid session semantic shadow fixture")
var ErrInvalidReport = errors.New("invalid session semantic shadow report")
var ErrInvalidOutput = errors.New("invalid session semantic shadow output")

type Provenance struct {
	Name       string `json:"name"`
	Release    string `json:"release"`
	SourcePath string `json:"source_path"`
	License    string `json:"license"`
}

type Candidate struct {
	ID     string `json:"id"`
	Author string `json:"author"`
}

type Case struct {
	ID                string      `json:"id"`
	Available         bool        `json:"available"`
	UnavailableReason string      `json:"unavailable_reason,omitempty"`
	TopK              int         `json:"top_k"`
	Active            []Candidate `json:"active"`
	Semantic          []Candidate `json:"semantic"`
	Mixed             []Candidate `json:"mixed"`
	Ranked            []Candidate `json:"ranked"`
	Relevant          []string    `json:"relevant"`
}

type Fixture struct {
	Schema     string     `json:"schema"`
	Provenance Provenance `json:"provenance"`
	Cases      []Case     `json:"cases"`
}

type Ratio struct {
	Numerator   int     `json:"numerator"`
	Denominator int     `json:"denominator"`
	Value       float64 `json:"value"`
}

type MetricAverage struct {
	Samples int     `json:"samples"`
	Value   float64 `json:"value"`
}

type Report struct {
	Schema             string           `json:"schema"`
	ToolVersion        string           `json:"tool_version"`
	InputSHA256        string           `json:"input_sha256"`
	ContractKey        string           `json:"contract_key,omitempty"`
	FixtureSchema      string           `json:"fixture_schema"`
	Provenance         Provenance       `json:"provenance"`
	Status             string           `json:"status"`
	ExternalModelCalls int              `json:"external_model_calls"`
	Cases              CaseCounts       `json:"cases"`
	CandidateMetrics   CandidateMetrics `json:"candidate_metrics"`
	RelevanceMetrics   RelevanceMetrics `json:"relevance_metrics"`
	Warnings           []string         `json:"warnings"`
	Limitations        []string         `json:"limitations"`
}

type CaseCounts struct {
	Total       int `json:"total"`
	Available   int `json:"available"`
	Unavailable int `json:"unavailable"`
	Labeled     int `json:"labeled"`
	Minimum     int `json:"minimum"`
}

type CandidateMetrics struct {
	Active                 int           `json:"active"`
	Semantic               int           `json:"semantic"`
	Intersection           int           `json:"intersection"`
	UniqueSemantic         int           `json:"unique_semantic"`
	PoolSurvival           int           `json:"pool_survival"`
	RankSurvival           int           `json:"rank_survival"`
	UniqueRankSurvival     int           `json:"unique_rank_survival"`
	ActiveDisplaced        int           `json:"active_displaced"`
	Overlap                MetricAverage `json:"overlap"`
	UniqueContribution     MetricAverage `json:"unique_contribution"`
	PoolSurvivalRatio      MetricAverage `json:"pool_survival_ratio"`
	RankSurvivalRatio      MetricAverage `json:"rank_survival_ratio"`
	UniqueRankSurvivalRate MetricAverage `json:"unique_rank_survival_ratio"`
	ActiveDisplacementRate MetricAverage `json:"active_displacement_ratio"`
}

type RelevanceMetrics struct {
	ActivePrecision    MetricAverage `json:"active_precision_at_k"`
	SimulatedPrecision MetricAverage `json:"simulated_precision_at_k"`
	ActiveRecall       MetricAverage `json:"active_recall_at_k"`
	SimulatedRecall    MetricAverage `json:"simulated_recall_at_k"`
	ActiveNDCG         MetricAverage `json:"active_ndcg_at_k"`
	SimulatedNDCG      MetricAverage `json:"simulated_ndcg_at_k"`
}

func Load(path string) (*Fixture, string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxFixtureBytes {
		return nil, "", ErrInvalidFixture
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, "", ErrInvalidFixture
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var fixture Fixture
	if err := decoder.Decode(&fixture); err != nil {
		return nil, "", ErrInvalidFixture
	}
	if err := ensureJSONEOF(decoder); err != nil || validateFixture(&fixture) != nil {
		return nil, "", ErrInvalidFixture
	}
	sum := sha256.Sum256(payload)
	return &fixture, hex.EncodeToString(sum[:]), nil
}

func LoadReport(path string) (*Report, string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxFixtureBytes {
		return nil, "", ErrInvalidReport
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, "", ErrInvalidReport
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var report Report
	if err := decoder.Decode(&report); err != nil {
		return nil, "", ErrInvalidReport
	}
	if err := ensureJSONEOF(decoder); err != nil || ValidateRolloutReport(&report) != nil {
		return nil, "", ErrInvalidReport
	}
	sum := sha256.Sum256(payload)
	return &report, hex.EncodeToString(sum[:]), nil
}

func ValidateRolloutReport(report *Report) error {
	if report == nil || report.Schema != ReportSchemaV1 || report.ToolVersion != ToolVersionV1 ||
		report.FixtureSchema != FixtureSchemaV1 || report.Status != "complete" ||
		report.ExternalModelCalls != 0 || len(report.InputSHA256) != sha256.Size*2 ||
		len(report.ContractKey) != sha256.Size*2 ||
		report.Cases.Available < 5 || report.Cases.Labeled < 5 ||
		report.Cases.Available > report.Cases.Total || report.Cases.Labeled > report.Cases.Available ||
		report.CandidateMetrics.UniqueSemantic <= 0 || report.CandidateMetrics.RankSurvival <= 0 ||
		report.CandidateMetrics.UniqueContribution.Samples < 5 ||
		report.CandidateMetrics.UniqueContribution.Value <= 0 ||
		report.CandidateMetrics.RankSurvivalRatio.Samples < 5 ||
		report.CandidateMetrics.RankSurvivalRatio.Value <= 0 ||
		report.RelevanceMetrics.ActiveNDCG.Samples < 5 ||
		report.RelevanceMetrics.SimulatedNDCG.Samples < 5 ||
		!finiteUnit(report.RelevanceMetrics.ActiveNDCG.Value) ||
		!finiteUnit(report.RelevanceMetrics.SimulatedNDCG.Value) ||
		report.RelevanceMetrics.SimulatedNDCG.Value < report.RelevanceMetrics.ActiveNDCG.Value {
		return ErrInvalidReport
	}
	if decoded, err := hex.DecodeString(report.InputSHA256); err != nil || len(decoded) != sha256.Size {
		return ErrInvalidReport
	}
	if decoded, err := hex.DecodeString(report.ContractKey); err != nil || len(decoded) != sha256.Size ||
		report.ContractKey != strings.ToLower(report.ContractKey) {
		return ErrInvalidReport
	}
	return nil
}

func BindReportContract(report *Report, contractKey string) error {
	contractKey = strings.ToLower(strings.TrimSpace(contractKey))
	decoded, err := hex.DecodeString(contractKey)
	if report == nil || err != nil || len(decoded) != sha256.Size {
		return ErrInvalidReport
	}
	report.ContractKey = contractKey
	return nil
}

func finiteUnit(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrInvalidFixture
	}
	return nil
}

func validateFixture(fixture *Fixture) error {
	if fixture == nil || fixture.Schema != FixtureSchemaV1 ||
		!boundedText(fixture.Provenance.Name, 1, 128) ||
		!boundedText(fixture.Provenance.Release, 1, 128) ||
		!boundedText(fixture.Provenance.License, 1, 256) ||
		!safeRelativePath(fixture.Provenance.SourcePath) ||
		len(fixture.Cases) == 0 || len(fixture.Cases) > maxCases {
		return ErrInvalidFixture
	}
	caseIDs := make(map[string]struct{}, len(fixture.Cases))
	for index := range fixture.Cases {
		current := &fixture.Cases[index]
		current.ID = strings.TrimSpace(current.ID)
		current.UnavailableReason = strings.ToLower(strings.TrimSpace(current.UnavailableReason))
		if !boundedText(current.ID, 1, 128) {
			return ErrInvalidFixture
		}
		if _, duplicate := caseIDs[current.ID]; duplicate {
			return ErrInvalidFixture
		}
		caseIDs[current.ID] = struct{}{}
		if !current.Available {
			if !validUnavailableReason(current.UnavailableReason) || current.TopK != 0 ||
				len(current.Active)+len(current.Semantic)+len(current.Mixed)+len(current.Ranked)+len(current.Relevant) != 0 {
				return ErrInvalidFixture
			}
			continue
		}
		if current.UnavailableReason != "" || current.TopK < 1 || current.TopK > domainrecommendation.MaxPolicyPreRankCandidates ||
			len(current.Active) == 0 || len(current.Active) > domainrecommendation.MaxPolicyPreRankCandidates ||
			len(current.Semantic) > domainrecommendation.MaxPolicyPreRankCandidates ||
			len(current.Mixed) == 0 || len(current.Mixed) > domainrecommendation.MaxPolicyPreRankCandidates ||
			len(current.Ranked) == 0 || len(current.Ranked) > domainrecommendation.MaxPolicyPreRankCandidates ||
			current.TopK > len(current.Ranked) {
			return ErrInvalidFixture
		}
		lists := [][]Candidate{current.Active, current.Semantic, current.Mixed, current.Ranked}
		metadata := make(map[string]string)
		for _, list := range lists {
			if validateCandidateList(list) != nil {
				return ErrInvalidFixture
			}
			for _, candidate := range list {
				if author, exists := metadata[candidate.ID]; exists && author != candidate.Author {
					return ErrInvalidFixture
				}
				metadata[candidate.ID] = candidate.Author
			}
		}
		union := candidateIDSet(append(append([]Candidate{}, current.Active...), current.Semantic...))
		mixed := candidateIDSet(current.Mixed)
		if !setSubset(mixed, union) || !setSubset(candidateIDSet(current.Ranked), mixed) ||
			validateRelevant(current.Relevant, union) != nil {
			return ErrInvalidFixture
		}
	}
	return nil
}

func validateCandidateList(candidates []Candidate) error {
	seen := make(map[string]struct{}, len(candidates))
	for index := range candidates {
		candidates[index].ID = strings.TrimSpace(candidates[index].ID)
		candidates[index].Author = strings.TrimSpace(candidates[index].Author)
		if !boundedText(candidates[index].ID, 1, maxCandidateIDLength) ||
			!boundedText(candidates[index].Author, 1, maxCandidateIDLength) {
			return ErrInvalidFixture
		}
		if _, duplicate := seen[candidates[index].ID]; duplicate {
			return ErrInvalidFixture
		}
		seen[candidates[index].ID] = struct{}{}
	}
	return nil
}

func validateRelevant(values []string, candidates map[string]struct{}) error {
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if !boundedText(value, 1, maxCandidateIDLength) {
			return ErrInvalidFixture
		}
		if _, exists := candidates[value]; !exists {
			return ErrInvalidFixture
		}
		if _, duplicate := seen[value]; duplicate {
			return ErrInvalidFixture
		}
		seen[value] = struct{}{}
	}
	return nil
}

func Evaluate(fixture *Fixture, inputSHA string, minimumCases int) (*Report, error) {
	if validateFixture(fixture) != nil || len(inputSHA) != sha256.Size*2 || minimumCases < 1 || minimumCases > maxCases {
		return nil, ErrInvalidFixture
	}
	if decoded, err := hex.DecodeString(inputSHA); err != nil || len(decoded) != sha256.Size {
		return nil, ErrInvalidFixture
	}
	report := &Report{
		Schema: ReportSchemaV1, ToolVersion: ToolVersionV1, InputSHA256: strings.ToLower(inputSHA),
		FixtureSchema: fixture.Schema, Provenance: fixture.Provenance, ExternalModelCalls: 0,
		Cases: CaseCounts{Total: len(fixture.Cases), Minimum: minimumCases},
		Limitations: []string{
			"Results are deterministic offline Shadow evidence, not an online causal experiment.",
			"No policy is activated or recommended automatically.",
			"Candidate identities, histories, vectors, credentials, and runtime payloads are excluded from the report.",
		},
	}
	var candidateSums [6]float64
	var relevanceSums [6]float64
	relevanceSamples := 0
	for _, fixtureCase := range fixture.Cases {
		if !fixtureCase.Available {
			report.Cases.Unavailable++
			continue
		}
		report.Cases.Available++
		active, semantic, mixed, ranked := caseCandidates(fixtureCase)
		comparison := applicationrecommendation.CompareSessionSemanticShadow(
			active, semantic, mixed, ranked, fixtureCase.TopK,
		)
		report.CandidateMetrics.Active += comparison.ActiveCount
		report.CandidateMetrics.Semantic += comparison.SemanticCount
		report.CandidateMetrics.Intersection += comparison.IntersectionCount
		report.CandidateMetrics.UniqueSemantic += comparison.UniqueSemanticCount
		report.CandidateMetrics.PoolSurvival += comparison.SemanticPoolSurvival
		report.CandidateMetrics.RankSurvival += comparison.SemanticRankSurvival
		report.CandidateMetrics.UniqueRankSurvival += comparison.UniqueRankSurvival
		report.CandidateMetrics.ActiveDisplaced += comparison.ActiveTopKDisplaced
		candidateSums[0] += comparison.OverlapRatio
		candidateSums[1] += comparison.UniqueContributionRatio
		candidateSums[2] += comparison.PoolSurvivalRatio
		candidateSums[3] += comparison.RankSurvivalRatio
		candidateSums[4] += comparison.UniqueRankSurvivalRatio
		candidateSums[5] += comparison.ActiveTopKDisplacedRatio
		if len(fixtureCase.Relevant) == 0 {
			continue
		}
		report.Cases.Labeled++
		relevanceSamples++
		relevant := stringSet(fixtureCase.Relevant)
		activeTop := candidateIDs(fixtureCase.Active, fixtureCase.TopK)
		rankedTop := candidateIDs(fixtureCase.Ranked, fixtureCase.TopK)
		relevanceSums[0] += precisionAtK(activeTop, relevant)
		relevanceSums[1] += precisionAtK(rankedTop, relevant)
		relevanceSums[2] += recallAtK(activeTop, relevant)
		relevanceSums[3] += recallAtK(rankedTop, relevant)
		relevanceSums[4] += ndcgAtK(activeTop, relevant)
		relevanceSums[5] += ndcgAtK(rankedTop, relevant)
	}
	report.CandidateMetrics.Overlap = average(candidateSums[0], report.Cases.Available)
	report.CandidateMetrics.UniqueContribution = average(candidateSums[1], report.Cases.Available)
	report.CandidateMetrics.PoolSurvivalRatio = average(candidateSums[2], report.Cases.Available)
	report.CandidateMetrics.RankSurvivalRatio = average(candidateSums[3], report.Cases.Available)
	report.CandidateMetrics.UniqueRankSurvivalRate = average(candidateSums[4], report.Cases.Available)
	report.CandidateMetrics.ActiveDisplacementRate = average(candidateSums[5], report.Cases.Available)
	report.RelevanceMetrics.ActivePrecision = average(relevanceSums[0], relevanceSamples)
	report.RelevanceMetrics.SimulatedPrecision = average(relevanceSums[1], relevanceSamples)
	report.RelevanceMetrics.ActiveRecall = average(relevanceSums[2], relevanceSamples)
	report.RelevanceMetrics.SimulatedRecall = average(relevanceSums[3], relevanceSamples)
	report.RelevanceMetrics.ActiveNDCG = average(relevanceSums[4], relevanceSamples)
	report.RelevanceMetrics.SimulatedNDCG = average(relevanceSums[5], relevanceSamples)
	if report.Cases.Available < minimumCases || report.Cases.Labeled < minimumCases {
		report.Status = "inconclusive"
		if report.Cases.Available < minimumCases {
			report.Warnings = append(report.Warnings, "available case count is below the registered minimum")
		}
		if report.Cases.Labeled < minimumCases {
			report.Warnings = append(report.Warnings, "labeled case count is below the registered minimum")
		}
	} else {
		report.Status = "complete"
	}
	sort.Strings(report.Warnings)
	return report, nil
}

func Write(report *Report, jsonPath, markdownPath string) error {
	if report == nil || (report.Status != "complete" && report.Status != "inconclusive") ||
		jsonPath == "" || markdownPath == "" || filepath.Clean(jsonPath) == filepath.Clean(markdownPath) {
		return ErrInvalidOutput
	}
	jsonBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return ErrInvalidOutput
	}
	jsonBytes = append(jsonBytes, '\n')
	markdownBytes := renderMarkdown(report)
	jsonTemp, err := writeTemp(jsonPath, jsonBytes)
	if err != nil {
		return err
	}
	markdownTemp, err := writeTemp(markdownPath, markdownBytes)
	if err != nil {
		_ = os.Remove(jsonTemp)
		return err
	}
	defer os.Remove(jsonTemp)
	defer os.Remove(markdownTemp)
	if err := os.Rename(jsonTemp, jsonPath); err != nil {
		return ErrInvalidOutput
	}
	if err := os.Rename(markdownTemp, markdownPath); err != nil {
		return ErrInvalidOutput
	}
	_ = os.Chmod(jsonPath, 0o600)
	_ = os.Chmod(markdownPath, 0o600)
	return nil
}

func renderMarkdown(report *Report) []byte {
	var output strings.Builder
	fmt.Fprintf(&output, "# Session Semantic Shadow Evaluation\n\n")
	fmt.Fprintf(&output, "- Status: `%s`\n", report.Status)
	fmt.Fprintf(&output, "- Tool: `%s`\n", report.ToolVersion)
	fmt.Fprintf(&output, "- Input SHA-256: `%s`\n", report.InputSHA256)
	if report.ContractKey != "" {
		fmt.Fprintf(&output, "- Contract key: `%s`\n", report.ContractKey)
	}
	fmt.Fprintf(&output, "- External model calls: `%d`\n", report.ExternalModelCalls)
	fmt.Fprintf(&output, "- Cases: `%d` total, `%d` available, `%d` labeled, minimum `%d`\n\n",
		report.Cases.Total, report.Cases.Available, report.Cases.Labeled, report.Cases.Minimum)
	fmt.Fprintf(&output, "## Candidate Evidence\n\n")
	fmt.Fprintf(&output, "| Metric | Count / Mean |\n| --- | ---: |\n")
	fmt.Fprintf(&output, "| Semantic candidates | %d |\n", report.CandidateMetrics.Semantic)
	fmt.Fprintf(&output, "| Unique semantic candidates | %d |\n", report.CandidateMetrics.UniqueSemantic)
	fmt.Fprintf(&output, "| Mean unique contribution | %.6f |\n", report.CandidateMetrics.UniqueContribution.Value)
	fmt.Fprintf(&output, "| Mean pool survival | %.6f |\n", report.CandidateMetrics.PoolSurvivalRatio.Value)
	fmt.Fprintf(&output, "| Mean rank survival | %.6f |\n", report.CandidateMetrics.RankSurvivalRatio.Value)
	fmt.Fprintf(&output, "| Mean active displacement | %.6f |\n\n", report.CandidateMetrics.ActiveDisplacementRate.Value)
	fmt.Fprintf(&output, "## Relevance Evidence\n\n")
	fmt.Fprintf(&output, "| Metric | Active | Simulated | Samples |\n| --- | ---: | ---: | ---: |\n")
	fmt.Fprintf(&output, "| Precision@K | %.6f | %.6f | %d |\n", report.RelevanceMetrics.ActivePrecision.Value, report.RelevanceMetrics.SimulatedPrecision.Value, report.RelevanceMetrics.ActivePrecision.Samples)
	fmt.Fprintf(&output, "| Recall@K | %.6f | %.6f | %d |\n", report.RelevanceMetrics.ActiveRecall.Value, report.RelevanceMetrics.SimulatedRecall.Value, report.RelevanceMetrics.ActiveRecall.Samples)
	fmt.Fprintf(&output, "| NDCG@K | %.6f | %.6f | %d |\n\n", report.RelevanceMetrics.ActiveNDCG.Value, report.RelevanceMetrics.SimulatedNDCG.Value, report.RelevanceMetrics.ActiveNDCG.Samples)
	if len(report.Warnings) > 0 {
		fmt.Fprintf(&output, "## Warnings\n\n")
		for _, warning := range report.Warnings {
			fmt.Fprintf(&output, "- %s\n", warning)
		}
		output.WriteString("\n")
	}
	fmt.Fprintf(&output, "## Limitations\n\n")
	for _, limitation := range report.Limitations {
		fmt.Fprintf(&output, "- %s\n", limitation)
	}
	return []byte(output.String())
}

func writeTemp(target string, payload []byte) (string, error) {
	directory := filepath.Dir(target)
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		return "", ErrInvalidOutput
	}
	file, err := os.CreateTemp(directory, ".frux-shadow-*")
	if err != nil {
		return "", ErrInvalidOutput
	}
	name := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(name)
	}
	if err := file.Chmod(0o600); err != nil {
		cleanup()
		return "", ErrInvalidOutput
	}
	if _, err := file.Write(payload); err != nil || file.Sync() != nil || file.Close() != nil {
		cleanup()
		return "", ErrInvalidOutput
	}
	return name, nil
}

func caseCandidates(value Case) ([]*domainrecommendation.Candidate, []*domainrecommendation.Candidate, []*domainrecommendation.Candidate, []*domainrecommendation.Candidate) {
	ids := make(map[string]int64)
	authors := make(map[string]int64)
	nextID, nextAuthor := int64(1), int64(1)
	convert := func(values []Candidate) []*domainrecommendation.Candidate {
		output := make([]*domainrecommendation.Candidate, 0, len(values))
		for _, candidate := range values {
			id, exists := ids[candidate.ID]
			if !exists {
				id, nextID = nextID, nextID+1
				ids[candidate.ID] = id
			}
			author, exists := authors[candidate.Author]
			if !exists {
				author, nextAuthor = nextAuthor, nextAuthor+1
				authors[candidate.Author] = author
			}
			output = append(output, domainrecommendation.RestoreCandidate(
				id, author, 0, 0, 0, 0, "", time.Unix(id, 0).UTC(),
			))
		}
		return output
	}
	return convert(value.Active), convert(value.Semantic), convert(value.Mixed), convert(value.Ranked)
}

func precisionAtK(ranked []string, relevant map[string]struct{}) float64 {
	if len(ranked) == 0 {
		return 0
	}
	return float64(hitCount(ranked, relevant)) / float64(len(ranked))
}

func recallAtK(ranked []string, relevant map[string]struct{}) float64 {
	if len(relevant) == 0 {
		return 0
	}
	return float64(hitCount(ranked, relevant)) / float64(len(relevant))
}

func ndcgAtK(ranked []string, relevant map[string]struct{}) float64 {
	if len(ranked) == 0 || len(relevant) == 0 {
		return 0
	}
	dcg := 0.0
	for index, value := range ranked {
		if _, exists := relevant[value]; exists {
			dcg += 1 / math.Log2(float64(index+2))
		}
	}
	idealCount := len(relevant)
	if idealCount > len(ranked) {
		idealCount = len(ranked)
	}
	idcg := 0.0
	for index := 0; index < idealCount; index++ {
		idcg += 1 / math.Log2(float64(index+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func hitCount(ranked []string, relevant map[string]struct{}) int {
	hits := 0
	for _, value := range ranked {
		if _, exists := relevant[value]; exists {
			hits++
		}
	}
	return hits
}

func candidateIDs(values []Candidate, limit int) []string {
	if limit > len(values) {
		limit = len(values)
	}
	output := make([]string, 0, limit)
	for _, value := range values[:limit] {
		output = append(output, value.ID)
	}
	return output
}

func average(sum float64, count int) MetricAverage {
	if count <= 0 {
		return MetricAverage{}
	}
	return MetricAverage{Samples: count, Value: sum / float64(count)}
}

func boundedText(value string, minimum, maximum int) bool {
	value = strings.TrimSpace(value)
	return len(value) >= minimum && len(value) <= maximum && !strings.ContainsAny(value, "\x00\r\n")
}

func safeRelativePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, "\\") || strings.Contains(value, "://") {
		return false
	}
	cleaned := filepath.Clean(value)
	return cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

func validUnavailableReason(value string) bool {
	switch value {
	case "no_vectors", "low_confidence", "timeout", "capacity", "error":
		return true
	default:
		return false
	}
}

func candidateIDSet(values []Candidate) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[strings.TrimSpace(value.ID)] = struct{}{}
	}
	return set
}

func setSubset(left, right map[string]struct{}) bool {
	for value := range left {
		if _, exists := right[value]; !exists {
			return false
		}
	}
	return true
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[strings.TrimSpace(value)] = struct{}{}
	}
	return set
}
