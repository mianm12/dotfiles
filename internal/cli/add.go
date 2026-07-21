package cli

import (
	"errors"
	"fmt"

	addrunner "github.com/mianm12/dotfiles/internal/add"
	"github.com/mianm12/dotfiles/internal/manifest"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/spf13/cobra"
)

const (
	moduleFlagName   = "module"
	templateFlagName = "template"
	scaffoldFlagName = "scaffold"
)

type addOptions struct {
	request       addrunner.Request
	dryRun        bool
	home          string
	homeSet       bool
	repository    string
	repositorySet bool
	profile       string
	profileSet    bool
}

type addProjection struct {
	contextLine string
	actions     []string
	warnings    []string
	notices     []string
	showGitHint bool
	exitCode    int
}

func newAddCommand(env environment, global *globalOptions) *cobra.Command {
	var module string
	var template bool
	var scaffold bool
	var dryRun bool
	command := &cobra.Command{
		Use:   "add [-m module] [--template|--scaffold] [--dry-run] path...",
		Short: "Add existing files to the dotfiles repository",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, paths []string) error {
			if command.Flags().Changed(moduleFlagName) && module == "" {
				return errors.New("--module must not be empty")
			}
			if template && scaffold {
				return errors.New("--template and --scaffold must not be used together")
			}
			mode := addrunner.ModeLink
			switch {
			case template:
				// M1 必须在任何 runtime IO 或 mutation lock 前硬拒绝。
				return addrunner.ErrTemplateUnsupported
			case scaffold:
				mode = addrunner.ModeScaffold
			}
			options := addOptions{
				request: addrunner.Request{
					Paths: append([]string(nil), paths...), Module: module, Mode: mode,
				},
				dryRun:        dryRun,
				home:          global.home,
				homeSet:       command.Flags().Changed(homeFlagName),
				repository:    global.repo,
				repositorySet: command.Flags().Changed(repoFlagName),
				profile:       global.profile,
				profileSet:    command.Flags().Changed(profileFlagName),
			}
			return runAdd(command, options, env)
		},
	}
	flags := command.Flags()
	flags.StringVarP(&module, moduleFlagName, "m", "", "select the destination module")
	flags.BoolVar(&template, templateFlagName, false, "add a managed template (M2)")
	flags.BoolVar(&scaffold, scaffoldFlagName, false, "add a one-time scaffold template")
	flags.BoolVarP(&dryRun, dryRunFlagName, "n", false, "print the add plan without mutation")
	return command
}

func runAdd(command *cobra.Command, options addOptions, env environment) error {
	overrides := dotruntime.Overrides{
		Home:       dotruntime.Override{Value: options.home, Set: options.homeSet},
		Repository: dotruntime.Override{Value: options.repository, Set: options.repositorySet},
		Profile:    dotruntime.Override{Value: options.profile, Set: options.profileSet},
	}
	if options.dryRun {
		load := env.addLoad
		if load == nil {
			load = dotruntime.LoadReadOnly
		}
		inputs, err := load(overrides, env.build.Version)
		if err != nil {
			return classifyAddError(command, err)
		}
		preflight := env.addPreflight
		if preflight == nil {
			preflight = addrunner.Preflight
		}
		plan, err := preflight(inputs, options.request)
		if err != nil {
			return classifyAddError(command, err)
		}
		projection, err := projectAddPlan(plan, true)
		if err != nil {
			return err
		}
		printAddProjection(command, projection)
		return commandExit(projection.exitCode)
	}

	runner := env.addRun
	if runner == nil {
		runner = addrunner.Run
	}
	result, runErr := runner(addrunner.RunOptions{
		Runtime: overrides, CLIVersion: env.build.Version, Request: options.request,
	})
	if !result.Valid() {
		if runErr != nil {
			return classifyAddError(command, runErr)
		}
		return fmt.Errorf("%w: runner returned an invalid result without an error", addrunner.ErrExecutionProtocol)
	}
	if runErr == nil && addExecutionIncomplete(result.StateCommitted(), result.Outcomes()) {
		return fmt.Errorf("%w: runner returned incomplete outcomes without an error", addrunner.ErrExecutionProtocol)
	}
	projection, err := projectAddResult(result)
	if err != nil {
		return errors.Join(runErr, err)
	}
	printAddProjection(command, projection)
	if runErr != nil {
		return classifyAddError(command, runErr)
	}
	return commandExit(projection.exitCode)
}

