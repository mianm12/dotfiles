package cli

import (
	"github.com/spf13/cobra"
)

func newVersionCommand(env environment) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show build information",
		Args:  noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runVersion(command, env)
		},
	}
}

func runVersion(command *cobra.Command, env environment) error {
	command.Printf("version=%s\n", env.build.Version)
	command.Printf("commit=%s\n", env.build.Commit)
	command.Printf("build_time=%s\n", env.build.BuildTime)
	return nil
}
