package infraofflineevaluation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	applicationofflineevaluation "github.com/shiyudesu/frux/internal/application/offlineevaluation"
	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
)

var ErrInvalidReportOutput = errors.New("invalid offline evaluation report output")

type PerformanceEvidence struct {
	Available bool                `json:"available"`
	Reason    string              `json:"reason,omitempty"`
	Metrics   []PerformanceMetric `json:"metrics,omitempty"`
}

type PublicReport struct {
	Version            string                                        `json:"version"`
	Kind               string                                        `json:"kind"`
	Track              domainofflineevaluation.Track                 `json:"track"`
	Result             string                                        `json:"result"`
	ExternalModelCalls int                                           `json:"external_model_calls"`
	Manifest           ManifestEvidence                              `json:"manifest"`
	Evaluation         applicationofflineevaluation.PublicEvaluation `json:"evaluation"`
	Performance        PerformanceEvidence                           `json:"performance"`
	Limitations        []string                                      `json:"limitations"`
}

func NewPublicReport(
	manifest ManifestEvidence,
	evaluation *applicationofflineevaluation.PublicEvaluation,
	performance []PerformanceMetric,
) (PublicReport, error) {
	if evaluation == nil || manifest.Dataset == "" || manifest.ManifestSHA256 == "" ||
		string(evaluation.Summary.Dataset) != manifest.Dataset || evaluation.Summary.Release != manifest.Release {
		return PublicReport{}, ErrInvalidReportOutput
	}
	report := PublicReport{
		Version: domainofflineevaluation.ReportVersion,
		Kind:    domainofflineevaluation.ReportKind,
		Track:   domainofflineevaluation.TrackPublicDataset,
		Result:  "success", ExternalModelCalls: domainofflineevaluation.ExternalModelCalls,
		Manifest: manifest, Evaluation: *evaluation,
		Performance: PerformanceEvidence{Available: len(performance) > 0, Metrics: append([]PerformanceMetric(nil), performance...)},
		Limitations: []string{
			"offline results are non-causal and do not establish production lift",
			"dataset user and item namespaces remain isolated and are not combined into one score",
			"public watch labels do not replace blinded Frux Golden Set judgments",
			"the evaluator does not recommend, activate, shadow, or roll out a policy",
		},
	}
	if !report.Performance.Available {
		report.Performance.Reason = "checksum-covered performance evidence not declared"
	}
	return report, nil
}

func RenderPublicReport(report PublicReport) ([]byte, []byte, error) {
	if report.Version != domainofflineevaluation.ReportVersion || report.Track != domainofflineevaluation.TrackPublicDataset ||
		report.ExternalModelCalls != 0 || report.Result != "success" {
		return nil, nil, ErrInvalidReportOutput
	}
	jsonContent, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	jsonContent = append(jsonContent, '\n')
	var markdown bytes.Buffer
	fmt.Fprintf(&markdown, "# Offline Recommendation Evaluation\n\n")
	fmt.Fprintf(&markdown, "- Dataset: `%s`\n", report.Manifest.Dataset)
	fmt.Fprintf(&markdown, "- Release: `%s`\n", report.Manifest.Release)
	fmt.Fprintf(&markdown, "- Schema: `%s`\n", report.Manifest.Schema)
	fmt.Fprintf(&markdown, "- Session profile: `%s`\n", report.Evaluation.Profile.Version)
	fmt.Fprintf(&markdown, "- Cases: %d\n", report.Evaluation.Summary.Cases)
	fmt.Fprintf(&markdown, "- External model calls: %d\n\n", report.ExternalModelCalls)
	markdown.WriteString("## Baselines\n\n")
	markdown.WriteString("| Baseline | Cases | HitRate@1 | NDCG@1 | MRR | Catalog coverage |\n")
	markdown.WriteString("| --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, baseline := range report.Evaluation.Baselines {
		fmt.Fprintf(&markdown, "| `%s` | %d/%d | %s | %s | %s | %s |\n",
			baseline.Baseline, baseline.CasesAvailable, baseline.CasesTotal,
			metricAtK(baseline.TopK, 1, "hit"), metricAtK(baseline.TopK, 1, "ndcg"),
			formatMetric(baseline.MRR), formatMetric(baseline.CatalogCoverage),
		)
	}
	markdown.WriteString("\n## Performance evidence\n\n")
	if report.Performance.Available {
		for _, metric := range report.Performance.Metrics {
			fmt.Fprintf(&markdown, "- `%s`: %.6f %s (%d samples, `%s`)\n", metric.Name, metric.Value, metric.Unit, metric.SampleCount, metric.MachineProfile)
		}
	} else {
		fmt.Fprintf(&markdown, "- Unavailable: %s\n", report.Performance.Reason)
	}
	markdown.WriteString("\n## Limitations\n\n")
	for _, limitation := range report.Limitations {
		fmt.Fprintf(&markdown, "- %s.\n", strings.TrimSuffix(limitation, "."))
	}
	return jsonContent, markdown.Bytes(), nil
}

