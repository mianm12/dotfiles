package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ghstlnx/dotfiles/internal/buildinfo"
	"github.com/spf13/cobra"
)

type environment struct {
	stdout      io.Writer
	stderr      io.Writer
	lookupEnv   func(string) (string, bool)
	userHomeDir func() (string, error)
	build       buildinfo.Info
}

type globalOptions struct {
	home    string
	profile string
	repo    string
	verbose bool
	noColor bool
}

// Run executes dot and returns its process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	return run(args, environment{
		stdout:      stdout,
		stderr:      stderr,
		lookupEnv:   os.LookupEnv,
		userHomeDir: os.UserHomeDir,
		build:       buildinfo.Current(),
	})
}

func run(args []string, env environment) int {
	root, err := newRootCommand(env)
	if err != nil {
		_, _ = fmt.Fprintf(env.stderr, "error: initialize CLI: %v\n", err)
		return 1
	}
	root.SetArgs(args)
	root.SetOut(env.stdout)
	root.SetErr(env.stderr)

	if err := root.Execute(); err != nil {
		root.PrintErrf("error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCommand(env environment) (*cobra.Command, error) {
	var options globalOptions
	root := &cobra.Command{
		Use:           "dot",
		Short:         "Manage a personal dotfiles repository",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := command.Help(); err != nil {
				return err
			}
			return errors.New("a command is required")
		},
		PersistentPreRunE: func(command *cobra.Command, _ []string) error {
			if command.Flags().Changed("profile") && options.profile == "" {
				return errors.New("--profile must not be empty")
			}
			return nil
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true

	flags := root.PersistentFlags()
	flags.StringVar(&options.repo, "repo", "", "override the repository path")
	flags.StringVar(&options.home, "home", "", "override the effective home")
	flags.StringVar(&options.profile, "profile", "", "override the configured profile")
	flags.BoolVarP(&options.verbose, "verbose", "v", false, "enable verbose output")
	flags.BoolVar(&options.noColor, "no-color", false, "disable colored output")
	if err := flags.MarkHidden("home"); err != nil {
		return nil, err
	}

	root.AddCommand(newVersionCommand(env, &options))
	return root, nil
}
