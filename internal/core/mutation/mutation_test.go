package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	mutationlock "github.com/mianm12/dotfiles/internal/core/mutation/internal/lock"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/storage"
)

type mutationFixture struct {
	root       string
	home       string
	repository string
	controls   corepaths.Controls
}

func newMutationFixture(t *testing.T, profiles string) mutationFixture {
	t.Helper()
	root := t.TempDir()
	fixture := mutationFixture{
		root:       root,
		home:       filepath.Join(root, "home"),
		repository: filepath.Join(root, "repository"),
	}
	fixture.controls = corepaths.Controls{
		Repository: fixture.repository,
		Config:     filepath.Join(root, "config", "machine.toml"),
		State:      filepath.Join(root, "state", "state.json"),
		Lock:       filepath.Join(root, "state", "mutation.lock"),
	}
	for _, directory := range []string{fixture.home, fixture.repository} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}
	writeMutationFile(
		t,
		filepath.Join(fixture.repository, "dot.toml"),
		"version = 1\n[profiles]\n"+profiles+"\n",
	)
	return fixture
}

func (fixture mutationFixture) machine(profiles, extras []string) config.Machine {
	return config.Machine{
		Version:      1,
		Repository:   fixture.repository,
		Profiles:     append([]string(nil), profiles...),
		ExtraModules: append([]string(nil), extras...),
	}
}

func publishMutationMachine(t *testing.T, path string, machine config.Machine) {
	t.Helper()
	data, err := config.MarshalMachine(machine)
	if err != nil {
		t.Fatalf("MarshalMachine() error = %v", err)
	}
	if _, err := storage.PublishPrivateFile(path, data); err != nil {
		t.Fatalf("PublishPrivateFile(machine) error = %v", err)
	}
}

func TestUpdateSelectionOwnsConfigOnlyMutation(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	machine := fixture.machine([]string{"base"}, nil)
	request := SelectionRequest{
		Home:      fixture.home,
		Controls:  fixture.controls,
		Operation: SelectionInitialize,
		Machine:   machine,
	}

	result, err := UpdateSelection(request)
	if err != nil || !result.Changed {
		t.Fatalf("UpdateSelection(init) = (%#v, %v), want changed", result, err)
	}
	if _, err := os.Lstat(fixture.controls.State); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state error = %v, want missing", err)
	}
	if entries, err := os.ReadDir(fixture.home); err != nil || len(entries) != 0 {
		t.Fatalf("HOME entries = (%v, %v), want empty", entries, err)
	}

	addRequest := SelectionRequest{
		Home:      fixture.home,
		Controls:  fixture.controls,
		Operation: SelectionAdd,
		Machine:   machine,
		ModuleID:  "app",
		Platform:  testPlatform(),
	}
	writeMutationFile(t, filepath.Join(fixture.repository, "modules", "app", "module.toml"), "")
	result, err = UpdateSelection(addRequest)
	if err != nil || !result.Changed || len(result.Machine.ExtraModules) != 1 {
		t.Fatalf("UpdateSelection(add) = (%#v, %v), want app selected", result, err)
	}
	addRequest.Machine = result.Machine
	repeated, err := UpdateSelection(addRequest)
	if err != nil || repeated.Changed {
		t.Fatalf("UpdateSelection(repeat) = (%#v, %v), want no-op", repeated, err)
	}
}

func TestUpdateSelectionRejectsInvalidInitBeforeLockBookkeeping(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	machine := fixture.machine([]string{"base"}, nil)
	machine.Version = 2

	result, err := UpdateSelection(SelectionRequest{
		Home:      fixture.home,
		Controls:  fixture.controls,
		Operation: SelectionInitialize,
		Machine:   machine,
	})
	if err == nil || result.Changed {
		t.Fatalf("UpdateSelection(invalid init) = (%#v, %v), want failure", result, err)
	}
	for _, path := range []string{fixture.controls.Config, fixture.controls.State, fixture.controls.Lock} {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("control path %q error = %v, want missing", path, statErr)
		}
	}
}

func TestUpdateSelectionRemoveDoesNotDecodeTargetManifest(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	machine := fixture.machine([]string{"base"}, []string{"broken"})
	publishMutationMachine(t, fixture.controls.Config, machine)
	writeMutationFile(
		t,
		filepath.Join(fixture.repository, "modules", "broken", "module.toml"),
		"unknown = true\n",
	)

	result, err := UpdateSelection(SelectionRequest{
		Home:      fixture.home,
		Controls:  fixture.controls,
		Operation: SelectionRemove,
		Machine:   machine,
		ModuleID:  "broken",
	})
	if err != nil || !result.Changed || len(result.Machine.ExtraModules) != 0 {
		t.Fatalf("UpdateSelection(remove malformed) = (%#v, %v)", result, err)
	}
}

