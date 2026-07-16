package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghstlnx/dotfiles/internal/buildinfo"
)

func TestVersionRepositoryUnavailable(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home-that-does-not-exist")
	stdout, stderr, exitCode := runForTest(t, []string{"version", "--home", home}, nil, buildinfo.Info{
		Version:   "v1.2.3",
		Commit:    "abc123",
		BuildTime: "2026-07-16T10:00:00Z",
	})

	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, stderr = %q", exitCode, stderr)
	}
	want := "version=v1.2.3\ncommit=abc123\nbuild_time=2026-07-16T10:00:00Z\nrequires=unavailable\n"
	if stdout != want {
		t.Fatalf("run() stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Fatalf("run() stderr = %q, want empty", stderr)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("version created effective home or returned unexpected error: %v", err)
	}
}

func TestVersionDevelopmentBuild(t *testing.T) {
	repo := writeRepository(t, ">=999.0.0")
	stdout, stderr, exitCode := runForTest(t, []string{"version", "--repo", repo}, nil, buildinfo.Info{
		Version:   "dev",
		Commit:    "unknown",
		BuildTime: "unknown",
	})

	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, stderr = %q", exitCode, stderr)
	}
	if !strings.Contains(stdout, "requires=>=999.0.0\nsatisfied=true\ncompatibility=development-build\n") {
		t.Fatalf("run() stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "warning: development build") {
		t.Fatalf("run() stderr = %q, want development warning", stderr)
	}
}

func TestVersionUnsatisfied(t *testing.T) {
	repo := writeRepository(t, ">=2.0.0")
	stdout, stderr, exitCode := runForTest(t, []string{"version", "--repo", repo}, nil, buildinfo.Info{
		Version:   "v1.9.9",
		Commit:    "abc123",
		BuildTime: "2026-07-16T10:00:00Z",
	})

	if exitCode != 1 {
		t.Fatalf("run() exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stdout, "requires=>=2.0.0\nsatisfied=false\n") {
		t.Fatalf("run() stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "run dot self-update") {
		t.Fatalf("run() stderr = %q, want update guidance", stderr)
	}
}

func TestVersionRejectsInvalidMachineConfig(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.toml")
	if err := os.WriteFile(configPath, []byte("profile = \"mac\"\nunknown = true\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	repo := writeRepository(t, ">=1.0.0")
	environment := map[string]string{"DOT_CONFIG": configPath}

	stdout, stderr, exitCode := runForTest(t, []string{"version", "--home", home, "--repo", repo}, environment, buildinfo.Info{
		Version:   "v1.0.0",
		Commit:    "abc123",
		BuildTime: "2026-07-16T10:00:00Z",
	})

	if exitCode != 1 {
		t.Fatalf("run() exit code = %d, want 1", exitCode)
	}
	if !strings.HasSuffix(stdout, "requires=error\n") {
		t.Fatalf("run() stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "machine config") {
		t.Fatalf("run() stderr = %q, want machine config error", stderr)
	}
}

func TestVersionRejectsInvalidPathBeforeRepositoryRead(t *testing.T) {
	stdout, stderr, exitCode := runForTest(t, []string{"version"}, map[string]string{
		"DOT_CONFIG": "relative/config.toml",
	}, buildinfo.Info{Version: "v1.0.0", Commit: "abc123", BuildTime: "now"})

	if exitCode != 1 {
		t.Fatalf("run() exit code = %d, want 1", exitCode)
	}
	if !strings.HasSuffix(stdout, "requires=error\n") {
		t.Fatalf("run() stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "DOT_CONFIG") {
		t.Fatalf("run() stderr = %q, want DOT_CONFIG error", stderr)
	}
}

func TestVersionRejectsInvalidRequires(t *testing.T) {
	repo := writeRepository(t, "^1.0.0")
	stdout, _, exitCode := runForTest(t, []string{"version", "--repo", repo}, nil, buildinfo.Info{
		Version:   "dev",
		Commit:    "unknown",
		BuildTime: "unknown",
	})

	if exitCode != 1 {
		t.Fatalf("run() exit code = %d, want 1", exitCode)
	}
	if !strings.HasSuffix(stdout, "requires=error\n") {
		t.Fatalf("run() stdout = %q", stdout)
	}
}

func TestVersionHelp(t *testing.T) {
	stdout, stderr, exitCode := runForTest(t, []string{"version", "--help"}, nil, buildinfo.Info{})

	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, want 0", exitCode)
	}
	want := "usage: dot version [--repo <dir>] [--profile <name>] [-v|--verbose] [--no-color]\n"
	if stdout != want {
		t.Fatalf("run() stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Fatalf("run() stderr = %q, want empty", stderr)
	}
}

func TestVersionReturnsErrorWhenStdoutWriteFails(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := run([]string{"version", "--home", filepath.Join(t.TempDir(), "missing")}, environment{
		stdout: failingWriter{err: errors.New("broken pipe")},
		stderr: &stderr,
		lookupEnv: func(string) (string, bool) {
			return "", false
		},
		userHomeDir: func() (string, error) {
			return t.TempDir(), nil
		},
		build: buildinfo.Info{Version: "dev", Commit: "unknown", BuildTime: "unknown"},
	})

	if exitCode != 1 {
		t.Fatalf("run() exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "write stdout: broken pipe") {
		t.Fatalf("run() stderr = %q, want stdout error", stderr.String())
	}
}

func runForTest(t *testing.T, args []string, variables map[string]string, build buildinfo.Info) (string, string, int) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	home := t.TempDir()
	exitCode := run(args, environment{
		stdout: &stdout,
		stderr: &stderr,
		lookupEnv: func(name string) (string, bool) {
			value, ok := variables[name]
			return value, ok
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		build: build,
	})
	return stdout.String(), stderr.String(), exitCode
}

func writeRepository(t *testing.T, requires string) string {
	t.Helper()
	repo := t.TempDir()
	content := "requires = \"" + requires + "\"\n"
	if err := os.WriteFile(filepath.Join(repo, "dot.toml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return repo
}

type failingWriter struct {
	err error
}

func (writer failingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
