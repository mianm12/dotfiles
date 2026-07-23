// Package cli exposes the public dot command surface.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/mianm12/dotfiles/internal/buildinfo"
	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/spf13/cobra"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

type usageError struct {
	err error
}

func (err usageError) Error() string { return err.err.Error() }
func (err usageError) Unwrap() error { return err.err }

func usagef(format string, arguments ...any) error {
	return usageError{err: fmt.Errorf(format, arguments...)}
}

type environment struct {
	stdin                 io.Reader
	stdout                io.Writer
	stderr                io.Writer
	userHomeDir           func() (string, error)
	getwd                 func() (string, error)
	platform              func() config.Platform
	afterSelectionPublish func() error
	build                 buildinfo.Info
}

// Run executes dot with arguments that exclude the program name.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return run(args, environment{
		stdin:       stdin,
		stdout:      stdout,
		stderr:      stderr,
		userHomeDir: os.UserHomeDir,
		getwd:       os.Getwd,
		platform: func() config.Platform {
			return detectPlatform(runtime.GOOS, runtime.GOARCH, os.ReadFile)
		},
		build: buildinfo.Current(),
	})
}

func run(args []string, env environment) int {
	root := newRootCommand(env)
	output := newCommandOutput(env.stdout, env.stderr)
	root.SetArgs(args)
	root.SetIn(env.stdin)
	root.SetOut(&output.stdout)
	root.SetErr(&output.stderr)
	root.InitDefaultHelpCmd()

	if _, _, err := root.Find(args); err != nil {
		_, _ = fmt.Fprintf(&output.stderr, "error: %v\n", err)
		return output.finish(exitUsage)
	}
	if err := root.Execute(); err != nil {
		code := exitError
		var usage usageError
		if errors.As(err, &usage) {
			code = exitUsage
		}
		root.PrintErrf("error: %v\n", err)
		return output.finish(code)
	}
	return output.finish(exitOK)
}

func newRootCommand(env environment) *cobra.Command {
	root := &cobra.Command{
		Use:           "dot",
		Short:         "Manage a personal dotfiles repository",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return usagef("a command is required")
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{err: err}
	})
	root.AddCommand(
		newInitCommand(env),
		newStatusCommand(env),
		newApplyCommand(env),
		newRemoveCommand(env),
		newVersionCommand(env),
	)
	root.SetHelpCommand(newHelpCommand(root))
	return root
}

func newHelpCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "help [COMMAND]",
		Short: "Help about any command",
		Args:  maximumArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			target := root
			if len(args) == 1 {
				found, remaining, err := root.Find(args)
				if err != nil || len(remaining) != 0 || found == root {
					return usagef("unknown help topic %q", args[0])
				}
				target = found
			}
			return target.Help()
		},
	}
}

func noArgs(command *cobra.Command, args []string) error {
	if len(args) != 0 {
		return usagef("%s accepts no arguments", command.CommandPath())
	}
	return nil
}

func maximumArgs(maximum int) cobra.PositionalArgs {
	return func(command *cobra.Command, args []string) error {
		if len(args) > maximum {
			return usagef(
				"%s accepts at most %d argument(s)",
				command.CommandPath(),
				maximum,
			)
		}
		return nil
	}
}

func exactArgs(count int) cobra.PositionalArgs {
	return func(command *cobra.Command, args []string) error {
		if len(args) != count {
			return usagef(
				"%s requires exactly %d argument(s)",
				command.CommandPath(),
				count,
			)
		}
		return nil
	}
}

type errorTrackingWriter struct {
	writer io.Writer
	err    error
}

func (writer *errorTrackingWriter) Write(data []byte) (int, error) {
	if writer.err != nil {
		return 0, writer.err
	}
	written, err := writer.writer.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		writer.err = err
	}
	return written, err
}

type commandOutput struct {
	stdout errorTrackingWriter
	stderr errorTrackingWriter
}

func newCommandOutput(stdout, stderr io.Writer) *commandOutput {
	return &commandOutput{
		stdout: errorTrackingWriter{writer: stdout},
		stderr: errorTrackingWriter{writer: stderr},
	}
}

func (output *commandOutput) finish(code int) int {
	if output.stdout.err != nil {
		_, _ = fmt.Fprintf(&output.stderr, "error: write stdout: %v\n", output.stdout.err)
		return exitError
	}
	if output.stderr.err != nil {
		return exitError
	}
	return code
}
