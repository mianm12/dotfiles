package cli

import (
	"github.com/mianm12/dotfiles/internal/core/converge"
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
		report, err := converge.Analyze(context.environment())
		if err != nil {
			return err
		}
		return printDryRunAnalysis(command, report)
	}

	result, runErr := converge.Apply(context.environment())
	return finishMutation(command, result, runErr, rerun)
}
