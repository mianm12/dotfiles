package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	applyrunner "github.com/mianm12/dotfiles/internal/apply"
	"github.com/mianm12/dotfiles/internal/planner"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/spf13/cobra"
)

const (
	forceFlagName   = "force"
	pruneFlagName   = "prune"
	noPruneFlagName = "no-prune"
	dryRunFlagName  = "dry-run"
	adoptFlagName   = "adopt"
	yesFlagName     = "yes"
)

type readOnlyPlanOptions struct {
	modules       []string
	force         bool
	prune         bool
	noPrune       bool
	pruneSet      bool
	noPruneSet    bool
	home          string
	homeSet       bool
	repository    string
	repositorySet bool
	profile       string
	profileSet    bool
	verbose       bool
}

type applyRun func(applyrunner.Options) (applyrunner.Result, error)

func newApplyCommand(env environment, global *globalOptions) *cobra.Command {
	var dryRun bool
	var force bool
	var adopt bool
	var prune bool
	var noPrune bool
	var yes bool
	command := &cobra.Command{
		Use:   "apply [module...]",
		Short: "Apply the selected dotfiles modules",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, modules []string) error {
			if adopt {
				return errors.New("--adopt requires M2 and is not supported in this build")
			}
			options := readOnlyPlanOptions{
				modules:       append([]string(nil), modules...),
				force:         force,
				prune:         prune,
				noPrune:       noPrune,
				pruneSet:      command.Flags().Changed(pruneFlagName),
				noPruneSet:    command.Flags().Changed(noPruneFlagName),
				home:          global.home,
				homeSet:       command.Flags().Changed(homeFlagName),
				repository:    global.repo,
				repositorySet: command.Flags().Changed(repoFlagName),
				profile:       global.profile,
				profileSet:    command.Flags().Changed(profileFlagName),
				verbose:       global.verbose,
			}
			if err := validatePlanFlags(options); err != nil {
				return err
			}
			if dryRun {
				return runReadOnlyPlan(command, options, env)
			}
			return runMutationApply(command, options, yes, env)
		},
	}
	flags := command.Flags()
	flags.BoolVarP(&dryRun, dryRunFlagName, "n", false, "print the plan without mutation")
	flags.BoolVar(&adopt, adoptFlagName, false, "adopt matching unmanaged rendered files (M2)")
	flags.BoolVarP(&yes, yesFlagName, "y", false, "skip interactive confirmations")
	bindReadOnlyPlanFlags(command, &force, &prune, &noPrune)
	return command
}

func newDiffCommand(env environment, global *globalOptions) *cobra.Command {
	var force bool
	var prune bool
	var noPrune bool
	command := &cobra.Command{
		Use:   "diff [module...]",
		Short: "Show the read-only apply plan",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, modules []string) error {
			return runReadOnlyPlan(command, readOnlyPlanOptions{
				modules:       append([]string(nil), modules...),
				force:         force,
				prune:         prune,
				noPrune:       noPrune,
				pruneSet:      command.Flags().Changed(pruneFlagName),
				noPruneSet:    command.Flags().Changed(noPruneFlagName),
				home:          global.home,
				homeSet:       command.Flags().Changed(homeFlagName),
				repository:    global.repo,
				repositorySet: command.Flags().Changed(repoFlagName),
				profile:       global.profile,
				profileSet:    command.Flags().Changed(profileFlagName),
				verbose:       global.verbose,
			}, env)
		},
	}
	bindReadOnlyPlanFlags(command, &force, &prune, &noPrune)
	return command
}

func bindReadOnlyPlanFlags(command *cobra.Command, force, prune, noPrune *bool) {
	flags := command.Flags()
	flags.BoolVar(force, forceFlagName, false, "plan supported conflict replacements")
	flags.BoolVar(prune, pruneFlagName, true, "include orphan pruning in the plan")
	flags.BoolVar(noPrune, noPruneFlagName, false, "omit orphan pruning from the plan")
}

func runReadOnlyPlan(command *cobra.Command, options readOnlyPlanOptions, env environment) error {
	if err := validatePlanFlags(options); err != nil {
		return err
	}
	plan, err := planner.PlanApply(plannerOptions(options, env))
	if err != nil {
		return err
	}
	projection, err := projectApplyPlan(plan, options.verbose)
	if err != nil {
		return err
	}
	printPlanProjection(command, projection)
	return commandExit(projection.exitCode)
}

func validatePlanFlags(options readOnlyPlanOptions) error {
	if options.pruneSet && options.noPruneSet {
		return errors.New("--prune and --no-prune must not be used together")
	}
	return nil
}

