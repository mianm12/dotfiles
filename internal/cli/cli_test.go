package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghstlnx/dotfiles/internal/buildinfo"
)

func TestVersion_RepositoryUnavailable(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home-that-does-not-exist")
	stdout, stderr, exitCode := runForTest(t, []string{"version", "--home", home}, nil, buildinfo.Info{
		Version:   "v1.2.3",
		Commit:    "abc123",
		BuildTime: "2026-07-16T10:00:00Z",
	})

	if exitCode != 0 {
		t.Errorf("run() exit code = %d, want 0", exitCode)
	}
	want := "version=v1.2.3\ncommit=abc123\nbuild_time=2026-07-16T10:00:00Z\nrequires=unavailable\n"
	if stdout != want {
		t.Errorf("run() stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("run() stderr = %q, want empty", stderr)
	}
	// version 是只读命令，仓库缺失时也不得创建 effective home。
	if _, err := os.Stat(home); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("os.Stat(%q) error = %v, want fs.ErrNotExist", home, err)
	}
}

func TestVersion_AcceptsGlobalFlagsBeforeCommand(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home-that-does-not-exist")
	stdout, stderr, exitCode := runForTest(t, []string{"--home", home, "version"}, nil, buildinfo.Info{
		Version:   "v1.2.3",
		Commit:    "abc123",
		BuildTime: "2026-07-16T10:00:00Z",
	})

	if exitCode != 0 {
		t.Errorf("run() exit code = %d, want 0", exitCode)
	}
	wantStdoutSuffix := "requires=unavailable\n"
	if !strings.HasSuffix(stdout, wantStdoutSuffix) {
		t.Errorf("run() stdout = %q, want suffix %q", stdout, wantStdoutSuffix)
	}
	if stderr != "" {
		t.Errorf("run() stderr = %q, want empty", stderr)
	}
}

func TestVersion_DevelopmentBuild(t *testing.T) {
	repo := writeRepository(t, ">=999.0.0")
	stdout, stderr, exitCode := runForTest(t, []string{"version", "--repo", repo}, nil, buildinfo.Info{
		Version:   "dev",
		Commit:    "unknown",
		BuildTime: "unknown",
	})

	if exitCode != 0 {
		t.Errorf("run() exit code = %d, want 0", exitCode)
	}
	wantStdout := "requires=>=999.0.0\nsatisfied=true\ncompatibility=development-build\n"
	if !strings.Contains(stdout, wantStdout) {
		t.Errorf("run() stdout = %q, want substring %q", stdout, wantStdout)
	}
	wantStderr := "warning: development build"
	if !strings.Contains(stderr, wantStderr) {
		t.Errorf("run() stderr = %q, want substring %q", stderr, wantStderr)
	}
}

func TestVersion_UnsatisfiedRequirement(t *testing.T) {
	repo := writeRepository(t, ">=2.0.0")
	stdout, stderr, exitCode := runForTest(t, []string{"version", "--repo", repo}, nil, buildinfo.Info{
		Version:   "v1.9.9",
		Commit:    "abc123",
		BuildTime: "2026-07-16T10:00:00Z",
	})

	if exitCode != 1 {
		t.Errorf("run() exit code = %d, want 1", exitCode)
	}
	wantStdout := "requires=>=2.0.0\nsatisfied=false\n"
	if !strings.Contains(stdout, wantStdout) {
		t.Errorf("run() stdout = %q, want substring %q", stdout, wantStdout)
	}
	wantStderr := "run dot self-update"
	if !strings.Contains(stderr, wantStderr) {
		t.Errorf("run() stderr = %q, want substring %q", stderr, wantStderr)
	}
}

