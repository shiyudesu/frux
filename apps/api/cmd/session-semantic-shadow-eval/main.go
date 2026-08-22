package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
	shadowevaluation "github.com/shiyudesu/frux/internal/infra/shadowevaluation"
)

func main() {
	input := flag.String("input", "testdata/session-semantic-shadow/golden-v1.json", "versioned local Shadow fixture")
	jsonOutput := flag.String("json-output", "session-semantic-shadow-report.json", "canonical JSON report path")
	markdownOutput := flag.String("markdown-output", "session-semantic-shadow-report.md", "canonical Markdown report path")
	minimumCases := flag.Int("min-cases", 5, "minimum available and labeled cases for a complete report")
	contractKey := flag.String("contract-key", "", "optional active multimodal contract key for rollout evidence")
	profile := flag.String("profile", "", "optional registered multimodal profile for rollout evidence")
	flag.Parse()
	if flag.NArg() != 0 || strings.TrimSpace(*input) == "" || strings.TrimSpace(*jsonOutput) == "" || strings.TrimSpace(*markdownOutput) == "" ||
		(strings.TrimSpace(*contractKey) != "" && strings.TrimSpace(*profile) != "") {
		fatal(shadowevaluation.ErrInvalidFixture)
	}
	fixture, checksum, err := shadowevaluation.Load(*input)
	if err != nil {
		fatal(err)
	}
	report, err := shadowevaluation.Evaluate(fixture, checksum, *minimumCases)
	if err != nil {
		fatal(err)
	}
	resolvedContractKey := strings.TrimSpace(*contractKey)
	if strings.TrimSpace(*profile) != "" {
		selected, err := multimodalprofile.Resolve(*profile)
		if err != nil {
			fatal(err)
		}
		resolvedContractKey = selected.Contract.Key()
	}
	if resolvedContractKey != "" {
		if err := shadowevaluation.BindReportContract(report, resolvedContractKey); err != nil {
			fatal(err)
		}
	}
	if err := shadowevaluation.Write(report, *jsonOutput, *markdownOutput); err != nil {
		fatal(err)
	}
	fmt.Printf(
		"session semantic Shadow evaluation complete: status=%s cases=%d available=%d labeled=%d external_model_calls=%d\n",
		report.Status, report.Cases.Total, report.Cases.Available, report.Cases.Labeled, report.ExternalModelCalls,
	)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "session semantic Shadow evaluation failed: %v\n", err)
	os.Exit(1)
}
