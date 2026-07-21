package add

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type gitResult struct {
	exitCode int
	output   []byte
}

type gitRunner func(repository string, environment, arguments []string) (gitResult, error)

func runSystemGit(repository string, environment, arguments []string) (gitResult, error) {
	commandArguments := append([]string{"-C", repository}, arguments...)
	command := exec.Command("git", commandArguments...)
	command.Env = environment
	output, err := command.CombinedOutput()
	if err == nil {
		return gitResult{exitCode: 0, output: output}, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return gitResult{exitCode: exitError.ExitCode(), output: output}, nil
	}
	return gitResult{}, fmt.Errorf("start git: %w", err)
}

func gitTrackable(
	run gitRunner,
	repository string,
	environment []string,
	source string,
) error {
	tracked, err := run(repository, environment, []string{"ls-files", "--error-unmatch", "--", source})
	if err != nil {
		return fmt.Errorf("inspect Git tracking for %q: %w", source, err)
	}
	switch tracked.exitCode {
	case 0:
		return nil
	case 1:
		// 尚未跟踪；继续用 Git 自身的完整 ignore/exclude 语义判断。
	default:
		return fmt.Errorf("inspect Git tracking for %q: git exited %d: %s", source, tracked.exitCode, strings.TrimSpace(string(tracked.output)))
	}

	ignored, err := run(repository, environment, []string{"check-ignore", "-q", "--no-index", "--", source})
	if err != nil {
		return fmt.Errorf("inspect Git ignore for %q: %w", source, err)
	}
	switch ignored.exitCode {
	case 0:
		return fmt.Errorf("source %q is ignored by Git", source)
	case 1:
		return nil
	default:
		return fmt.Errorf("inspect Git ignore for %q: git exited %d: %s", source, ignored.exitCode, strings.TrimSpace(string(ignored.output)))
	}
}

func gitEnvironment(base []string, home string) []string {
	result := make([]string, 0, len(base)+6)
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if name == "HOME" || strings.HasPrefix(name, "XDG_") || strings.HasPrefix(name, "GIT_") {
			continue
		}
		result = append(result, entry)
	}
	return append(
		result,
		"HOME="+home,
		"XDG_CONFIG_HOME="+home+string(os.PathSeparator)+".config",
		"XDG_DATA_HOME="+home+string(os.PathSeparator)+".local"+string(os.PathSeparator)+"share",
		"XDG_STATE_HOME="+home+string(os.PathSeparator)+".local"+string(os.PathSeparator)+"state",
		"XDG_CACHE_HOME="+home+string(os.PathSeparator)+".cache",
		"GIT_CONFIG_NOSYSTEM=1",
	)
}