func TestVersion_RejectsInvalidMachineConfig(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.toml")
	if err := os.WriteFile(configPath, []byte("profile = \"mac\"\nunknown = true\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", configPath, err)
	}
	repo := writeRepository(t, ">=1.0.0")
	envVars := map[string]string{"DOT_CONFIG": configPath}

	stdout, stderr, exitCode := runForTest(t, []string{"version", "--home", home, "--repo", repo}, envVars, buildinfo.Info{
		Version:   "v1.0.0",
		Commit:    "abc123",
		BuildTime: "2026-07-16T10:00:00Z",
	})

	if exitCode != 1 {
		t.Errorf("run() exit code = %d, want 1", exitCode)
	}
	wantStdoutSuffix := "requires=error\n"
	if !strings.HasSuffix(stdout, wantStdoutSuffix) {
		t.Errorf("run() stdout = %q, want suffix %q", stdout, wantStdoutSuffix)
	}
	wantStderr := "machine config"
	if !strings.Contains(stderr, wantStderr) {
		t.Errorf("run() stderr = %q, want substring %q", stderr, wantStderr)
	}
}

func TestVersion_RejectsInvalidConfiguredRepositoryDespiteOverride(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.toml")
	content := "profile = \"mac\"\nrepo = \"relative/repo\"\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", configPath, err)
	}
	repo := writeRepository(t, ">=1.0.0")
	envVars := map[string]string{"DOT_CONFIG": configPath}

	stdout, stderr, exitCode := runForTest(t, []string{"version", "--home", home, "--repo", repo}, envVars, buildinfo.Info{
		Version:   "v1.0.0",
		Commit:    "abc123",
		BuildTime: "2026-07-16T10:00:00Z",
	})

	if exitCode != 1 {
		t.Errorf("run() exit code = %d, want 1", exitCode)
	}
	wantStdoutSuffix := "requires=error\n"
	if !strings.HasSuffix(stdout, wantStdoutSuffix) {
		t.Errorf("run() stdout = %q, want suffix %q", stdout, wantStdoutSuffix)
	}
	wantStderr := "machine config repo"
	if !strings.Contains(stderr, wantStderr) {
		t.Errorf("run() stderr = %q, want substring %q", stderr, wantStderr)
	}
}

func TestVersion_RejectsInvalidPathBeforeRepositoryRead(t *testing.T) {
	// 显式控制路径非法时必须直接失败，不能因仓库不可用而降级为 unavailable。
	stdout, stderr, exitCode := runForTest(t, []string{"version"}, map[string]string{
		"DOT_CONFIG": "relative/config.toml",
	}, buildinfo.Info{Version: "v1.0.0", Commit: "abc123", BuildTime: "now"})

	if exitCode != 1 {
		t.Errorf("run() exit code = %d, want 1", exitCode)
	}
	wantStdoutSuffix := "requires=error\n"
	if !strings.HasSuffix(stdout, wantStdoutSuffix) {
		t.Errorf("run() stdout = %q, want suffix %q", stdout, wantStdoutSuffix)
	}
	wantStderr := "DOT_CONFIG"
	if !strings.Contains(stderr, wantStderr) {
		t.Errorf("run() stderr = %q, want substring %q", stderr, wantStderr)
	}
}

func TestVersion_RejectsInvalidRequires(t *testing.T) {
	repo := writeRepository(t, "^1.0.0")
	stdout, stderr, exitCode := runForTest(t, []string{"version", "--repo", repo}, nil, buildinfo.Info{
		Version:   "dev",
		Commit:    "unknown",
		BuildTime: "unknown",
	})

	if exitCode != 1 {
		t.Errorf("run() exit code = %d, want 1", exitCode)
	}
	wantStdoutSuffix := "requires=error\n"
	if !strings.HasSuffix(stdout, wantStdoutSuffix) {
		t.Errorf("run() stdout = %q, want suffix %q", stdout, wantStdoutSuffix)
	}
	wantStderr := "invalid requires"
	if !strings.Contains(stderr, wantStderr) {
		t.Errorf("run() stderr = %q, want substring %q", stderr, wantStderr)
	}
}

