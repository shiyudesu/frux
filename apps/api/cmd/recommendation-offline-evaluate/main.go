package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	applicationofflineevaluation "github.com/shiyudesu/frux/internal/application/offlineevaluation"
	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
	infraofflineevaluation "github.com/shiyudesu/frux/internal/infra/offlineevaluation"
)

const (
	modeValidation = "validation"
)

type commandOptions struct {
	track           domainofflineevaluation.Track
	evaluate        bool
	root            string
	manifest        string
	input           string
	baseline        string
	candidates      []string
	diagnosticOnly  bool
	outputJSON      string
	outputMarkdown  string
	kValues         []int
	maxCases        int
	maxItems        int
	maxInteractions int64
	overwrite       bool
}

type validationReport struct {
	Version            string                                   `json:"version"`
	Kind               string                                   `json:"kind"`
	Track              domainofflineevaluation.Track            `json:"track"`
	Mode               string                                   `json:"mode"`
	Result             string                                   `json:"result"`
	ExternalModelCalls int                                      `json:"external_model_calls"`
	K                  []int                                    `json:"k,omitempty"`
	MaxCases           int                                      `json:"max_cases,omitempty"`
	MaxItems           int                                      `json:"max_items,omitempty"`
	MaxInteractions    int64                                    `json:"max_interactions,omitempty"`
	Baselines          []domainofflineevaluation.Baseline       `json:"baselines,omitempty"`
	Manifest           *infraofflineevaluation.ManifestEvidence `json:"manifest,omitempty"`
	InputSHA256        string                                   `json:"input_sha256,omitempty"`
	Policies           []infraofflineevaluation.PolicyEvidence  `json:"policies,omitempty"`
}

type publicEvaluationExecutor func(
	context.Context,
	commandOptions,
	*infraofflineevaluation.LoadedManifest,
) (infraofflineevaluation.PublicReport, error)

