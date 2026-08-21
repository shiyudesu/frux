package main

import (
	"context"
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

type commandOptions struct {
	execute    bool
	cleanup    bool
	reportPath string
	timeout    time.Duration
}

type sessionCommandExecutor func(
	context.Context,
	applicationacceptance.SessionSemanticConfig,
	applicationacceptance.SessionSemanticReport,
) (applicationacceptance.SessionSemanticReport, error)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "session semantic acceptance failed")
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	return runWithExecutor(arguments, output, executeSessionCommand)
}

func runWithExecutor(arguments []string, output io.Writer, executor sessionCommandExecutor) error {
	options, err := parseOptions(arguments)
	if err != nil {
		return err
	}
	startedAt := time.Now().UTC()
	runID, err := newRunID()
	if err != nil {
		return err
	}
	decision, environmentErr := loadEnvironmentAndDecide(options.execute)
	report := applicationacceptance.NewSessionSemanticReport(runID, decision.Mode, startedAt, options.cleanup)
	if environmentErr != nil {
		failReport(&report, applicationacceptance.SessionFailureConfiguration)
		return emitReport(output, options.reportPath, report)
	}
	config, configErr := infraacceptance.LoadSessionSemanticConfigFromEnv(options.timeout)
	if configErr != nil {
		failReport(&report, applicationacceptance.SessionFailureConfiguration)
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
	if executor == nil {
		return errors.New("session acceptance executor unavailable")
	}
	report, runErr := executor(context.Background(), config, report)
	if emitErr := emitReport(output, options.reportPath, report); emitErr != nil {
		return emitErr
	}
	return runErr
}

func executeSessionCommand(
	ctx context.Context,
	config applicationacceptance.SessionSemanticConfig,
	report applicationacceptance.SessionSemanticReport,
) (applicationacceptance.SessionSemanticReport, error) {
	httpClient, err := infraacceptance.NewHTTPClient(config.HTTPTimeout, config.MaxResponseBytes, nil)
	if err != nil {
		return report, err
	}
	apiClient, err := infraacceptance.NewAPIClient(httpClient, config.APIEndpoint)
	if err != nil {
		return report, err
	}
	store, err := infraacceptance.NewSessionStore(config.PostgresDSN)
	if err != nil {
		return report, err
	}
	defer store.Close()
	runner, err := infraacceptance.NewSessionRunner(config, httpClient, apiClient, store)
	if err != nil {
		return report, err
	}
	return runner.Run(ctx, report)
}

func loadEnvironmentAndDecide(execute bool) (applicationacceptance.ExecutionDecision, error) {
	if err := infraenvfile.LoadMultimodal(infraenvfile.MultimodalFruxRuntime); err != nil {
		return applicationacceptance.DecideSessionSemanticMutation(
			execute, os.Getenv(applicationacceptance.SessionSemanticMutationGate),
		), err
	}
	if err := infraenvfile.LoadSessionSemanticAcceptance(); err != nil {
		return applicationacceptance.DecideSessionSemanticMutation(
			execute, os.Getenv(applicationacceptance.SessionSemanticMutationGate),
		), err
	}
	return applicationacceptance.DecideSessionSemanticMutation(
		execute, os.Getenv(applicationacceptance.SessionSemanticMutationGate),
	), nil
}

func parseOptions(arguments []string) (commandOptions, error) {
	flags := flag.NewFlagSet("session-semantic-acceptance", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options commandOptions
	flags.BoolVar(&options.execute, "execute", false, "execute the explicitly confirmed mutation workflow")
	flags.BoolVar(&options.cleanup, "cleanup", false, "revert run favorite and delete only the disabled runner policy")
	flags.StringVar(&options.reportPath, "report", "", "optional JSON report path")
	flags.DurationVar(&options.timeout, "timeout", 0, "per-stage timeout override")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return commandOptions{}, errors.New("invalid session acceptance command options")
	}
	return options, nil
}

func newRunID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "session-acceptance-" + hex.EncodeToString(value[:]), nil
}

func failReport(
	report *applicationacceptance.SessionSemanticReport,
	code applicationacceptance.SessionSemanticFailureCode,
) {
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

func emitReport(
	output io.Writer,
	reportPath string,
	report applicationacceptance.SessionSemanticReport,
) error {
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
	temporary, err := os.CreateTemp(directory, ".session-semantic-acceptance-*.tmp")
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