func TestApplyResolvesInsideOwnedLockAndConverges(t *testing.T) {
	fixture := newMutationFixture(t, `base = ["app"]`)
	machine := fixture.machine([]string{"base"}, nil)
	publishMutationMachine(t, fixture.controls.Config, machine)
	writeMutationFile(t, filepath.Join(fixture.repository, "modules", "app", "module.toml"), `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`)
	source := filepath.Join(fixture.repository, "modules", "app", "config")
	writeMutationFile(t, source, "portable")
	request := ApplyRequest{
		Home:     fixture.home,
		Controls: fixture.controls,
		Machine:  machine,
		Platform: testPlatform(),
	}

	first, err := Apply(request)
	if err != nil || !first.TargetsChanged || !first.StateChanged {
		t.Fatalf("Apply(first) = (%#v, %v)", first, err)
	}
	if destination, err := os.Readlink(filepath.Join(fixture.home, ".app")); err != nil || destination != source {
		t.Fatalf("managed link = (%q, %v), want %q", destination, err, source)
	}
	second, err := Apply(request)
	if err != nil || second.TargetsChanged || second.StateChanged {
		t.Fatalf("Apply(second) = (%#v, %v), want no-op", second, err)
	}
}

func TestApplyRejectsOrdinaryFileBeforeLockBookkeeping(t *testing.T) {
	fixture := newMutationFixture(t, `base = ["app"]`)
	machine := fixture.machine([]string{"base"}, nil)
	publishMutationMachine(t, fixture.controls.Config, machine)
	writeMutationFile(t, filepath.Join(fixture.repository, "modules", "app", "module.toml"), `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`)
	writeMutationFile(
		t,
		filepath.Join(fixture.repository, "modules", "app", "config"),
		"portable",
	)
	target := filepath.Join(fixture.home, ".app")
	writeMutationFile(t, target, "private")

	result, err := Apply(ApplyRequest{
		Home:     fixture.home,
		Controls: fixture.controls,
		Machine:  machine,
		Platform: testPlatform(),
	})
	if err == nil || !result.Plan.HasIssues() || result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(ordinary file) = (%#v, %v), want read-only conflict", result, err)
	}
	if contents, readErr := os.ReadFile(target); readErr != nil || string(contents) != "private" {
		t.Fatalf("ordinary target = (%q, %v), want unchanged", contents, readErr)
	}
	assertApplyBookkeepingMissing(t, fixture)
}

func TestApplyRejectsMalformedOtherEffectiveManifestBeforeLockBookkeeping(t *testing.T) {
	fixture := newMutationFixture(t, `base = ["broken", "good"]`)
	machine := fixture.machine([]string{"base"}, nil)
	publishMutationMachine(t, fixture.controls.Config, machine)
	writeMutationFile(t, filepath.Join(fixture.repository, "modules", "good", "module.toml"), `
[[links]]
id = "config"
source = "config"
target = "~/.good"
`)
	writeMutationFile(
		t,
		filepath.Join(fixture.repository, "modules", "good", "config"),
		"good",
	)
	writeMutationFile(
		t,
		filepath.Join(fixture.repository, "modules", "broken", "module.toml"),
		"unknown = true\n",
	)
	result, err := Apply(ApplyRequest{
		Home:     fixture.home,
		Controls: fixture.controls,
		Machine:  machine,
		Platform: testPlatform(),
	})
	if err == nil || result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(malformed effective manifest) = (%#v, %v), want read-only failure", result, err)
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Fatalf("Apply(malformed effective manifest) error = %v, want broken module context", err)
	}
	if _, statErr := os.Lstat(filepath.Join(fixture.home, ".good")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("good target error = %v, want missing", statErr)
	}
	assertApplyBookkeepingMissing(t, fixture)
}

func TestApplyRejectsTargetTopologyBeforeLockBookkeeping(t *testing.T) {
	fixture := newMutationFixture(t, `base = ["app"]`)
	machine := fixture.machine([]string{"base"}, nil)
	publishMutationMachine(t, fixture.controls.Config, machine)
	writeMutationFile(t, filepath.Join(fixture.repository, "modules", "app", "module.toml"), `
[[links]]
id = "parent"
source = "parent"
target = "~/.config/app"

[[links]]
id = "child"
source = "child"
target = "~/.config/app/child"
`)
	writeMutationFile(
		t,
		filepath.Join(fixture.repository, "modules", "app", "parent"),
		"parent",
	)
	writeMutationFile(
		t,
		filepath.Join(fixture.repository, "modules", "app", "child"),
		"child",
	)

	result, err := Apply(ApplyRequest{
		Home:     fixture.home,
		Controls: fixture.controls,
		Machine:  machine,
		Platform: testPlatform(),
	})
	if err == nil || !result.Plan.HasIssues() || result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(target topology) = (%#v, %v), want read-only blocker", result, err)
	}
	assertApplyBookkeepingMissing(t, fixture)
}