func main() {
	if err := run(os.Args[1:], os.Stdout, executePublicEvaluation); err != nil {
		fmt.Fprintln(os.Stderr, "offline recommendation evaluation failed")
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer, executor publicEvaluationExecutor) error {
	options, err := parseOptions(arguments)
	if err != nil {
		return err
	}
	report := validationReport{
		Version: domainofflineevaluation.ReportVersion,
		Kind:    domainofflineevaluation.ReportKind,
		Track:   options.track, Mode: modeValidation, Result: "validated",
		ExternalModelCalls: domainofflineevaluation.ExternalModelCalls,
	}
	var loaded *infraofflineevaluation.LoadedManifest
	var replayBundle *infraofflineevaluation.LoadedReplayBundle
	var baseline applicationofflineevaluation.NamedPolicy
	var candidates []applicationofflineevaluation.NamedPolicy
	var goldenBundle *infraofflineevaluation.LoadedGoldenBundle
	switch options.track {
	case domainofflineevaluation.TrackPublicDataset:
		limits := infraofflineevaluation.DefaultManifestLimits()
		limits.MaxRows = options.maxInteractions
		loaded, err = infraofflineevaluation.LoadManifest(options.root, options.manifest, limits)
		if err != nil {
			return err
		}
		evidence := loaded.Evidence
		report.Manifest = &evidence
		report.K = append([]int(nil), options.kValues...)
		report.MaxCases = options.maxCases
		report.MaxItems = options.maxItems
		report.MaxInteractions = options.maxInteractions
		report.Baselines = domainofflineevaluation.Baselines()
	case domainofflineevaluation.TrackReplay:
		replayBundle, err = infraofflineevaluation.LoadReplayBundle(options.input)
		if err != nil {
			return err
		}
		baseline, err = infraofflineevaluation.LoadNamedPolicy(options.baseline)
		if err != nil {
			return err
		}
		for _, path := range options.candidates {
			candidate, loadErr := infraofflineevaluation.LoadNamedPolicy(path)
			if loadErr != nil {
				return loadErr
			}
			candidates = append(candidates, candidate)
		}
		report.InputSHA256 = replayBundle.SHA256
		report.K = append([]int(nil), options.kValues...)
		report.Policies = append(report.Policies, infraofflineevaluation.PolicyEvidence{
			Name: baseline.Name, InputSHA256: baseline.InputSHA256, NormalizedSHA256: baseline.NormalizedSHA256,
		})
		for _, candidate := range candidates {
			report.Policies = append(report.Policies, infraofflineevaluation.PolicyEvidence{
				Name: candidate.Name, InputSHA256: candidate.InputSHA256, NormalizedSHA256: candidate.NormalizedSHA256,
			})
		}
	case domainofflineevaluation.TrackGolden:
		goldenBundle, err = infraofflineevaluation.LoadGoldenBundle(options.input)
		if err != nil {
			return err
		}
		report.InputSHA256 = goldenBundle.SHA256
		report.K = append([]int(nil), options.kValues...)
	default:
		return errors.New("invalid evaluation track")
	}
	var payload any = report
	if options.evaluate {
		switch options.track {
		case domainofflineevaluation.TrackPublicDataset:
			if executor == nil {
				return errors.New("evaluation executor unavailable")
			}
			publicReport, executeErr := executor(context.Background(), options, loaded)
			if executeErr != nil {
				return executeErr
			}
			payload = publicReport
		case domainofflineevaluation.TrackReplay:
			replayReport, executeErr := executeReplayEvaluation(options, replayBundle, baseline, candidates)
			if executeErr != nil {
				return executeErr
			}
			payload = replayReport
		case domainofflineevaluation.TrackGolden:
			goldenReport, executeErr := executeGoldenEvaluation(options, goldenBundle)
			if executeErr != nil {
				return executeErr
			}
			payload = goldenReport
		}
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = output.Write(encoded)
	return err
}

func executeReplayEvaluation(
	options commandOptions,
	bundle *infraofflineevaluation.LoadedReplayBundle,
	baseline applicationofflineevaluation.NamedPolicy,
	candidates []applicationofflineevaluation.NamedPolicy,
) (infraofflineevaluation.ReplayReport, error) {
	if bundle == nil {
		return infraofflineevaluation.ReplayReport{}, errors.New("replay bundle unavailable")
	}
	evaluation, err := applicationofflineevaluation.EvaluateReplay(
		bundle.Bundle, baseline, candidates, options.kValues, options.diagnosticOnly,
	)
	if err != nil {
		return infraofflineevaluation.ReplayReport{}, err
	}
	report, err := infraofflineevaluation.NewReplayReport(bundle.SHA256, baseline, candidates, evaluation)
	if err != nil {
		return infraofflineevaluation.ReplayReport{}, err
	}
	if err := infraofflineevaluation.PublishReplayReport(options.outputJSON, options.outputMarkdown, report, options.overwrite); err != nil {
		return infraofflineevaluation.ReplayReport{}, err
	}
	return report, nil
}

func executeGoldenEvaluation(
	options commandOptions,
	bundle *infraofflineevaluation.LoadedGoldenBundle,
) (infraofflineevaluation.GoldenReport, error) {
	if bundle == nil {
		return infraofflineevaluation.GoldenReport{}, errors.New("Golden bundle unavailable")
	}
	evaluation, err := applicationofflineevaluation.EvaluateGolden(bundle.Bundle, options.kValues)
	if err != nil {
		return infraofflineevaluation.GoldenReport{}, err
	}
	report, err := infraofflineevaluation.NewGoldenReport(bundle.SHA256, evaluation)
	if err != nil {
		return infraofflineevaluation.GoldenReport{}, err
	}
	if err := infraofflineevaluation.PublishGoldenReport(options.outputJSON, options.outputMarkdown, report, options.overwrite); err != nil {
		return infraofflineevaluation.GoldenReport{}, err
	}
	return report, nil
}

func executePublicEvaluation(
	ctx context.Context,
	options commandOptions,
	loaded *infraofflineevaluation.LoadedManifest,
) (infraofflineevaluation.PublicReport, error) {
	limits := infraofflineevaluation.DefaultDatasetLimits()
	limits.MaxInteractions = options.maxInteractions
	limits.MaxItems = options.maxItems
	dataset, err := infraofflineevaluation.LoadDataset(loaded, limits)
	if err != nil {
		return infraofflineevaluation.PublicReport{}, err
	}
	evaluation, err := applicationofflineevaluation.EvaluatePublicDataset(
		dataset, domainofflineevaluation.DefaultCaseProfile(), options.kValues, options.maxCases,
	)
	if err != nil {
		return infraofflineevaluation.PublicReport{}, err
	}
	performance, err := infraofflineevaluation.LoadPerformanceEvidence(loaded)
	if err != nil {
		return infraofflineevaluation.PublicReport{}, err
	}
	report, err := infraofflineevaluation.NewPublicReport(loaded.Evidence, evaluation, performance)
	if err != nil {
		return infraofflineevaluation.PublicReport{}, err
	}
	if err := infraofflineevaluation.PublishPublicReport(
		options.outputJSON, options.outputMarkdown, report, options.overwrite,
	); err != nil {
		return infraofflineevaluation.PublicReport{}, err
	}
	return report, nil
}

func parseOptions(arguments []string) (commandOptions, error) {
	if len(arguments) == 0 {
		return commandOptions{}, errors.New("evaluation track is required")
	}
	track, ok := parseTrack(arguments[0])
	if !ok {
		return commandOptions{}, errors.New("invalid evaluation track")
	}
	flags := flag.NewFlagSet(arguments[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	options := commandOptions{track: track, kValues: []int{1, 5, 10, 20}, maxCases: 10_000, maxItems: 1_000_000, maxInteractions: 100_000_000}
	flags.BoolVar(&options.evaluate, "evaluate", false, "execute the selected offline evaluation")
	flags.StringVar(&options.root, "root", "", "operator-owned dataset root")
	flags.StringVar(&options.manifest, "manifest", "manifest.json", "dataset-root-relative manifest")
	flags.StringVar(&options.input, "input", "", "replay or Golden input bundle")
	flags.StringVar(&options.baseline, "baseline", "", "baseline policy file for replay")
	var candidatePaths stringList
	flags.Var(&candidatePaths, "candidate", "candidate policy file for replay (repeatable)")
	flags.BoolVar(&options.diagnosticOnly, "diagnostic-only", false, "list non-replayable differences without comparative metrics")
	flags.StringVar(&options.outputJSON, "output-json", "", "canonical JSON report path")
	flags.StringVar(&options.outputMarkdown, "output-markdown", "", "canonical Markdown report path")
	var kRaw string
	flags.StringVar(&kRaw, "k", "1,5,10,20", "sorted unique K values")
	flags.IntVar(&options.maxCases, "max-cases", options.maxCases, "maximum evaluation cases")
	flags.IntVar(&options.maxItems, "max-items", options.maxItems, "maximum dataset items")
	flags.Int64Var(&options.maxInteractions, "max-interactions", options.maxInteractions, "maximum interaction rows")
	flags.BoolVar(&options.overwrite, "overwrite", false, "replace existing reports atomically")
	if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 {
		return commandOptions{}, errors.New("invalid evaluation options")
	}
	options.candidates = append([]string(nil), candidatePaths...)
	var err error
	if options.kValues, err = parseK(kRaw); err != nil || options.maxCases < 1 || options.maxCases > 1_000_000 ||
		options.maxItems < 1 || options.maxItems > 10_000_000 || options.maxInteractions < 1 || options.maxInteractions > 100_000_000 {
		return commandOptions{}, errors.New("invalid evaluation bounds")
	}
	if track == domainofflineevaluation.TrackPublicDataset {
		if strings.TrimSpace(options.root) == "" || !safeRelativeOption(options.manifest) {
			return commandOptions{}, errors.New("invalid public dataset options")
		}
	} else if track == domainofflineevaluation.TrackReplay {
		if strings.TrimSpace(options.input) == "" || strings.TrimSpace(options.baseline) == "" || len(options.candidates) == 0 || len(options.candidates) > 20 {
			return commandOptions{}, errors.New("replay inputs are required")
		}
	} else if strings.TrimSpace(options.input) == "" {
		return commandOptions{}, errors.New("Golden input bundle is required")
	}
	if options.evaluate {
		if !validOutputPair(options.outputJSON, options.outputMarkdown) {
			return commandOptions{}, errors.New("invalid report outputs")
		}
	}
	return options, nil
}

type stringList []string

func (v *stringList) String() string {
	return strings.Join(*v, ",")
}

func (v *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("empty path")
	}
	*v = append(*v, value)
	return nil
}

func parseTrack(value string) (domainofflineevaluation.Track, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "public-dataset":
		return domainofflineevaluation.TrackPublicDataset, true
	case "replay":
		return domainofflineevaluation.TrackReplay, true
	case "golden":
		return domainofflineevaluation.TrackGolden, true
	default:
		return "", false
	}
}

func parseK(value string) ([]int, error) {
	parts := strings.Split(value, ",")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		parsed, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		values = append(values, parsed)
	}
	sort.Ints(values)
	if !domainofflineevaluation.ValidK(values) {
		return nil, errors.New("invalid K values")
	}
	return values, nil
}

func validOutputPair(jsonPath, markdownPath string) bool {
	jsonPath = strings.TrimSpace(jsonPath)
	markdownPath = strings.TrimSpace(markdownPath)
	if jsonPath == "" || markdownPath == "" {
		return false
	}
	jsonAbsolute, jsonErr := filepath.Abs(jsonPath)
	markdownAbsolute, markdownErr := filepath.Abs(markdownPath)
	return jsonErr == nil && markdownErr == nil && jsonAbsolute != markdownAbsolute &&
		strings.EqualFold(filepath.Ext(jsonPath), ".json") && strings.EqualFold(filepath.Ext(markdownPath), ".md")
}

func safeRelativeOption(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !filepath.IsAbs(value) && filepath.Clean(value) == value &&
		value != "." && value != ".." && !strings.HasPrefix(value, ".."+string(filepath.Separator))
}
