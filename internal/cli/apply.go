package cli

import (
	"github.com/mianm12/dotfiles/internal/core/mutation"
	"github.com/spf13/cobra"
)

func newApplyCommand(env environment) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "apply",
		Short: "Converge the current effective selection",
		Args:  noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runApply(command, dryRun, env)
		},
	}
	command.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "print the plan without mutation")
	return command
}

func runApply(
	command *cobra.Command,
	dryRun bool,
	env environment,
) error {
	context, err := resolveContext(env)
	if err != nil {
		return err
	}
	rerun := "dot apply"
	if dryRun {
		machine, err := loadRequiredMachine(context)
		if err != nil {
			return err
		}
		analysis, err := analyzeApply(context, machine)
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
		RerunHint: rerun,
	})
	return finishMutation(command, result, runErr, rerun)
}
