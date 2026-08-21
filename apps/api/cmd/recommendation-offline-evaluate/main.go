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

	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
	infraofflineevaluation "github.com/shiyudesu/frux/internal/infra/offlineevaluation"
)

const (
	modeValidation = "validation"
	modeEvaluation = "evaluation"
)

type commandOptions struct {
	track           domainofflineevaluation.Track
	evaluate        bool
	root            string
	manifest        string
	input           string
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
}

type publicEvaluationExecutor func(
	context.Context,
	commandOptions,
	*infraofflineevaluation.LoadedManifest,
	validationReport,
) (validationReport, error)

func main() {
	if err := run(os.Args[1:], os.Stdout, nil); err != nil {
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
	case domainofflineevaluation.TrackReplay, domainofflineevaluation.TrackGolden:
		if err := validateBoundedInput(options.input); err != nil {
			return err
		}
	default:
		return errors.New("invalid evaluation track")
	}
	if options.evaluate {
		if options.track != domainofflineevaluation.TrackPublicDataset || executor == nil {
			return errors.New("evaluation executor unavailable")
		}
		report.Mode = modeEvaluation
		report, err = executor(context.Background(), options, loaded, report)
		if err != nil {
			return err
		}
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = output.Write(encoded)
	return err
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
	var err error
	if options.kValues, err = parseK(kRaw); err != nil || options.maxCases < 1 || options.maxCases > 1_000_000 ||
		options.maxItems < 1 || options.maxItems > 10_000_000 || options.maxInteractions < 1 || options.maxInteractions > 100_000_000 {
		return commandOptions{}, errors.New("invalid evaluation bounds")
	}
	if track == domainofflineevaluation.TrackPublicDataset {
		if strings.TrimSpace(options.root) == "" || !safeRelativeOption(options.manifest) {
			return commandOptions{}, errors.New("invalid public dataset options")
		}
	} else if strings.TrimSpace(options.input) == "" {
		return commandOptions{}, errors.New("input bundle is required")
	}
	if options.evaluate {
		if !validOutputPair(options.outputJSON, options.outputMarkdown) {
			return commandOptions{}, errors.New("invalid report outputs")
		}
	}
	return options, nil
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

func validateBoundedInput(input string) error {
	info, err := os.Stat(filepath.Clean(strings.TrimSpace(input)))
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<20 {
		return errors.New("invalid evaluation input")
	}
	return nil
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
