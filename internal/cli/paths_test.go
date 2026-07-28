package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPathsPrintsDerivedPathsWithoutMutation(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, *cliTestEnv)
	}{
		{name: "uninitialized"},
		{
			name: "corrupt config and state",
			setup: func(t *testing.T, fixture *cliTestEnv) {
				writeCLIFile(t, fixture.config, "not toml")
				writeCLIFile(t, fixture.state, "not json")
			},
		},
		{
			name: "unusual control entries",
			setup: func(t *testing.T, fixture *cliTestEnv) {
				for _, parent := range []string{
					filepath.Dir(fixture.config),
					filepath.Dir(fixture.state),
				} {
					if err := os.MkdirAll(parent, 0o700); err != nil {
						t.Fatalf("os.MkdirAll(%q) error = %v", parent, err)
					}
				}
				if err := os.Symlink("missing-config", fixture.config); err != nil {
					t.Fatalf("os.Symlink(config) error = %v", err)
				}
				if err := os.Symlink("missing-state", fixture.state); err != nil {
					t.Fatalf("os.Symlink(state) error = %v", err)
				}
				if err := os.Mkdir(fixture.lock, 0o700); err != nil {
					t.Fatalf("os.Mkdir(lock) error = %v", err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLITestEnv(t, "")
			if test.setup != nil {
				test.setup(t, fixture)
			}
			fixture.env.platform = nil
			fixture.env.getwd = nil
			before := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.runInjected("paths")

			want := fmt.Sprintf(
				"home=%s\nmachine_config=%s\nstate=%s\nlock=%s\n",
				fixture.home,
				fixture.config,
				fixture.state,
				fixture.lock,
			)
			if code != exitOK || stdout != want || stderr != "" {
				t.Fatalf(
					"paths = (%d, %q, %q), want (%d, %q, \"\")",
					code,
					stdout,
					stderr,
					exitOK,
					want,
				)
			}
			assertSnapshotUnchanged(t, before)
		})
	}
}

func TestPathsRejectsArgumentsAndInvalidHome(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "argument",
			args: []string{"paths", "extra"},
			want: "error: dot paths accepts no arguments\n",
		},
		{
			name: "unknown flag",
			args: []string{"paths", "--unknown"},
			want: "error: unknown flag: --unknown\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLITestEnv(t, "")
			fixture.env.userHomeDir = func() (string, error) {
				t.Fatal("HOME resolver called for invalid paths arguments")
				return "", nil
			}
			code, stdout, stderr := fixture.runInjected(test.args...)
			if code != exitUsage ||
				stdout != "" ||
				stderr != test.want {
				t.Fatalf(
					"run(%q) = (%d, %q, %q), want usage failure %q",
					test.args,
					code,
					stdout,
					stderr,
					test.want,
				)
			}
		})
	}

	for _, test := range []struct {
		name string
		home func() (string, error)
		want string
	}{
		{
			name: "missing resolver",
			want: "error: HOME resolver is unavailable\n",
		},
		{
			name: "resolver error",
			home: func() (string, error) {
				return "", errors.New("synthetic HOME failure")
			},
			want: "error: resolve current user HOME: synthetic HOME failure\n",
		},
		{
			name: "empty",
			home: func() (string, error) {
				return "", nil
			},
			want: "error: current user HOME must be a non-empty absolute path\n",
		},
		{
			name: "relative",
			home: func() (string, error) {
				return "relative", nil
			},
			want: "error: current user HOME must be a non-empty absolute path\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{"paths"}, environment{
				stdout:      &stdout,
				stderr:      &stderr,
				userHomeDir: test.home,
			})
			if code != exitError ||
				stdout.String() != "" ||
				stderr.String() != test.want {
				t.Fatalf(
					"paths = (%d, %q, %q), want runtime failure %q",
					code,
					stdout.String(),
					stderr.String(),
					test.want,
				)
			}
		})
	}
}

func TestPathsReportsStdoutFailure(t *testing.T) {
	fixture := newCLITestEnv(t, "")
	before := snapshotTree(t, fixture.root)
	var stderr bytes.Buffer
	env := fixture.env
	env.stdout = failedWriter{}
	env.stderr = &stderr
	env.platform = nil

	code := run([]string{"paths"}, env)

	want := "error: write stdout: synthetic stdout failure\n"
	if code != exitError || stderr.String() != want {
		t.Fatalf(
			"paths stdout failure = (%d, %q), want (%d, %q)",
			code,
			stderr.String(),
			exitError,
			want,
		)
	}
	assertSnapshotUnchanged(t, before)
}

func TestPathsUsesCleanAbsoluteHome(t *testing.T) {
	fixture := newCLITestEnv(t, "")
	separator := string(filepath.Separator)
	home := fixture.home + separator + "." + separator + "nested" + separator + ".."
	fixture.env.userHomeDir = func() (string, error) {
		return home, nil
	}
	fixture.env.platform = nil

	code, stdout, stderr := fixture.runInjected("paths")

	cleanHome := filepath.Clean(home)
	want := fmt.Sprintf(
		"home=%s\nmachine_config=%s\nstate=%s\nlock=%s\n",
		cleanHome,
		filepath.Join(cleanHome, ".config", "dot", "config.toml"),
		filepath.Join(cleanHome, ".local", "state", "dot", "state.json"),
		filepath.Join(cleanHome, ".local", "state", "dot", "lock"),
	)
	if code != exitOK || stdout != want || stderr != "" {
		t.Fatalf(
			"paths clean HOME = (%d, %q, %q), want (%d, %q, \"\")",
			code,
			stdout,
			stderr,
			exitOK,
			want,
		)
	}
}
