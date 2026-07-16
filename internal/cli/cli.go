package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ghstlnx/dotfiles/internal/buildinfo"
	"github.com/ghstlnx/dotfiles/internal/config"
	"github.com/ghstlnx/dotfiles/internal/manifest"
	"github.com/ghstlnx/dotfiles/internal/paths"
)

type environment struct {
	stdout      io.Writer
	stderr      io.Writer
	lookupEnv   func(string) (string, bool)
	userHomeDir func() (string, error)
	build       buildinfo.Info
}

type outputWriter struct {
	writer io.Writer
	err    error
}

func (output *outputWriter) printf(format string, arguments ...any) {
	if output.err != nil {
		return
	}
	_, output.err = fmt.Fprintf(output.writer, format, arguments...)
}

func (output *outputWriter) println(arguments ...any) {
	if output.err != nil {
		return
	}
	_, output.err = fmt.Fprintln(output.writer, arguments...)
}

type commandOutput struct {
	stdout outputWriter
	stderr outputWriter
}

func newCommandOutput(stdout, stderr io.Writer) *commandOutput {
	return &commandOutput{
		stdout: outputWriter{writer: stdout},
		stderr: outputWriter{writer: stderr},
	}
}

func (output *commandOutput) exitCode(code int) int {
	if output.stdout.err != nil {
		output.stderr.printf("error: write stdout: %v\n", output.stdout.err)
		return 1
	}
	if output.stderr.err != nil {
		return 1
	}
	return code
}

type versionOptions struct {
	home       string
	homeSet    bool
	profile    string
	profileSet bool
	repo       string
	repoSet    bool
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
	output := newCommandOutput(env.stdout, env.stderr)
	if len(args) == 0 {
		writeUsage(&output.stderr)
		return output.exitCode(1)
	}

	switch args[0] {
	case "version":
		options, help, err := parseVersionOptions(args[1:], &output.stdout)
		if help {
			return output.exitCode(0)
		}
		if err != nil {
			output.stderr.printf("error: %v\n", err)
			return output.exitCode(1)
		}
		return runVersion(options, env, output)
	default:
		output.stderr.printf("error: unknown command %q\n", args[0])
		writeUsage(&output.stderr)
		return output.exitCode(1)
	}
}

func parseVersionOptions(args []string, output *outputWriter) (versionOptions, bool, error) {
	var options versionOptions
	var verbose bool
	var noColor bool

	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.home, "home", "", "override the effective home (test use only)")
	flags.StringVar(&options.profile, "profile", "", "override the configured profile")
	flags.StringVar(&options.repo, "repo", "", "override the repository path")
	flags.BoolVar(&verbose, "v", false, "enable verbose output")
	flags.BoolVar(&verbose, "verbose", false, "enable verbose output")
	flags.BoolVar(&noColor, "no-color", false, "disable colored output")
	flags.Usage = func() {}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			writeVersionUsage(output)
			return versionOptions{}, true, nil
		}
		return versionOptions{}, false, err
	}
	if flags.NArg() != 0 {
		return versionOptions{}, false, fmt.Errorf("version does not accept positional arguments")
	}
	flags.Visit(func(current *flag.Flag) {
		switch current.Name {
		case "home":
			options.homeSet = true
		case "profile":
			options.profileSet = true
		case "repo":
			options.repoSet = true
		}
	})
	if options.profileSet && options.profile == "" {
		return versionOptions{}, false, fmt.Errorf("--profile must not be empty")
	}
	return options, false, nil
}

func runVersion(options versionOptions, env environment, output *commandOutput) int {
	output.stdout.printf("version=%s\n", env.build.Version)
	output.stdout.printf("commit=%s\n", env.build.Commit)
	output.stdout.printf("build_time=%s\n", env.build.BuildTime)

	home, err := paths.EffectiveHome(options.home, options.homeSet, env.userHomeDir)
	if err != nil {
		return reportVersionError(output, err)
	}
	configPath, err := paths.Config(home, env.lookupEnv)
	if err != nil {
		return reportVersionError(output, err)
	}
	machine, exists, err := config.Load(configPath)
	if err != nil {
		return reportVersionError(output, err)
	}
	if exists && machine.Repo != nil {
		if _, err := paths.ResolveControlPath(*machine.Repo, home); err != nil {
			return reportVersionError(output, fmt.Errorf("machine config repo: %w", err))
		}
	}

	repo, err := paths.Repository(home, options.repo, options.repoSet, env.lookupEnv, machine.Repo)
	if err != nil {
		return reportVersionError(output, err)
	}
	requirement, err := manifest.ReadRequirement(repo)
	if errors.Is(err, manifest.ErrRepositoryUnavailable) {
		output.stdout.println("requires=unavailable")
		return output.exitCode(0)
	}
	if err != nil {
		return reportVersionError(output, err)
	}

	output.stdout.printf("requires=%s\n", requirement.Raw)
	satisfied, development, err := manifest.Satisfies(env.build.Version, requirement)
	if err != nil {
		output.stdout.println("satisfied=error")
		output.stderr.printf("error: %v\n", err)
		return output.exitCode(1)
	}
	output.stdout.printf("satisfied=%t\n", satisfied)
	if development {
		output.stdout.println("compatibility=development-build")
		output.stderr.println("warning: development build skipped the requires version comparison")
		return output.exitCode(0)
	}
	if !satisfied {
		output.stderr.printf("error: CLI %s does not satisfy %s; run dot self-update\n", env.build.Version, requirement.Raw)
		return output.exitCode(1)
	}
	return output.exitCode(0)
}

func reportVersionError(output *commandOutput, err error) int {
	output.stdout.println("requires=error")
	output.stderr.printf("error: %v\n", err)
	return output.exitCode(1)
}

func writeUsage(output *outputWriter) {
	output.println("usage: dot <command> [flags] [args]")
	output.println("commands: version")
}

func writeVersionUsage(output *outputWriter) {
	output.println("usage: dot version [--repo <dir>] [--profile <name>] [-v|--verbose] [--no-color]")
}
