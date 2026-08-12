package cli

import (
	"github.com/mianm12/dotfiles/internal/core/converge"
	"github.com/spf13/cobra"
)

func newStatusCommand(env environment) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Inspect module convergence without mutation",
		Args:  noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runStatus(command, env)
		},
	}
}

func runStatus(command *cobra.Command, env environment) error {
	context, err := resolveContext(env)
	if err != nil {
		return err
	}
	report, err := converge.Analyze(context.environment())
	if err != nil {
		return err
	}
	return printStatusAnalysis(command, report)
}