func TestApplyRejectsControlTopologyBeforeLockBookkeeping(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	fixture.controls.Config = filepath.Join(
		fixture.repository,
		"control",
		"machine.toml",
	)
	machine := fixture.machine([]string{"base"}, nil)
	publishMutationMachine(t, fixture.controls.Config, machine)

	result, err := Apply(ApplyRequest{
		Home:     fixture.home,
		Controls: fixture.controls,
		Machine:  machine,
		Platform: testPlatform(),
	})
	if err == nil || result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(control topology) = (%#v, %v), want read-only blocker", result, err)
	}
	if !errors.Is(err, corepaths.ErrControlTopology) {
		t.Fatalf("Apply(control topology) error = %v, want ErrControlTopology", err)
	}
	assertApplyBookkeepingMissing(t, fixture)
}

func TestApplyRejectsIndeterminateSelectionBeforeLockBookkeeping(t *testing.T) {
	fixture := newMutationFixture(t, `base = ["app"]`)
	machine := fixture.machine([]string{"base"}, nil)
	publishMutationMachine(t, fixture.controls.Config, machine)
	writeMutationFile(t, filepath.Join(fixture.repository, "modules", "app", "module.toml"), `
[match]
os = ["linux"]
distro = ["ubuntu"]
`)
	platform := testPlatform()
	platform.Distro = config.UnknownPlatformField("distribution is unavailable")

	result, err := Apply(ApplyRequest{
		Home:     fixture.home,
		Controls: fixture.controls,
		Machine:  machine,
		Platform: platform,
	})
	if err == nil || result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(indeterminate selection) = (%#v, %v), want read-only blocker", result, err)
	}
	if !strings.Contains(err.Error(), "indeterminate") {
		t.Fatalf("Apply(indeterminate selection) error = %v", err)
	}
	assertApplyBookkeepingMissing(t, fixture)
}

func TestApplyRejectsBusyLock(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	machine := fixture.machine([]string{"base"}, nil)
	publishMutationMachine(t, fixture.controls.Config, machine)
	release, err := mutationlock.Acquire(filepath.Dir(fixture.controls.Lock), fixture.controls.Lock)
	if err != nil {
		t.Fatalf("lock.Acquire() error = %v", err)
	}
	defer func() { _ = release() }()

	result, err := Apply(ApplyRequest{
		Home:     fixture.home,
		Controls: fixture.controls,
		Machine:  machine,
		Platform: testPlatform(),
	})
	if !errors.Is(err, ErrBusy) || result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(locked) = (%#v, %v), want ErrBusy", result, err)
	}
}

func TestApplyRejectsMachineDriftWithoutMutation(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	expected := fixture.machine([]string{"base"}, nil)
	changed := fixture.machine(nil, nil)
	publishMutationMachine(t, fixture.controls.Config, changed)

	_, err := Apply(ApplyRequest{
		Home:     fixture.home,
		Controls: fixture.controls,
		Machine:  expected,
		Platform: testPlatform(),
	})
	if err == nil || !strings.Contains(err.Error(), "machine config changed") {
		t.Fatalf("Apply(machine drift) error = %v", err)
	}
	if _, err := os.Lstat(fixture.controls.State); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state error = %v, want missing", err)
	}
}

func TestJoinReleaseErrorPreservesFailureAndRerunGuidance(t *testing.T) {
	runErr := errors.New("synthetic apply failure")
	releaseErr := errors.New("synthetic release failure")

	err := joinReleaseError(runErr, releaseErr, "dot apply")
	if !errors.Is(err, runErr) || !errors.Is(err, releaseErr) {
		t.Fatalf("joinReleaseError() = %v, want both failures", err)
	}
	if !strings.Contains(err.Error(), "release mutation lock") ||
		!strings.Contains(err.Error(), "rerun dot apply") {
		t.Fatalf("joinReleaseError() = %q, want release and rerun guidance", err)
	}
}

func testPlatform() config.Platform {
	return config.Platform{
		OS:     config.KnownPlatformField("linux"),
		Distro: config.KnownPlatformField("ubuntu"),
		Arch:   config.KnownPlatformField("x86_64"),
	}
}

func assertApplyBookkeepingMissing(t *testing.T, fixture mutationFixture) {
	t.Helper()
	for _, path := range []string{
		fixture.controls.State,
		fixture.controls.Lock,
		filepath.Dir(fixture.controls.State),
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("apply bookkeeping path %q error = %v, want missing", path, err)
		}
	}
}

func writeMutationFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
