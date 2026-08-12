package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
)

func TestSelectAddChangesOnlyMachineConfig(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
	fixture.writeMachine(t, []string{"base"}, nil)

	code, stdout, stderr := fixture.run("select", "add", "extra")
	if code != exitOK || stderr != "" ||
		!strings.Contains(stdout, "selection_changed=true") ||
		!strings.Contains(stdout, "run dot apply") {
		t.Fatalf("select add = (%d, %q, %q)", code, stdout, stderr)
	}
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 1 || extras[0] != "extra" {
		t.Fatalf("extra_modules = %v, want [extra]", extras)
	}
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, filepath.Join(fixture.home, ".extra"))

	code, stdout, stderr = fixture.run("select", "add", "extra")
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "selection_changed=false") {
		t.Fatalf("repeated select add = (%d, %q, %q)", code, stdout, stderr)
	}
	code, _, stderr = fixture.run("apply")
	if code != exitOK {
		t.Fatalf("apply selected extra = (%d, %q)", code, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".extra"),
		filepath.Join(fixture.repository, "modules", "extra", "config"),
	)
}

func TestSelectAddRejectsUnknownAndUnavailableModulesWithoutMutation(t *testing.T) {
	tests := []struct {
		name     string
		moduleID string
		manifest string
		platform config.Platform
		want     string
	}{
		{name: "unknown", moduleID: "missing", want: "unknown module"},
		{
			name:     "not applicable",
			moduleID: "gated",
			manifest: "[match]\nos = [\"macos\"]",
			platform: cliTestPlatform("linux", "ubuntu", "x86_64"),
			want:     "not applicable",
		},
		{
			name:     "indeterminate",
			moduleID: "gated",
			manifest: "[match]\ndistro = [\"ubuntu\"]\nos = [\"linux\"]",
			platform: cliIndeterminateLinuxPlatform("distribution unavailable"),
			want:     "indeterminate",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLITestEnv(t, "base = []")
			if test.manifest != "" {
				fixture.writeModule(t, test.moduleID, test.manifest, nil)
			}
			fixture.writeMachine(t, []string{"base"}, nil)
			if test.platform.OS.Value != "" {
				fixture.env.platform = func() config.Platform { return test.platform }
			}
			before := snapshotTree(t, fixture.root)

			code, stdout, stderr := fixture.runInjected("select", "add", test.moduleID)
			if code != exitError || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("select add = (%d, %q, %q), want %q", code, stdout, stderr, test.want)
			}
			assertSnapshotUnchanged(t, before)
		})
	}
}

func TestSelectRemoveLeavesArtifactsForNextApply(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
	fixture.writeMachine(t, []string{"base"}, []string{"extra"})
	if code, _, stderr := fixture.run("apply"); code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	target := filepath.Join(fixture.home, ".extra")
	stateBefore := snapshotPaths(t, fixture.state, target)

	code, stdout, stderr := fixture.run("select", "remove", "extra")
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "selection_changed=true") {
		t.Fatalf("select remove = (%d, %q, %q)", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, stateBefore)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want empty", extras)
	}

	if code, _, stderr := fixture.run("apply"); code != exitOK {
		t.Fatalf("cleanup apply = (%d, %q)", code, stderr)
	}
	assertCLIMissing(t, target)
	if records := loadTestState(t, fixture).Links; len(records) != 0 {
		t.Fatalf("state records = %#v, want empty", records)
	}
}

func TestSelectRemoveAllowsDeletedMalformedAndProfileSelectedModules(t *testing.T) {
	t.Run("deleted direct module", func(t *testing.T) {
		fixture := newCLITestEnv(t, "base = []")
		fixture.writeMachine(t, []string{"base"}, []string{"gone"})
		code, _, stderr := fixture.run("select", "remove", "gone")
		if code != exitOK || stderr != "" {
			t.Fatalf("select remove gone = (%d, %q)", code, stderr)
		}
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("extra_modules = %v, want empty", extras)
		}
	})

	t.Run("malformed direct module", func(t *testing.T) {
		fixture := newCLITestEnv(t, "base = []")
		fixture.writeModule(t, "broken", "unknown = true", nil)
		fixture.writeMachine(t, []string{"base"}, []string{"broken"})
		code, _, stderr := fixture.run("select", "remove", "broken")
		if code != exitOK || stderr != "" {
			t.Fatalf("select remove broken = (%d, %q)", code, stderr)
		}
	})

	t.Run("profile remains active", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", "", nil)
		fixture.writeMachine(t, []string{"base"}, []string{"app"})
		code, stdout, stderr := fixture.run("select", "remove", "app")
		if code != exitOK || stderr != "" || !strings.Contains(stdout, "remains selected") {
			t.Fatalf("select remove profile module = (%d, %q, %q)", code, stdout, stderr)
		}
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("extra_modules = %v, want empty", extras)
		}
	})
}

func TestSelectRemoveMissingDirectSelectionIsNoop(t *testing.T) {
	fixture := newCLITestEnv(t, "base = []")
	fixture.writeMachine(t, []string{"base"}, nil)
	before := snapshotPaths(t, fixture.config)

	for range 2 {
		code, stdout, stderr := fixture.run("select", "remove", "anything")
		if code != exitOK || stderr != "" || !strings.Contains(stdout, "selection_changed=false") {
			t.Fatalf("select remove absent = (%d, %q, %q)", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
	}
}
