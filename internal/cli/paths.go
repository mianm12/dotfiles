package cli

import "github.com/spf13/cobra"

func newPathsCommand(env environment) *cobra.Command {
	return &cobra.Command{
		Use:   "paths",
		Short: "Show dot control paths",
		Args:  noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			context, err := resolveControlContext(env)
			if err != nil {
				return err
			}
			command.Printf(
				"home=%s\nmachine_config=%s\nstate=%s\nlock=%s\n",
				context.home,
				context.configPath,
				context.statePath,
				context.lockPath,
			)
			return nil
		},
	}
}