func plannerOptions(options readOnlyPlanOptions, env environment) planner.ApplyOptions {
	return planner.ApplyOptions{
		Runtime: dotruntime.Overrides{
			Home: dotruntime.Override{
				Value: options.home,
				Set:   options.homeSet,
			},
			Repository: dotruntime.Override{
				Value: options.repository,
				Set:   options.repositorySet,
			},
			Profile: dotruntime.Override{
				Value: options.profile,
				Set:   options.profileSet,
			},
		},
		CLIVersion: env.build.Version,
		Modules:    options.modules,
		Force:      options.force,
		NoPrune:    options.noPrune || !options.prune,
	}
}

func runMutationApply(command *cobra.Command, options readOnlyPlanOptions, yes bool, env environment) error {
	confirm := confirmationCallback(command, yes, env.openTerminal)
	runner := env.applyRun
	if runner == nil {
		runner = applyrunner.Run
	}
	result, runErr := runner(applyrunner.Options{
		Runtime:    plannerOptions(options, env).Runtime,
		CLIVersion: env.build.Version,
		Modules:    append([]string(nil), options.modules...),
		Force:      options.force,
		NoPrune:    options.noPrune || !options.prune,
		Confirm:    confirm,
	})
	if result.Plan.Valid() {
		var projection planProjection
		var projectionErr error
		if !result.ActionOutcomesReady && runErr != nil {
			projection, projectionErr = projectApplyPlan(result.Plan, options.verbose)
		} else {
			projection, projectionErr = projectApplyResult(result, options.verbose, runErr != nil)
		}
		if projectionErr != nil {
			return errors.Join(runErr, projectionErr)
		}
		printPlanProjection(command, projection)
		for _, backupPath := range result.BackupPaths {
			command.Println("backup  " + backupPath)
		}
		if runErr == nil {
			return commandExit(projection.exitCode)
		}
	}
	if runErr != nil {
		return runErr
	}
	return fmt.Errorf("%w: runner returned an invalid plan without an error", applyrunner.ErrExecutionProtocol)
}

func confirmationCallback(
	command *cobra.Command,
	yes bool,
	openTerminal func() (io.ReadCloser, error),
) applyrunner.ConfirmPrune {
	if yes {
		return func([]planner.PruneConfirmationGroup) (bool, error) { return true, nil }
	}
	return func(groups []planner.PruneConfirmationGroup) (bool, error) {
		writer := command.ErrOrStderr()
		if _, err := fmt.Fprintln(writer, "Whole-module orphan prune:"); err != nil {
			return false, err
		}
		for _, group := range groups {
			if _, err := fmt.Fprintf(writer, "  %s:\n", group.Module); err != nil {
				return false, err
			}
			for _, target := range group.Targets {
				effect := "remove state only"
				if target.WouldDeleteTarget {
					effect = "delete target"
				}
				if _, err := fmt.Fprintf(writer, "    %s  %s\n", effect, target.Target); err != nil {
					return false, err
				}
			}
		}
		if _, err := fmt.Fprint(writer, "Remove orphaned modules? [y/N] "); err != nil {
			return false, err
		}
		if openTerminal == nil {
			openTerminal = func() (io.ReadCloser, error) { return os.Open("/dev/tty") }
		}
		terminal, err := openTerminal()
		if err != nil {
			_, writeErr := fmt.Fprintln(writer, "\nwarning: no user terminal available; prune deferred")
			return false, writeErr
		}
		answer, readErr := bufio.NewReader(terminal).ReadString('\n')
		closeErr := terminal.Close()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				_, writeErr := fmt.Fprintln(writer, "warning: confirmation input ended; prune deferred")
				return false, errors.Join(writeErr, closeErr)
			}
			return false, errors.Join(readErr, closeErr)
		}
		if closeErr != nil {
			return false, closeErr
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return true, nil
		default:
			return false, nil
		}
	}
}

func projectApplyResult(result applyrunner.Result, verbose, hasRuntimeError bool) (planProjection, error) {
	fileOutcomes, pruneOutcomes, err := validateApplyOutcomes(result, hasRuntimeError)
	if err != nil {
		return planProjection{}, err
	}
	projection, err := projectApplyPlanWithOutcomes(result.Plan, verbose, fileOutcomes, pruneOutcomes)
	if err != nil {
		return planProjection{}, err
	}
	conflict := false
	for _, action := range result.Plan.FileActions() {
		conflict = conflict || action.Verb == planner.FileConflict
	}
	unfinished := false
	pruneIncomplete := false
	for _, outcome := range result.FileOutcomes {
		conflict = conflict || outcome.Status == applyrunner.ActionConflict
		unfinished = unfinished || outcome.Status == applyrunner.ActionDeferred || outcome.Status == applyrunner.ActionFailed
	}
	for _, outcome := range result.PruneOutcomes {
		conflict = conflict || outcome.Status == applyrunner.ActionConflict
		pruneIncomplete = pruneIncomplete || outcome.Status != applyrunner.ActionSucceeded
	}
	unfinished = unfinished || pruneIncomplete
	if pruneIncomplete {
		projection.warnings = append(projection.warnings, "prune was deferred; rerun apply after resolving unfinished work")
	}
	switch {
	case conflict:
		projection.exitCode = exitConflict
	case unfinished || len(projection.warnings) > 0:
		projection.exitCode = exitActionable
	default:
		projection.exitCode = exitOK
	}
	return projection, nil
}

