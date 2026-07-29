package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusReportsExistingLocalWithoutProvenanceAsPending(t *testing.T) {
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

	beforeStatus := snapshotPaths(t, fixture.config, target)
	code, stdout, stderr := fixture.run("status")
	if code != exitOK ||
		!strings.Contains(stdout, "app  pending") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf("status before provenance = (%d, %q, %q), want pending", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, beforeStatus)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)

	code, stdout, stderr = fixture.run("apply")
	if code != exitOK ||
		!strings.Contains(stdout, "state_changed=true") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf("apply local provenance = (%d, %q, %q), want state-only mutation", code, stdout, stderr)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "personal" {
		t.Fatalf("local after apply = (%q, %v), want preserved", data, err)
	}

	beforeRepeat := snapshotPaths(t, fixture.config, fixture.state, fixture.lock, target)
	code, stdout, stderr = fixture.run("status")
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "app  converged") {
		t.Fatalf("status after provenance = (%d, %q, %q), want converged", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, beforeRepeat)
	assertApplyNoMutation(t, fixture, fixture.run)
}

func TestStatusReportsLinkStateRefreshAsPending(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/current/config"
`, map[string]string{"config": "portable"})
	fixture.writeMachine(t, []string{"base"}, nil)

	physicalA := filepath.Join(fixture.home, "physical-a")
	physicalB := filepath.Join(fixture.home, "physical-b")
	for _, directory := range []string{physicalA, physicalB} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", directory, err)
		}
	}
	parent := filepath.Join(fixture.home, "current")
	if err := os.Symlink(physicalA, parent); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", physicalA, parent, err)
	}

	code, _, stderr := fixture.run("apply")
	if code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	destination := filepath.Join(fixture.repository, "modules", "app", "config")
	oldTarget := filepath.Join(physicalA, "config")
	assertCLILink(t, oldTarget, destination)

	if err := os.Remove(parent); err != nil {
		t.Fatalf("os.Remove(%q) error = %v", parent, err)
	}
	if err := os.Symlink(physicalB, parent); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", physicalB, parent, err)
	}
	newTarget := filepath.Join(physicalB, "config")
	if err := os.Symlink(destination, newTarget); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", destination, newTarget, err)
	}

	beforeStatus := snapshotPaths(
		t,
		fixture.config,
		fixture.state,
		fixture.lock,
		parent,
		oldTarget,
		newTarget,
	)
	code, stdout, stderr := fixture.run("status")
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "app  pending") {
		t.Fatalf("status before state refresh = (%d, %q, %q), want pending", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, beforeStatus)

	code, stdout, stderr = fixture.run("apply")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "targets_changed=false state_changed=true") {
		t.Fatalf("state refresh apply = (%d, %q, %q)", code, stdout, stderr)
	}
	assertApplyNoMutation(t, fixture, fixture.run)
}

func TestStatusReportsPathConflictWithoutMutation(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["first", "second", "pending"]`)
	fixture.writeModule(t, "first", `
[[links]]
id = "config"
source = "config"
target = "~/.same"
`, map[string]string{"config": "first"})
	fixture.writeModule(t, "second", `
[[links]]
id = "config"
source = "config"
target = "~/.same"
`, map[string]string{"config": "second"})
	fixture.writeModule(t, "pending", `
[[links]]
id = "config"
source = "config"
target = "~/.pending"
`, map[string]string{"config": "pending"})
	fixture.writeMachine(t, []string{"base"}, nil)
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("status")
	if code != exitOK ||
		!strings.Contains(stdout, "first  conflict") ||
		!strings.Contains(stdout, "second  conflict") ||
		!strings.Contains(stdout, "pending  pending") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"status = (%d, %q, %q), want conflict plus independent pending status",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
}

func TestStatusReportsCrossModuleUpdatePruneConflict(t *testing.T) {
	topology := newParentUpdateStaleCLIEnv(t, "new")
	fixture := topology.fixture
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("status")

	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "stale  conflict") ||
		!strings.Contains(
			stdout,
			"cleanup would be invalidated by active link update",
		) {
		t.Fatalf(
			"status = (%d, %q, %q), want cross-module update/prune conflict",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLILink(t, topology.parentTarget, topology.oldSource)
	assertCLILink(t, topology.staleActual, topology.staleSource)
	assertCLIMissing(t, fixture.lock)
}

func TestStatusDelaysInactiveMalformedManifest(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "modules", "broken", "module.toml"),
		"unknown = true\n",
	)
	fixture.writeMachine(t, []string{"base"}, nil)
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("status")
	if code != exitOK ||
		!strings.Contains(stdout, "broken  inactive") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"status = (%d, %q, %q), want unloaded inactive module",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.run("status", "broken")
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, `module "broken"`) {
		t.Fatalf(
			"status broken = (%d, %q, %q), want strict manifest failure",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
}

func TestStatusReportsEveryPlacementConflict(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "first"
source = "first"
target = "~/.first"

[[links]]
id = "second"
source = "second"
target = "~/.second"
`, map[string]string{
		"first":  "first",
		"second": "second",
	})
	fixture.writeMachine(t, []string{"base"}, nil)
	first := filepath.Join(fixture.home, ".first")
	second := filepath.Join(fixture.home, ".second")
	writeCLIFile(t, first, "personal")
	writeCLIFile(t, second, "personal")
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.run("status", "app")

	if code != exitOK ||
		!strings.Contains(stdout, "app  conflict selection=profile\n") ||
		!strings.Contains(stdout, "conflict     app/first "+first) ||
		!strings.Contains(stdout, "conflict     app/second "+second) ||
		strings.Count(stdout, `reason="actual target is regular file"`) != 2 ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"status placement conflicts = (%d, %q, %q), want both concrete conflicts",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
}

func TestStatusShowsNamedPortableVariant(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[variants.portable]
root = "."

[[variants.portable.links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "app"})
	fixture.writeMachine(t, []string{"base"}, nil)
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.runInjected("status", "app")

	if code != exitOK ||
		!strings.Contains(stdout, "app  pending selection=profile variant=portable\n") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"status named portable variant = (%d, %q, %q), want explicit variant",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
}
