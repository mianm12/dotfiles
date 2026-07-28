package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestFIFOInputsFailBeforeMutation(t *testing.T) {
	for _, input := range []string{
		"machine config",
		"state",
		"root manifest",
		"module manifest",
	} {
		t.Run(input, func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = ["app"]`)
			fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})

			var fifo string
			switch input {
			case "machine config":
				fifo = fixture.config
			case "state":
				fixture.writeMachine(t, []string{"base"}, nil)
				fifo = fixture.state
			case "root manifest":
				fixture.writeMachine(t, []string{"base"}, nil)
				fifo = filepath.Join(fixture.repository, "dot.toml")
				if err := os.Remove(fifo); err != nil {
					t.Fatalf("os.Remove(root manifest) error = %v", err)
				}
			case "module manifest":
				fixture.writeMachine(t, []string{"base"}, nil)
				fifo = filepath.Join(fixture.repository, "modules", "app", "module.toml")
				if err := os.Remove(fifo); err != nil {
					t.Fatalf("os.Remove(module manifest) error = %v", err)
				}
			default:
				t.Fatalf("unsupported input %q", input)
			}
			if err := os.MkdirAll(filepath.Dir(fifo), 0o700); err != nil {
				t.Fatalf("os.MkdirAll(FIFO parent) error = %v", err)
			}
			if err := syscall.Mkfifo(fifo, 0o600); err != nil {
				t.Fatalf("syscall.Mkfifo(%q) error = %v", fifo, err)
			}
			before := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.runProcess("apply")

			if code != exitError ||
				stdout != "" ||
				!strings.Contains(stderr, "regular file") {
				t.Fatalf(
					"apply with %s FIFO = (%d, %q, %q), want fast input error",
					input,
					code,
					stdout,
					stderr,
				)
			}
			assertSnapshotUnchanged(t, before)
		})
	}
}

func TestScopedDryRunDefersOutOfScopeFIFOManifest(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["good"]`)
	fixture.writeModule(t, "good", `
[[links]]
id = "config"
source = "config"
target = "~/.good"
`, map[string]string{"config": "good"})
	badRoot := filepath.Join(fixture.repository, "modules", "bad")
	if err := os.MkdirAll(badRoot, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(bad module) error = %v", err)
	}
	if err := syscall.Mkfifo(filepath.Join(badRoot, "module.toml"), 0o600); err != nil {
		t.Fatalf("syscall.Mkfifo(bad manifest) error = %v", err)
	}
	fixture.writeMachine(t, []string{"base"}, nil)
	before := snapshotTree(t, fixture.root)

	code, _, stderr := fixture.runProcess("apply", "good", "--dry-run")
	if code != exitOK {
		t.Fatalf("scoped good dry-run = (%d, %q), want success", code, stderr)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr := fixture.runProcess("apply", "bad", "--dry-run")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "regular file") {
		t.Fatalf(
			"scoped bad dry-run = (%d, %q, %q), want fast manifest error",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
}

func TestReadOnlyCommandsDoNotRewriteEmptyStateModules(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeMachine(t, []string{"base"}, nil)
	writeCLIFile(t, fixture.state, fmt.Sprintf(
		`{"version":2,"home":%q,"modules":{"empty":{"placements":{}}}}`,
		fixture.home,
	))

	for _, args := range [][]string{
		{"status"},
		{"apply", "--dry-run"},
	} {
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.runInjected(args...)

		if code != exitOK || stderr != "" || strings.Contains(stdout, "empty") {
			t.Fatalf(
				"%v = (%d, %q, %q), want read-only in-memory cleanup",
				args,
				code,
				stdout,
				stderr,
			)
		}
		assertSnapshotUnchanged(t, before)
	}

	code, stdout, stderr := fixture.runInjected("apply")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(
			stdout,
			"selection_changed=false targets_changed=false state_changed=true",
		) {
		t.Fatalf("apply canonical rewrite = (%d, %q, %q)", code, stdout, stderr)
	}
	loaded, err := state.Load(fixture.state, fixture.home)
	if err != nil {
		t.Fatalf("state.Load(canonical) error = %v", err)
	}
	if loaded.NeedsRewrite || len(loaded.Snapshot.Modules) != 0 {
		t.Fatalf("canonical loaded state = %#v", loaded)
	}
	assertApplyNoMutation(t, fixture, fixture.runInjected)
}
