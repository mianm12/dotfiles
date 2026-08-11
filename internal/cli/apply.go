package cli

import (
	"fmt"

	"github.com/mianm12/dotfiles/internal/core/mutation"
	"github.com/spf13/cobra"
)

func newApplyCommand(env environment) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "apply [MODULE]",
		Short: "Converge the current effective selection",
		Args:  maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			var moduleID *string
			if len(args) == 1 {
				moduleID = &args[0]
			}
			return runApply(command, moduleID, dryRun, env)
		},
	}
	command.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "print the plan without mutation")
	return command
}

func runApply(
	command *cobra.Command,
	moduleID *string,
	dryRun bool,
	env environment,
) error {
	context, err := resolveContext(env)
	if err != nil {
		return err
	}
	rerun := "dot apply"
	if moduleID != nil {
		rerun = fmt.Sprintf("dot apply %s", *moduleID)
	}
	if dryRun {
		machine, err := loadRequiredMachine(context)
		if err != nil {
			return err
		}
		analysis, err := analyzeApply(context, machine, moduleID)
		if err != nil {
			return err
		}
		return printDryRunAnalysis(command, analysis)
	}

	machine, err := loadRequiredMachine(context)
	if err != nil {
		return err
	}
	result, runErr := mutation.Apply(mutation.ApplyRequest{
		Home:      context.home,
		Controls:  context.controls(machine.Repository),
		Machine:   machine,
		Platform:  context.platform,
		ModuleID:  moduleID,
		RerunHint: rerun,
	})
	return finishMutation(command, result, runErr, rerun)
}
