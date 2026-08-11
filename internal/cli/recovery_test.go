package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestApplyConvergesAfterInterruptedFacts(t *testing.T) {
	t.Run("selection persisted before artifacts", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
		fixture.writeMachine(t, []string{"base"}, []string{"extra"})

		code, _, stderr := fixture.run("apply")
		if code != exitOK || stderr == "" {
			t.Fatalf("recovery apply = (%d, %q)", code, stderr)
		}
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".extra"),
			filepath.Join(fixture.repository, "modules", "extra", "config"),
		)
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("link created before state commit", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "config"})
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".app")
		destination := filepath.Join(fixture.repository, "modules", "app", "config")
		if err := os.Symlink(destination, target); err != nil {
			t.Fatalf("os.Symlink(interrupted link) error = %v", err)
		}

		code, stdout, stderr := fixture.run("apply")
		if code != exitOK ||
			!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
			stderr == "" {
			t.Fatalf("recovery apply = (%d, %q, %q)", code, stdout, stderr)
		}
		assertCLILink(t, target, destination)
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("local published before state commit", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[locals]]
id = "local"
example = "local.example"
target = "~/.app.local"
`, map[string]string{"local.example": "example"})
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".app.local")
		writeCLIFile(t, target, "personal")

		code, stdout, stderr := fixture.run("apply")
		if code != exitOK ||
			!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
			stderr == "" {
			t.Fatalf("recovery apply = (%d, %q, %q)", code, stdout, stderr)
		}
		if record := loadTestState(t, fixture).Modules["app"].Placements["local"]; record.Kind != state.KindLocal {
			t.Fatalf("local state record = %#v, want local provenance", record)
		}
		if data, err := os.ReadFile(target); err != nil || string(data) != "personal" {
			t.Fatalf("local = (%q, %v), want preserved personal bytes", data, err)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("updated link before state commit", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "new"
target = "~/.app"
`, map[string]string{
			"old": "old",
			"new": "new",
		})
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".app")
		oldDestination := filepath.Join(fixture.repository, "modules", "app", "old")
		newDestination := filepath.Join(fixture.repository, "modules", "app", "new")
		if err := os.Symlink(newDestination, target); err != nil {
			t.Fatalf("os.Symlink(updated link) error = %v", err)
		}
		writeLinkState(t, fixture, target, oldDestination)

		code, stdout, stderr := fixture.run("apply")
		if code != exitOK ||
			!strings.Contains(stdout, "targets_changed=false state_changed=true") ||
			stderr != "" {
			t.Fatalf("repair-state apply = (%d, %q, %q)", code, stdout, stderr)
		}
		record := loadTestState(t, fixture).Modules["app"].Placements["config"]
		if record.LinkDestination != newDestination {
			t.Fatalf("state destination = %q, want %q", record.LinkDestination, newDestination)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("old link deleted during update", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "new"
target = "~/.app"
`, map[string]string{
			"old": "old",
			"new": "new",
		})
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".app")
		oldDestination := filepath.Join(fixture.repository, "modules", "app", "old")
		writeLinkState(t, fixture, target, oldDestination)

		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("recovery apply = (%d, %q)", code, stderr)
		}
		assertCLILink(
			t,
			target,
			filepath.Join(fixture.repository, "modules", "app", "new"),
		)
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("prune completed before state commit", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", "", map[string]string{"old": "old"})
		fixture.writeMachine(t, []string{"base"}, nil)
		target := filepath.Join(fixture.home, ".old")
		oldDestination := filepath.Join(fixture.repository, "modules", "app", "old")
		writeLinkState(t, fixture, target, oldDestination)

		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("recovery apply = (%d, %q)", code, stderr)
		}
		if modules := loadTestState(t, fixture).Modules; len(modules) != 0 {
			t.Fatalf("state modules = %#v, want stale record forgotten", modules)
		}
		assertApplyNoMutation(t, fixture, fixture.run)
	})
}

func TestApplyUsesTwoStagesForParentUpdateAndTraversedStaleCleanup(
	t *testing.T,
) {
	topology := newParentUpdateStaleCLIEnv(t, "old")
	fixture := topology.fixture

	code, _, stderr := fixture.run("apply")
	if code != exitOK || stderr != "" {
		t.Fatalf("stage-one apply = (%d, %q), want stale cleanup", code, stderr)
	}
	assertCLILink(t, topology.parentTarget, topology.oldSource)
	assertCLIMissing(t, topology.staleActual)
	if _, exists := loadTestState(t, fixture).Modules["stale"]; exists {
		t.Fatal("stage-one apply retained stale ownership")
	}
	assertApplyNoMutation(t, fixture, fixture.run)

	writeModuleManifest(t, fixture, "parent", `
[[links]]
id = "tree"
source = "new"
target = "~/owned"
`)
	code, _, stderr = fixture.run("apply")
	if code != exitOK || stderr != "" {
		t.Fatalf("stage-two apply = (%d, %q), want parent update", code, stderr)
	}
	assertCLILink(t, topology.parentTarget, topology.newSource)
	assertApplyNoMutation(t, fixture, fixture.run)
}

func TestApplyPrunesTraversedStaleLinksChildFirst(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeMachine(t, []string{"base"}, nil)
	parentSource := filepath.Join(fixture.root, "old-repository", "tree")
	outside := filepath.Join(fixture.root, "outside")
	for _, directory := range []string{parentSource, outside} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(parentSource, "out")); err != nil {
		t.Fatalf("os.Symlink(parent internal link) error = %v", err)
	}
	parentTarget := filepath.Join(fixture.home, "owned")
	if err := os.Symlink(parentSource, parentTarget); err != nil {
		t.Fatalf("os.Symlink(parent target) error = %v", err)
	}
	access := filepath.Join(fixture.home, "access")
	if err := os.Symlink(filepath.Join(parentSource, "out"), access); err != nil {
		t.Fatalf("os.Symlink(initial access) error = %v", err)
	}
	childSource := filepath.Join(fixture.root, "old-repository", "child")
	writeCLIFile(t, childSource, "stale")
	childActual := filepath.Join(outside, "child")
	if err := os.Symlink(childSource, childActual); err != nil {
		t.Fatalf("os.Symlink(child target) error = %v", err)
	}
	childTarget := filepath.Join(access, "child")
	resolvedParent, err := corepaths.ResolveAbsoluteTarget(
		fixture.home,
		parentTarget,
	)
	if err != nil {
		t.Fatalf("ResolveAbsoluteTarget(parent) error = %v", err)
	}
	resolvedChild, err := corepaths.ResolveAbsoluteTarget(
		fixture.home,
		childTarget,
	)
	if err != nil {
		t.Fatalf("ResolveAbsoluteTarget(child) error = %v", err)
	}
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Modules: map[string]state.Module{
			"stale": {Placements: map[string]state.Placement{
				"a-parent": {
					Kind:            state.KindLink,
					Target:          parentTarget,
					ResolvedTarget:  resolvedParent.Resolved(),
					LinkDestination: parentSource,
				},
				"z-child": {
					Kind:            state.KindLink,
					Target:          childTarget,
					ResolvedTarget:  resolvedChild.Resolved(),
					LinkDestination: childSource,
				},
			}},
		},
	})
	if err := os.Remove(access); err != nil {
		t.Fatalf("os.Remove(access) error = %v", err)
	}
	if err := os.Symlink(filepath.Join(parentTarget, "out"), access); err != nil {
		t.Fatalf("os.Symlink(rebound access) error = %v", err)
	}
	if current, err := corepaths.ResolveAbsoluteTarget(
		fixture.home,
		childTarget,
	); err != nil || current.Resolved() != resolvedChild.Resolved() {
		t.Fatalf(
			"ResolveAbsoluteTarget(rebound child) = (%#v, %v), want %q",
			current,
			err,
			resolvedChild.Resolved(),
		)
	}

	code, _, stderr := fixture.run("apply")
	if code != exitOK || stderr != "" {
		t.Fatalf("apply = (%d, %q), want ordered stale cleanup", code, stderr)
	}
	assertCLIMissing(t, childActual)
	assertCLIMissing(t, parentTarget)
	if modules := loadTestState(t, fixture).Modules; len(modules) != 0 {
		t.Fatalf("state modules = %#v, want both stale records removed", modules)
	}
	assertApplyNoMutation(t, fixture, fixture.run)
}

func TestDuplicateStaleOwnershipAcrossStatusDryRunAndApply(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeMachine(t, []string{"base"}, nil)
	realParent := filepath.Join(fixture.root, "targets")
	if err := os.MkdirAll(realParent, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", realParent, err)
	}
	firstAlias := filepath.Join(fixture.home, "first")
	secondAlias := filepath.Join(fixture.home, "second")
	for _, alias := range []string{firstAlias, secondAlias} {
		if err := os.Symlink(realParent, alias); err != nil {
			t.Fatalf("os.Symlink(%q) error = %v", alias, err)
		}
	}
	source := filepath.Join(fixture.root, "old-repository", "config")
	writeCLIFile(t, source, "stale")
	actual := filepath.Join(realParent, "config")
	if err := os.Symlink(source, actual); err != nil {
		t.Fatalf("os.Symlink(actual) error = %v", err)
	}
	firstTarget := filepath.Join(firstAlias, "config")
	secondTarget := filepath.Join(secondAlias, "config")
	resolved, err := corepaths.ResolveAbsoluteTarget(fixture.home, firstTarget)
	if err != nil {
		t.Fatalf("ResolveAbsoluteTarget(first) error = %v", err)
	}
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Modules: map[string]state.Module{
			"stale-a": {Placements: map[string]state.Placement{
				"config": {
					Kind:            state.KindLink,
					Target:          firstTarget,
					ResolvedTarget:  resolved.Resolved(),
					LinkDestination: source,
				},
			}},
			"stale-b": {Placements: map[string]state.Placement{
				"config": {
					Kind:            state.KindLink,
					Target:          secondTarget,
					ResolvedTarget:  resolved.Resolved(),
					LinkDestination: source,
				},
			}},
		},
	})
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("status")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "forget") ||
		!strings.Contains(stdout, "shares ownership") {
		t.Fatalf(
			"status = (%d, %q, %q), want cross-module duplicate ownership",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.run("apply", "--dry-run")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "prune") ||
		!strings.Contains(stdout, "forget") ||
		!strings.Contains(stdout, "shares ownership") {
		t.Fatalf(
			"apply dry-run = (%d, %q, %q), want one prune and one forget",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)

	code, _, stderr = fixture.run("apply")
	if code != exitOK ||
		!strings.Contains(stderr, "forgot ownership") ||
		!strings.Contains(stderr, "shares ownership") {
		t.Fatalf("apply = (%d, %q), want duplicate cleanup", code, stderr)
	}
	assertCLIMissing(t, actual)
	if modules := loadTestState(t, fixture).Modules; len(modules) != 0 {
		t.Fatalf("state modules = %#v, want duplicate records removed", modules)
	}
	assertApplyNoMutation(t, fixture, fixture.run)
}

func TestMutationOutputFailureAdvisesRerun(t *testing.T) {
	t.Run("init", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})

		stderr := runWithFailedStdout(
			t,
			[]string{"init", fixture.repository, "--profile", "base"},
		)
		if !strings.Contains(stderr, "selection may already be updated") ||
			!strings.Contains(stderr, "rerun dot init") {
			t.Fatalf("stderr = %q, want init selection recovery advice", stderr)
		}
		target := filepath.Join(fixture.home, ".app")
		assertCLIMissing(t, target)
		if code, _, applyErr := fixture.run("apply"); code != exitOK {
			t.Fatalf("apply after init output failure = (%d, %q)", code, applyErr)
		}
		assertCLILink(t, target, filepath.Join(fixture.repository, "modules", "app", "config"))
	})

	t.Run("apply", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
		fixture.writeMachine(t, []string{"base"}, nil)

		stderr := runWithFailedStdout(t, []string{"apply"})
		assertOutputFailure(t, stderr, "dot apply")
		target := filepath.Join(fixture.home, ".app")
		assertCLILink(t, target, filepath.Join(fixture.repository, "modules", "app", "config"))
		assertApplyNoMutation(t, fixture, fixture.run)
	})

	t.Run("select remove", func(t *testing.T) {
		fixture := newCLITestEnv(t, "base = []")
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
		fixture.writeMachine(t, []string{"base"}, []string{"app"})
		code, _, stderr := fixture.run("apply")
		if code != exitOK {
			t.Fatalf("initial apply = (%d, %q)", code, stderr)
		}

		stderr = runWithFailedStdout(t, []string{"select", "remove", "app"})
		if !strings.Contains(stderr, "selection may already be updated") ||
			!strings.Contains(stderr, "rerun dot select remove app") {
			t.Fatalf("stderr = %q, want select remove recovery advice", stderr)
		}
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".app"),
			filepath.Join(fixture.repository, "modules", "app", "config"),
		)
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("extra_modules = %v, want empty", extras)
		}

		code, stdout, stderr := fixture.run("apply")
		if code != exitOK || stderr != "" {
			t.Fatalf("recovery apply = (%d, %q, %q)", code, stdout, stderr)
		}
		assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
	})
}

func TestScopedApplyFailureAdvisesScopedRerun(t *testing.T) {
	newFixture := func(t *testing.T) *cliTestEnv {
		t.Helper()
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "portable"})
		fixture.writeMachine(t, []string{"base"}, []string{"extra"})
		return fixture
	}

	t.Run("result output failure", func(t *testing.T) {
		fixture := newFixture(t)

		stderr := runWithFailedStdout(t, []string{"apply", "extra"})

		assertOutputFailure(t, stderr, "dot apply extra")
		assertCLILink(
			t,
			filepath.Join(fixture.home, ".extra"),
			filepath.Join(fixture.repository, "modules", "extra", "config"),
		)
	})
}
