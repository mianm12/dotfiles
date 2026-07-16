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
	if len(args) == 0 {
		writeUsage(env.stderr)
		return 1
	}

	switch args[0] {
	case "version":
		options, help, err := parseVersionOptions(args[1:], env.stdout)
		if help {
			return 0
		}
		if err != nil {
			fmt.Fprintf(env.stderr, "error: %v\n", err)
			return 1
		}
		return runVersion(options, env)
	default:
		fmt.Fprintf(env.stderr, "error: unknown command %q\n", args[0])
		writeUsage(env.stderr)
		return 1
	}
}

func parseVersionOptions(args []string, output io.Writer) (versionOptions, bool, error) {
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

func runVersion(options versionOptions, env environment) int {
	fmt.Fprintf(env.stdout, "version=%s\n", env.build.Version)
	fmt.Fprintf(env.stdout, "commit=%s\n", env.build.Commit)
	fmt.Fprintf(env.stdout, "build_time=%s\n", env.build.BuildTime)

	home, err := paths.EffectiveHome(options.home, options.homeSet, env.userHomeDir)
	if err != nil {
		return reportVersionError(env, err)
	}
	configPath, err := paths.Config(home, env.lookupEnv)
	if err != nil {
		return reportVersionError(env, err)
	}
	machine, exists, err := config.Load(configPath)
	if err != nil {
		return reportVersionError(env, err)
	}
	if exists && machine.Repo != nil {
		if _, err := paths.ResolveControlPath(*machine.Repo, home); err != nil {
			return reportVersionError(env, fmt.Errorf("machine config repo: %w", err))
		}
	}

	repo, err := paths.Repository(home, options.repo, options.repoSet, env.lookupEnv, machine.Repo)
	if err != nil {
		return reportVersionError(env, err)
	}
	requirement, err := manifest.ReadRequirement(repo)
	if errors.Is(err, manifest.ErrRepositoryUnavailable) {
		fmt.Fprintln(env.stdout, "requires=unavailable")
		return 0
	}
	if err != nil {
		return reportVersionError(env, err)
	}

	fmt.Fprintf(env.stdout, "requires=%s\n", requirement.Raw)
	satisfied, development, err := manifest.Satisfies(env.build.Version, requirement)
	if err != nil {
		fmt.Fprintln(env.stdout, "satisfied=error")
		fmt.Fprintf(env.stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(env.stdout, "satisfied=%t\n", satisfied)
	if development {
		fmt.Fprintln(env.stdout, "compatibility=development-build")
		fmt.Fprintln(env.stderr, "warning: development build skipped the requires version comparison")
		return 0
	}
	if !satisfied {
		fmt.Fprintf(env.stderr, "error: CLI %s does not satisfy %s; run dot self-update\n", env.build.Version, requirement.Raw)
		return 1
	}
	return 0
}

func reportVersionError(env environment, err error) int {
	fmt.Fprintln(env.stdout, "requires=error")
	fmt.Fprintf(env.stderr, "error: %v\n", err)
	return 1
}

func writeUsage(output io.Writer) {
	fmt.Fprintln(output, "usage: dot <command> [flags] [args]")
	fmt.Fprintln(output, "commands: version")
}

func writeVersionUsage(output io.Writer) {
	fmt.Fprintln(output, "usage: dot version [--repo <dir>] [--profile <name>] [-v|--verbose] [--no-color]")
}