func TestVersion_Help(t *testing.T) {
	stdout, stderr, exitCode := runForTest(t, []string{"version", "--help"}, nil, buildinfo.Info{})

	if exitCode != 0 {
		t.Errorf("run() exit code = %d, want 0", exitCode)
	}
	for _, want := range []string{"Usage:", "dot version"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("run() stdout = %q, want substring %q", stdout, want)
		}
	}
	if strings.Contains(stdout, "--home") {
		t.Errorf("run() stdout = %q, want hidden --home flag omitted", stdout)
	}
	if stderr != "" {
		t.Errorf("run() stderr = %q, want empty", stderr)
	}
}

func TestVersion_RejectsEmptyProfile(t *testing.T) {
	_, stderr, exitCode := runForTest(t, []string{"version", "--profile="}, nil, buildinfo.Info{})

	if exitCode != 1 {
		t.Errorf("run() exit code = %d, want 1", exitCode)
	}
	wantStderr := "--profile must not be empty"
	if !strings.Contains(stderr, wantStderr) {
		t.Errorf("run() stderr = %q, want substring %q", stderr, wantStderr)
	}
}

func TestRoot_HelpListsSpecifiedCommandsAndFlags(t *testing.T) {
	stdout, stderr, exitCode := runForTest(t, []string{"--help"}, nil, buildinfo.Info{})

	if exitCode != 0 {
		t.Errorf("run() exit code = %d, want 0", exitCode)
	}
	for _, want := range []string{"version", "--repo", "--profile", "--verbose", "--no-color"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("run() stdout = %q, want substring %q", stdout, want)
		}
	}
	for _, unwanted := range []string{"completion", "--home"} {
		if strings.Contains(stdout, unwanted) {
			t.Errorf("run() stdout = %q, want substring %q omitted", stdout, unwanted)
		}
	}
	if stderr != "" {
		t.Errorf("run() stderr = %q, want empty", stderr)
	}
}

func TestRoot_GlobalFlags(t *testing.T) {
	root, err := newRootCommand(environment{})
	if err != nil {
		t.Fatalf("newRootCommand() error = %v, want nil", err)
	}

	tests := []struct {
		name      string
		shorthand string
		hidden    bool
	}{
		{name: "repo"},
		{name: "home", hidden: true},
		{name: "profile"},
		{name: "verbose", shorthand: "v"},
		{name: "no-color"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := root.PersistentFlags().Lookup(tt.name)
			if flag == nil {
				t.Fatalf("global flag %q is not registered", tt.name)
			}
			if flag.Shorthand != tt.shorthand {
				t.Errorf("global flag %q shorthand = %q, want %q", tt.name, flag.Shorthand, tt.shorthand)
			}
			if flag.Hidden != tt.hidden {
				t.Errorf("global flag %q hidden = %t, want %t", tt.name, flag.Hidden, tt.hidden)
			}
		})
	}
}

func TestRoot_RequiresCommand(t *testing.T) {
	stdout, stderr, exitCode := runForTest(t, nil, nil, buildinfo.Info{})

	if exitCode != 1 {
		t.Errorf("run() exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stdout, "Usage:") {
		t.Errorf("run() stdout = %q, want root help", stdout)
	}
	wantStderr := "a command is required"
	if !strings.Contains(stderr, wantStderr) {
		t.Errorf("run() stderr = %q, want substring %q", stderr, wantStderr)
	}
}

func runForTest(t *testing.T, args []string, envVars map[string]string, build buildinfo.Info) (string, string, int) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	home := t.TempDir()
	exitCode := run(args, environment{
		stdout: &stdout,
		stderr: &stderr,
		lookupEnv: func(name string) (string, bool) {
			value, ok := envVars[name]
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
	manifestPath := filepath.Join(repo, "dot.toml")
	if err := os.WriteFile(manifestPath, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", manifestPath, err)
	}
	return repo
}