func addExecutionIncomplete(stateCommitted bool, outcomes []addrunner.ItemOutcome) bool {
	if !stateCommitted {
		return true
	}
	for _, outcome := range outcomes {
		if outcome.Status != addrunner.OutcomeSucceeded {
			return true
		}
	}
	return false
}

func classifyAddError(command *cobra.Command, err error) error {
	if !errors.Is(err, addrunner.ErrModuleAmbiguous) && !errors.Is(err, addrunner.ErrModuleActivation) {
		return err
	}
	command.PrintErrf("conflict: %v\n", err)
	return commandExit(exitConflict)
}

func projectAddPlan(plan addrunner.BatchPlan, dryRun bool) (addProjection, error) {
	items := plan.Items()
	if !plan.Valid() || len(items) == 0 {
		return addProjection{}, fmt.Errorf("%w: add projection received an invalid plan", addrunner.ErrExecutionProtocol)
	}
	reason := "added"
	exitCode := exitOK
	if dryRun {
		reason = "add dry-run"
		exitCode = exitActionable
	}
	projection := addProjection{
		contextLine: fmt.Sprintf("repo=%s profile=%s os=%s", plan.Repository(), plan.Profile(), plan.GOOS()),
		actions:     make([]string, 0, len(items)),
		exitCode:    exitCode,
	}
	if plan.DevelopmentBuild() {
		projection.notices = append(projection.notices, "development build skipped the requires version comparison")
	}
	for _, item := range items {
		verb, err := addVerb(item.Kind())
		if err != nil {
			return addProjection{}, err
		}
		projection.actions = append(projection.actions, fmt.Sprintf("%s  %s  (%s)", verb, item.Target(), reason))
	}
	return projection, nil
}

func addRecoveryWarnings(targetCommits int, stateCommitted bool) []string {
	if targetCommits == 0 || stateCommitted {
		return nil
	}
	return []string{"add committed link targets but state was not stored; rerun dot apply to recover state"}
}

func projectAddResult(result addrunner.Result) (addProjection, error) {
	if !result.Valid() {
		return addProjection{}, fmt.Errorf("%w: add projection received an invalid result", addrunner.ErrExecutionProtocol)
	}
	plan := result.Plan()
	items := plan.Items()
	outcomes := result.Outcomes()
	projection := addProjection{
		contextLine: fmt.Sprintf("repo=%s profile=%s os=%s", plan.Repository(), plan.Profile(), plan.GOOS()),
		actions:     make([]string, 0, len(items)),
		exitCode:    exitOK,
	}
	if plan.DevelopmentBuild() {
		projection.notices = append(projection.notices, "development build skipped the requires version comparison")
	}
	projection.warnings = append(
		projection.warnings,
		addRecoveryWarnings(result.TargetCommits(), result.StateCommitted())...,
	)
	for index, item := range items {
		verb, err := addVerb(item.Kind())
		if err != nil {
			return addProjection{}, err
		}
		reason := "added"
		switch outcomes[index].Status {
		case addrunner.OutcomeSucceeded:
			projection.showGitHint = true
		case addrunner.OutcomeFailed:
			verb = "skip"
			reason = "add failed"
		case addrunner.OutcomeDeferred:
			verb = "skip"
			reason = "not attempted"
		default:
			return addProjection{}, fmt.Errorf("%w: unsupported add outcome %q", addrunner.ErrExecutionProtocol, outcomes[index].Status)
		}
		projection.actions = append(projection.actions, fmt.Sprintf("%s  %s  (%s)", verb, item.Target(), reason))
	}
	return projection, nil
}

func addVerb(kind manifest.FileKind) (string, error) {
	switch kind {
	case manifest.FileKindLink:
		return "link", nil
	case manifest.FileKindScaffold:
		return "scaffold", nil
	default:
		return "", fmt.Errorf("%w: unsupported add plan kind %q", addrunner.ErrExecutionProtocol, kind)
	}
}

func printAddProjection(command *cobra.Command, projection addProjection) {
	command.Println(projection.contextLine)
	for _, action := range projection.actions {
		command.Println(action)
	}
	if projection.showGitHint {
		command.Println("Next: run git add for the published source paths, then git commit.")
	}
	for _, warning := range projection.warnings {
		command.PrintErrf("warning: %s\n", warning)
	}
	for _, notice := range projection.notices {
		command.PrintErrf("notice: %s\n", notice)
	}
}