func validateApplyOutcomes(result applyrunner.Result, hasRuntimeError bool) (
	map[int]applyrunner.ActionOutcomeStatus,
	map[int]applyrunner.ActionOutcomeStatus,
	error,
) {
	if !result.ActionOutcomesReady {
		return nil, nil, errors.New("apply runner did not provide action outcomes")
	}
	files := result.Plan.FileActions()
	fileOutcomes := make(map[int]applyrunner.ActionOutcomeStatus, len(result.FileOutcomes))
	conflicts := 0
	failed := false
	for _, outcome := range result.FileOutcomes {
		if outcome.Index < 0 || outcome.Index >= len(files) || files[outcome.Index].Target != outcome.Target ||
			files[outcome.Index].Verb.ExecutionClass() == planner.FilePlanOnly || !validActionOutcome(outcome.Status) {
			return nil, nil, fmt.Errorf("invalid file outcome for index %d target %q", outcome.Index, outcome.Target)
		}
		if _, exists := fileOutcomes[outcome.Index]; exists {
			return nil, nil, fmt.Errorf("duplicate file outcome for index %d", outcome.Index)
		}
		fileOutcomes[outcome.Index] = outcome.Status
		if outcome.Status == applyrunner.ActionConflict {
			conflicts++
		}
		failed = failed || outcome.Status == applyrunner.ActionFailed
	}
	for index, action := range files {
		_, exists := fileOutcomes[index]
		if (action.Verb.ExecutionClass() != planner.FilePlanOnly) != exists {
			return nil, nil, fmt.Errorf("file outcome coverage mismatch for index %d target %q", index, action.Target)
		}
	}

	prune := result.Plan.Prune().Actions()
	if len(result.PruneOutcomes) != len(prune) {
		return nil, nil, fmt.Errorf("prune outcome count is %d, want %d", len(result.PruneOutcomes), len(prune))
	}
	pruneOutcomes := make(map[int]applyrunner.ActionOutcomeStatus, len(result.PruneOutcomes))
	deferred := false
	for _, outcome := range result.PruneOutcomes {
		if outcome.Index < 0 || outcome.Index >= len(prune) || prune[outcome.Index].Target != outcome.Target ||
			!validActionOutcome(outcome.Status) {
			return nil, nil, fmt.Errorf("invalid prune outcome for index %d target %q", outcome.Index, outcome.Target)
		}
		if prune[outcome.Index].Deferred && outcome.Status != applyrunner.ActionDeferred {
			return nil, nil, fmt.Errorf(
				"static deferred prune outcome for index %d target %q is %q, want %q",
				outcome.Index,
				outcome.Target,
				outcome.Status,
				applyrunner.ActionDeferred,
			)
		}
		if _, exists := pruneOutcomes[outcome.Index]; exists {
			return nil, nil, fmt.Errorf("duplicate prune outcome for index %d", outcome.Index)
		}
		pruneOutcomes[outcome.Index] = outcome.Status
		deferred = deferred || outcome.Status != applyrunner.ActionSucceeded
		if outcome.Status == applyrunner.ActionConflict {
			conflicts++
		}
		failed = failed || outcome.Status == applyrunner.ActionFailed
	}
	if failed && !hasRuntimeError {
		return nil, nil, errors.New("failed action outcome requires a runtime error")
	}
	if conflicts != result.UnresolvedConflicts {
		return nil, nil, fmt.Errorf("runtime conflict count is %d, want %d", conflicts, result.UnresolvedConflicts)
	}
	if deferred != result.PruneDeferred {
		return nil, nil, fmt.Errorf("prune deferred summary is %t, want %t from outcomes", result.PruneDeferred, deferred)
	}
	return fileOutcomes, pruneOutcomes, nil
}

func validActionOutcome(status applyrunner.ActionOutcomeStatus) bool {
	switch status {
	case applyrunner.ActionSucceeded, applyrunner.ActionConflict, applyrunner.ActionDeferred, applyrunner.ActionFailed:
		return true
	default:
		return false
	}
}

