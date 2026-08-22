package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	infraenvfile "github.com/shiyudesu/frux/internal/infra/envfile"
	rollout "github.com/shiyudesu/frux/internal/infra/rollout"
)

type commandOptions struct {
	action     rollout.Action
	execute    bool
	reportPath string
}

type commandExecutor func(context.Context, rollout.Config, bool, string) (rollout.OperatorReport, error)

func main() {
	if err := run(os.Args[1:], os.Stdout, rollout.Run); err != nil {
		fmt.Fprintln(os.Stderr, "session semantic rollout failed")
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer, executor commandExecutor) error {
	options, err := parseOptions(arguments)
	if err != nil {
		return err
	}
	if err := infraenvfile.LoadMultimodal(infraenvfile.MultimodalFruxRuntime); err != nil {
		return err
	}
	if err := infraenvfile.LoadSessionSemanticRollout(); err != nil {
		return err
	}
	config, err := rollout.LoadConfigFromEnv(options.action)
	if err != nil {
		return err
	}
	if executor == nil {
		return errors.New("session semantic rollout executor unavailable")
	}
	report, runErr := executor(
		context.Background(), config, options.execute, os.Getenv(rollout.MutationGate),
	)
	reportPath := strings.TrimSpace(options.reportPath)
	if reportPath == "" {
		reportPath = strings.TrimSpace(os.Getenv("FRUX_SESSION_SEMANTIC_ROLLOUT_REPORT"))
	}
	if err := rollout.WriteReport(output, reportPath, report); err != nil {
		return err
	}
	return runErr
}

func parseOptions(arguments []string) (commandOptions, error) {
	flags := flag.NewFlagSet("session-semantic-rollout", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	action := flags.String("action", string(rollout.ActionPlan), "plan, create, activate, status, or disable")
	execute := flags.Bool("execute", false, "execute a separately acknowledged mutation action")
	report := flags.String("report", "", "optional JSON operator report path override")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return commandOptions{}, errors.New("invalid session semantic rollout command options")
	}
	parsedAction := rollout.Action(strings.ToLower(strings.TrimSpace(*action)))
	if !rollout.ValidAction(parsedAction) || (!rollout.MutatingAction(parsedAction) && *execute) {
		return commandOptions{}, errors.New("invalid session semantic rollout command options")
	}
	return commandOptions{action: parsedAction, execute: *execute, reportPath: strings.TrimSpace(*report)}, nil
}
