package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/buildinfo"
)

func TestVersion_PrintsBuildInfoWithoutRepositoryAccess(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home-that-does-not-exist")
	stdout, stderr, code := runForTest(t, []string{"version", "--home", home}, nil, buildinfo.Info{
		Version:   "v1.2.3",
		Commit:    "abc123",
		BuildTime: "2026-07-16T10:00:00Z",
	})
	want := "version=v1.2.3\ncommit=abc123\nbuild_time=2026-07-16T10:00:00Z\n"
	if code != exitOK || stdout != want || stderr != "" {
		t.Fatalf("version = stdout %q, stderr %q, exit %d; want %q, empty, 0", stdout, stderr, code, want)
	}
	if _, err := os.Stat(home); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("version changed missing home %q: %v", home, err)
	}
}

func TestVersion_AcceptsGlobalFlagsBeforeCommand(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home-that-does-not-exist")
	stdout, stderr, code := runForTest(t, []string{"--home", home, "version"}, nil, buildinfo.Info{
		Version: "v1.2.3", Commit: "abc123", BuildTime: "now",
	})
	if code != exitOK || stdout != "version=v1.2.3\ncommit=abc123\nbuild_time=now\n" || stderr != "" {
		t.Fatalf("global flags before version = (%q, %q, %d)", stdout, stderr, code)
	}
}

func TestVersion_ExitsWithErrorWhenStdoutWriteFails(t *testing.T) {
	var stderr bytes.Buffer
	home := filepath.Join(t.TempDir(), "missing-home")
	code := run([]string{"version", "--home", home}, environment{
		stdout: failingWriter{err: errors.New("broken pipe")},
		stderr: &stderr,
		userHomeDir: func() (string, error) {
			return home, nil
		},
		build: buildinfo.Info{Version: "dev", Commit: "unknown", BuildTime: "unknown"},
	})
	if code != exitError || !strings.Contains(stderr.String(), "write stdout: broken pipe") {
		t.Fatalf("version output failure = stderr %q, exit %d", stderr.String(), code)
	}
}

func TestVersion_Help(t *testing.T) {
	stdout, stderr, code := runForTest(t, []string{"version", "--help"}, nil, buildinfo.Info{})
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "Usage:") ||
		!strings.Contains(stdout, "dot version") || strings.Contains(stdout, "--home") {
		t.Fatalf("version help = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
}

func TestVersion_RejectsEmptyProfile(t *testing.T) {
	_, stderr, code := runForTest(t, []string{"version", "--profile="}, nil, buildinfo.Info{})
	if code != exitError || !strings.Contains(stderr, "--profile must not be empty") {
		t.Fatalf("empty profile = stderr %q, exit %d", stderr, code)
	}
}

func TestRoot_HelpListsSpecifiedCommandsAndFlags(t *testing.T) {
	stdout, stderr, code := runForTest(t, []string{"--help"}, nil, buildinfo.Info{})
	if code != exitOK || stderr != "" {
		t.Fatalf("root help = stderr %q, exit %d", stderr, code)
	}
	for _, want := range []string{"apply", "init", "status", "version", "--repo", "--profile", "--verbose", "--no-color"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("root help = %q, want %q", stdout, want)
		}
	}
	for _, unwanted := range []string{"add", "completion", "diff", "doctor", "--home"} {
		if strings.Contains(stdout, unwanted) {
			t.Errorf("root help = %q, want %q omitted", stdout, unwanted)
		}
	}
}

func TestRoot_GlobalFlags(t *testing.T) {
	root, err := newRootCommand(environment{})
	if err != nil {
		t.Fatalf("newRootCommand() error = %v", err)
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
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			flag := root.PersistentFlags().Lookup(test.name)
			if flag == nil || flag.Shorthand != test.shorthand || flag.Hidden != test.hidden {
				t.Fatalf("global flag %q = %#v", test.name, flag)
			}
		})
	}
}

func TestRoot_RequiresCommand(t *testing.T) {
	stdout, stderr, code := runForTest(t, nil, nil, buildinfo.Info{})
	if code != exitError || !strings.Contains(stdout, "Usage:") ||
		!strings.Contains(stderr, "a command is required") {
		t.Fatalf("empty root = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
}

func runForTest(t *testing.T, args []string, envVars map[string]string, build buildinfo.Info) (string, string, int) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	home := t.TempDir()
	code := run(args, environment{
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
		goos:  runtime.GOOS,
	})
	return stdout.String(), stderr.String(), code
}

type failingWriter struct {
	err error
}

func (writer failingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