type planProjection struct {
	contextLine string
	actionLines []string
	warnings    []string
	notices     []string
	upToDate    bool
	exitCode    int
}

func projectApplyPlan(plan planner.ApplyPlan, verbose bool) (planProjection, error) {
	return projectApplyPlanWithOutcomes(plan, verbose, nil, nil)
}

func projectApplyPlanWithOutcomes(
	plan planner.ApplyPlan,
	verbose bool,
	fileOutcomes map[int]applyrunner.ActionOutcomeStatus,
	pruneOutcomes map[int]applyrunner.ActionOutcomeStatus,
) (planProjection, error) {
	if !plan.Valid() {
		return planProjection{}, errors.New("cannot present an invalid apply plan")
	}
	context := plan.Context()
	projection := planProjection{
		contextLine: fmt.Sprintf("repo=%s profile=%s os=%s", context.Repository, context.Profile, context.GOOS),
	}
	if context.DevelopmentBuild {
		projection.notices = append(projection.notices, "development build skipped the requires version comparison")
	}

	actionable := false
	conflict := false
	for index, action := range plan.FileActions() {
		verb, err := filePresentationVerb(action.Verb)
		if err != nil {
			return planProjection{}, err
		}
		outcome := fileOutcomes[index]
		if outcome == applyrunner.ActionConflict {
			verb = "CONFLICT"
		}
		switch {
		case action.Verb == planner.FileConflict || outcome == applyrunner.ActionConflict:
			conflict = true
		case action.Verb == planner.FileSkip:
			if action.Reason == planner.FileReasonScaffoldDeleted {
				actionable = true
				projection.warnings = append(
					projection.warnings,
					fmt.Sprintf("%s: scaffold target was deleted; use --force to rebuild", action.Target),
				)
			}
		default:
			actionable = true
		}
		if action.Verb != planner.FileSkip || verbose {
			projection.actionLines = append(projection.actionLines, planActionLine(verb, action.Target, string(action.Reason)))
		}
	}
	for index, action := range plan.Prune().Actions() {
		verb := "prune"
		outcome := pruneOutcomes[index]
		switch outcome {
		case applyrunner.ActionConflict:
			verb = "CONFLICT"
		case applyrunner.ActionDeferred:
			verb = "prune (deferred)"
		default:
			if action.Deferred {
				verb = "prune (deferred)"
			}
		}
		projection.actionLines = append(projection.actionLines, planActionLine(verb, action.Target, string(action.Reason)))
		actionable = true
		if action.Warning {
			projection.warnings = append(
				projection.warnings,
				fmt.Sprintf("%s: orphan target is no longer owned; only state would be removed", action.Target),
			)
		}
	}
	for _, action := range plan.Hooks().Actions() {
		switch action.Verb {
		case planner.HookRun:
			projection.actionLines = append(
				projection.actionLines,
				planActionLine("run-hook", action.StateKey, "pending-run-once"),
			)
			actionable = true
		case planner.HookSkip:
			if verbose {
				projection.actionLines = append(
					projection.actionLines,
					planActionLine("skip", action.StateKey, "fingerprint-current"),
				)
			}
		default:
			return planProjection{}, fmt.Errorf("unsupported hook presentation verb %q", action.Verb)
		}
	}

	switch {
	case conflict:
		projection.exitCode = exitConflict
	case actionable:
		projection.exitCode = exitActionable
	default:
		projection.exitCode = exitOK
		projection.upToDate = true
	}
	return projection, nil
}

func filePresentationVerb(verb planner.FileVerb) (string, error) {
	switch verb {
	case planner.FileSkip:
		return "skip", nil
	case planner.FileCreateLink:
		return "link", nil
	case planner.FileScaffold:
		return "scaffold", nil
	case planner.FileAdopt:
		return "adopt", nil
	case planner.FileBackupReplace:
		return "backup+replace", nil
	case planner.FileConflict:
		return "CONFLICT", nil
	default:
		return "", fmt.Errorf("unsupported file presentation verb %q", verb)
	}
}

func planActionLine(verb, target, reason string) string {
	return fmt.Sprintf("%s  %s  (%s)", verb, target, reason)
}

func printPlanProjection(command *cobra.Command, projection planProjection) {
	command.Println(projection.contextLine)
	for _, line := range projection.actionLines {
		command.Println(line)
	}
	if projection.upToDate {
		command.Println("Already up to date.")
	}
	for _, warning := range projection.warnings {
		command.PrintErrf("warning: %s\n", warning)
	}
	for _, notice := range projection.notices {
		command.PrintErrf("notice: %s\n", notice)
	}
}
