package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
	infraacceptance "github.com/shiyudesu/frux/internal/infra/acceptance"
	infraenvfile "github.com/shiyudesu/frux/internal/infra/envfile"
)

var errExecutionNotImplemented = errors.New("acceptance execution workflow is not implemented")

type commandOptions struct {
	execute    bool
	cleanup    bool
	reportPath string
	query      string
	timeout    time.Duration
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "multimodal acceptance failed")
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	options, err := parseOptions(arguments)
	if err != nil {
		return err
	}
	startedAt := time.Now().UTC()
	runID, err := newRunID()
	if err != nil {
		return err
	}
	decision := applicationacceptance.DecideExecution(
		options.execute,
		os.Getenv(applicationacceptance.BillableAcknowledgementEnvironment),
	)
	report := applicationacceptance.NewReport(runID, decision.Mode, startedAt, options.cleanup)
	if err := infraenvfile.LoadMultimodal(infraenvfile.MultimodalFruxRuntime); err != nil {
		failReport(&report, applicationacceptance.FailureConfiguration)
		return emitReport(output, options.reportPath, report)
	}
	_, configErr := infraacceptance.LoadConfigFromEnv(options.query, options.timeout)
	if configErr != nil {
		failReport(&report, applicationacceptance.FailureConfiguration)
		report.Prerequisites = []applicationacceptance.PrerequisiteResult{{
			Name: "configuration", Result: applicationacceptance.ResultFailed,
		}}
		if emitErr := emitReport(output, options.reportPath, report); emitErr != nil {
			return emitErr
		}
		return configErr
	}
	report.Prerequisites = []applicationacceptance.PrerequisiteResult{{
		Name: "configuration", Result: applicationacceptance.ResultSuccess,
	}}
	if !decision.Confirmed {
		report.Result = applicationacceptance.ResultSuccess
		report.FinishedAt = time.Now().UTC()
		for index := range report.Stages {
			if report.Stages[index].Name == applicationacceptance.StagePreflight {
				report.Stages[index].Result = applicationacceptance.ResultSuccess
			} else {
				report.Stages[index].Result = applicationacceptance.ResultSkipped
			}
		}
		return emitReport(output, options.reportPath, report)
	}
	failReport(&report, applicationacceptance.FailureInternal)
	if err := emitReport(output, options.reportPath, report); err != nil {
		return err
	}
	return errExecutionNotImplemented
}

func parseOptions(arguments []string) (commandOptions, error) {
	flags := flag.NewFlagSet("multimodal-acceptance", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options commandOptions
	flags.BoolVar(&options.execute, "execute", false, "execute the explicitly confirmed billable workflow")
	flags.BoolVar(&options.cleanup, "cleanup", false, "delete only videos created by this run after verification")
	flags.StringVar(&options.reportPath, "report", "", "optional JSON report path")
	flags.StringVar(&options.query, "query", "", "hybrid acceptance query")
	flags.DurationVar(&options.timeout, "timeout", 0, "per-stage timeout override")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return commandOptions{}, errors.New("invalid acceptance command options")
	}
	return options, nil
}

func newRunID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "acceptance-" + hex.EncodeToString(value[:]), nil
}

func failReport(report *applicationacceptance.Report, code applicationacceptance.FailureCode) {
	if report == nil {
		return
	}
	report.Result = applicationacceptance.ResultFailed
	report.Failure = code
	report.FinishedAt = time.Now().UTC()
	for index := range report.Stages {
		if report.Stages[index].Result == applicationacceptance.ResultPlanned {
			report.Stages[index].Result = applicationacceptance.ResultSkipped
		}
	}
}

func emitReport(output io.Writer, reportPath string, report applicationacceptance.Report) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if _, err := output.Write(encoded); err != nil {
		return err
	}
	if reportPath == "" {
		return nil
	}
	return writeReport(reportPath, encoded)
}

func writeReport(path string, content []byte) error {
	path = filepath.Clean(path)
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".multimodal-acceptance-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