func PublishPublicReport(jsonPath, markdownPath string, report PublicReport, overwrite bool) error {
	jsonContent, markdownContent, err := RenderPublicReport(report)
	if err != nil {
		return err
	}
	jsonPath, markdownPath, directory, err := validateReportPaths(jsonPath, markdownPath, overwrite)
	if err != nil {
		return err
	}
	jsonTemp, err := writeReportTemp(directory, ".offline-evaluation-json-*", jsonContent)
	if err != nil {
		return err
	}
	defer os.Remove(jsonTemp)
	markdownTemp, err := writeReportTemp(directory, ".offline-evaluation-markdown-*", markdownContent)
	if err != nil {
		return err
	}
	defer os.Remove(markdownTemp)
	jsonBackup, markdownBackup := "", ""
	if overwrite {
		if jsonBackup, err = backupExisting(jsonPath); err != nil {
			return err
		}
		if markdownBackup, err = backupExisting(markdownPath); err != nil {
			restoreBackup(jsonBackup, jsonPath)
			return err
		}
	}
	rollback := func() {
		_ = os.Remove(jsonPath)
		_ = os.Remove(markdownPath)
		restoreBackup(jsonBackup, jsonPath)
		restoreBackup(markdownBackup, markdownPath)
	}
	if err := os.Rename(jsonTemp, jsonPath); err != nil {
		rollback()
		return err
	}
	jsonTemp = ""
	if err := os.Rename(markdownTemp, markdownPath); err != nil {
		rollback()
		return err
	}
	markdownTemp = ""
	directoryHandle, err := os.Open(directory)
	if err != nil || directoryHandle.Sync() != nil || directoryHandle.Close() != nil {
		rollback()
		return ErrInvalidReportOutput
	}
	removeBackup(jsonBackup)
	removeBackup(markdownBackup)
	return nil
}

func metricAtK(values []domainofflineevaluation.TopKMetrics, k int, kind string) string {
	for _, value := range values {
		if value.K != k {
			continue
		}
		if kind == "ndcg" {
			return formatMetric(value.NDCG)
		}
		return formatMetric(value.HitRate)
	}
	return "unavailable"
}

func formatMetric(metric domainofflineevaluation.Metric) string {
	if metric.Availability != domainofflineevaluation.AvailabilityAvailable || metric.Value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.6f", *metric.Value)
}

func validateReportPaths(jsonPath, markdownPath string, overwrite bool) (string, string, string, error) {
	jsonAbsolute, jsonErr := filepath.Abs(strings.TrimSpace(jsonPath))
	markdownAbsolute, markdownErr := filepath.Abs(strings.TrimSpace(markdownPath))
	if jsonErr != nil || markdownErr != nil || jsonAbsolute == markdownAbsolute ||
		!strings.EqualFold(filepath.Ext(jsonAbsolute), ".json") || !strings.EqualFold(filepath.Ext(markdownAbsolute), ".md") ||
		filepath.Dir(jsonAbsolute) != filepath.Dir(markdownAbsolute) {
		return "", "", "", ErrInvalidReportOutput
	}
	directory := filepath.Dir(jsonAbsolute)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", "", ErrInvalidReportOutput
	}
	for _, path := range []string{jsonAbsolute, markdownAbsolute} {
		info, statErr := os.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || !overwrite {
			return "", "", "", ErrInvalidReportOutput
		}
	}
	return jsonAbsolute, markdownAbsolute, directory, nil
}

func writeReportTemp(directory, pattern string, content []byte) (string, error) {
	file, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func backupExisting(path string) (string, error) {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".offline-evaluation-backup-*")
	if err != nil {
		return "", err
	}
	backup := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(backup)
		return "", err
	}
	if err := os.Remove(backup); err != nil {
		return "", err
	}
	if err := os.Rename(path, backup); err != nil {
		return "", err
	}
	return backup, nil
}

func restoreBackup(backup, target string) {
	if backup != "" {
		_ = os.Rename(backup, target)
	}
}

func removeBackup(backup string) {
	if backup != "" {
		_ = os.Remove(backup)
	}
}
