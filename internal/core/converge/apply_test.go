package converge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
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

func (fixture mutationFixture) environment() Environment {
	return Environment{
		Home:       fixture.home,
		ConfigPath: fixture.controls.Config,
		StatePath:  fixture.controls.State,
		LockPath:   fixture.controls.Lock,
		Platform:   testPlatform,
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

func TestSelectionMutationsOwnConfigOnlyChanges(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	environment := fixture.environment()
	result, err := Initialize(environment, fixture.repository, []string{"base"})
	if err != nil || !result.Changed {
		t.Fatalf("Initialize() = (%#v, %v), want changed", result, err)
	}
	if _, err := os.Lstat(fixture.controls.State); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state error = %v, want missing", err)
	}
	if entries, err := os.ReadDir(fixture.home); err != nil || len(entries) != 0 {
		t.Fatalf("HOME entries = (%v, %v), want empty", entries, err)
	}

	writeMutationFile(t, filepath.Join(fixture.repository, "modules", "app", "module.toml"), "")
	platformCalls := 0
	environment.Platform = func() config.Platform {
		platformCalls++
		return testPlatform()
	}
	result, err = SelectAdd(environment, "app")
	if err != nil || !result.Changed || len(result.Machine.ExtraModules) != 1 {
		t.Fatalf("SelectAdd() = (%#v, %v), want app selected", result, err)
	}
	if platformCalls != 1 {
		t.Fatalf("SelectAdd platform resolver calls = %d, want one locked decision", platformCalls)
	}
	repeated, err := SelectAdd(environment, "app")
	if err != nil || repeated.Changed {
		t.Fatalf("SelectAdd(repeat) = (%#v, %v), want no-op", repeated, err)
	}
	if platformCalls != 1 {
		t.Fatalf("SelectAdd repeat resolved unnecessary platform: calls=%d", platformCalls)
	}
}

func TestSelectionMutationsDoNotReadStateContentOrTargets(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	writeMutationFile(t, fixture.controls.State, "not valid state")
	writeMutationFile(t, filepath.Join(fixture.repository, "modules", "app", "module.toml"), `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`)
	writeMutationFile(t, filepath.Join(fixture.repository, "modules", "app", "config"), "source")
	target := filepath.Join(fixture.home, ".app")
	writeMutationFile(t, target, "personal")
	stateBefore, err := os.ReadFile(fixture.controls.State)
	if err != nil {
		t.Fatalf("os.ReadFile(state) error = %v", err)
	}
	targetBefore, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("os.ReadFile(target) error = %v", err)
	}

	if _, err := Initialize(fixture.environment(), fixture.repository, []string{"base"}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := SelectAdd(fixture.environment(), "app"); err != nil {
		t.Fatalf("SelectAdd() error = %v", err)
	}
	stateAfter, err := os.ReadFile(fixture.controls.State)
	if err != nil || string(stateAfter) != string(stateBefore) {
		t.Fatalf("state after selection mutations = (%q, %v), want unchanged", stateAfter, err)
	}
	targetAfter, err := os.ReadFile(target)
	if err != nil || string(targetAfter) != string(targetBefore) {
		t.Fatalf("target after selection mutations = (%q, %v), want unchanged", targetAfter, err)
	}
}

func TestInitializeRejectsInvalidProfileAfterLockWithoutBusinessMutation(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	result, err := Initialize(
		fixture.environment(),
		fixture.repository,
		[]string{"missing-profile"},
	)
	if err == nil || result.Changed {
		t.Fatalf("Initialize(invalid profile) = (%#v, %v), want failure", result, err)
	}
	for _, path := range []string{fixture.controls.Config, fixture.controls.State} {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("control path %q error = %v, want missing", path, statErr)
		}
	}
	assertApplyBookkeepingPresent(t, fixture)
}

func TestSelectRemoveDoesNotDecodeTargetManifest(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	machine := fixture.machine([]string{"base"}, []string{"broken"})
	publishMutationMachine(t, fixture.controls.Config, machine)
	writeMutationFile(
		t,
		filepath.Join(fixture.repository, "modules", "broken", "module.toml"),
		"unknown = true\n",
	)

	result, err := SelectRemove(fixture.environment(), "broken")
	if err != nil || !result.Changed || len(result.Machine.ExtraModules) != 0 {
		t.Fatalf("SelectRemove(malformed) = (%#v, %v)", result, err)
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
	request := fixture.environment()

	first, err := Apply(request)
	if err != nil || first.Report.HasSkip() ||
		!first.TargetsChanged || !first.StateChanged {
		t.Fatalf("Apply(first) = (%#v, %v)", first, err)
	}
	if destination, err := os.Readlink(filepath.Join(fixture.home, ".app")); err != nil || destination != source {
		t.Fatalf("managed link = (%q, %v), want %q", destination, err, source)
	}
	second, err := Apply(request)
	if err != nil || second.Report.HasSkip() ||
		second.TargetsChanged || second.StateChanged {
		t.Fatalf("Apply(second) = (%#v, %v), want no-op", second, err)
	}
}

func TestApplyReturnsSkipResultAfterLockWithoutBusinessMutation(t *testing.T) {
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

	result, err := Apply(fixture.environment())
	if err != nil || !result.Report.HasSkip() ||
		len(result.Report.Lines) == 0 ||
		result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(ordinary file) = (%#v, %v), want skip result", result, err)
	}
	if contents, readErr := os.ReadFile(target); readErr != nil || string(contents) != "private" {
		t.Fatalf("ordinary target = (%q, %v), want unchanged", contents, readErr)
	}
	if _, statErr := os.Lstat(fixture.controls.State); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state error = %v, want missing", statErr)
	}
	assertApplyBookkeepingPresent(t, fixture)
}

func TestApplySkipBlocksPermissionRepairThenRepairsOnce(t *testing.T) {
	fixture := newMutationFixture(t, `base = ["app"]`)
	publishMutationMachine(t, fixture.controls.Config, fixture.machine([]string{"base"}, nil))
	writeMutationFile(t, filepath.Join(fixture.repository, "modules", "app", "module.toml"), `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`)
	source := filepath.Join(fixture.repository, "modules", "app", "config")
	writeMutationFile(t, source, "portable")

	initial, err := Apply(fixture.environment())
	if err != nil || initial.Report.HasSkip() || !initial.TargetsChanged || !initial.StateChanged {
		t.Fatalf("Apply(initial) = (%#v, %v), want converged link and state", initial, err)
	}
	target := filepath.Join(fixture.home, ".app")
	if err := os.Remove(target); err != nil {
		t.Fatalf("os.Remove(managed target) error = %v", err)
	}
	writeMutationFile(t, target, "personal")

	controlPaths := []struct {
		path string
		mode os.FileMode
	}{
		{path: filepath.Dir(fixture.controls.Config), mode: 0o755},
		{path: fixture.controls.Config, mode: 0o644},
		{path: filepath.Dir(fixture.controls.State), mode: 0o755},
		{path: fixture.controls.State, mode: 0o644},
		{path: fixture.controls.Lock, mode: 0o644},
	}
	for _, control := range controlPaths {
		if err := os.Chmod(control.path, control.mode); err != nil {
			t.Fatalf("os.Chmod(%q) error = %v", control.path, err)
		}
	}

	blocked, err := Apply(fixture.environment())
	if err != nil || !blocked.Report.HasSkip() || blocked.ControlsChanged ||
		blocked.TargetsChanged || blocked.StateChanged || countOps(blocked.Report, OpChmod) != len(controlPaths) {
		t.Fatalf("Apply(blocked) = (%#v, %v), want skip plus five unexecuted chmod lines", blocked, err)
	}
	for _, control := range controlPaths {
		assertMutationMode(t, control.path, control.mode)
	}
	if contents, err := os.ReadFile(target); err != nil || string(contents) != "personal" {
		t.Fatalf("blocked target = (%q, %v), want personal file preserved", contents, err)
	}

	if err := os.Remove(target); err != nil {
		t.Fatalf("os.Remove(personal target) error = %v", err)
	}
	repaired, err := Apply(fixture.environment())
	if err != nil || repaired.Report.HasSkip() || !repaired.ControlsChanged ||
		!repaired.TargetsChanged || repaired.StateChanged || countLineOps(repaired.Done, OpChmod) != len(controlPaths) {
		t.Fatalf("Apply(repair) = (%#v, %v), want five chmods and recreated link", repaired, err)
	}
	for index, control := range controlPaths {
		want := storage.PrivateFileMode
		if index == 0 || index == 2 {
			want = storage.PrivateDirectoryMode
		}
		assertMutationMode(t, control.path, want)
	}

	again, err := Apply(fixture.environment())
	if err != nil || again.Report.HasSkip() || again.ControlsChanged ||
		again.TargetsChanged || again.StateChanged || len(again.Done) != 0 {
		t.Fatalf("Apply(after repair) = (%#v, %v), want no-op", again, err)
	}
}

func TestApplyReportsLockedAnalysisFailureWithoutBusinessMutation(t *testing.T) {
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
	result, err := Apply(fixture.environment())
	if err == nil || result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(malformed effective manifest) = (%#v, %v), want read-only failure", result, err)
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Fatalf("Apply(malformed effective manifest) error = %v, want broken module context", err)
	}
	if _, statErr := os.Lstat(filepath.Join(fixture.home, ".good")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("good target error = %v, want missing", statErr)
	}
	assertFailure(t, err, false)
	if _, statErr := os.Lstat(fixture.controls.State); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state error = %v, want missing", statErr)
	}
	assertApplyBookkeepingPresent(t, fixture)
}

func TestApplyReturnsTargetTopologySkipResultAfterLock(t *testing.T) {
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

	result, err := Apply(fixture.environment())
	if err != nil || !result.Report.HasSkip() ||
		len(result.Report.Lines) == 0 ||
		result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(target topology) = (%#v, %v), want skip result", result, err)
	}
	assertApplyBookkeepingPresent(t, fixture)
}

func TestApplyReturnsRepositoryControlTopologySkipResultAfterLock(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	fixture.controls.Config = filepath.Join(
		fixture.repository,
		"control",
		"machine.toml",
	)
	machine := fixture.machine([]string{"base"}, nil)
	publishMutationMachine(t, fixture.controls.Config, machine)

	result, err := Apply(fixture.environment())
	if err == nil || result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(control topology) = (%#v, %v), want analysis failure", result, err)
	}
	assertFailure(t, err, false)
	assertApplyBookkeepingPresent(t, fixture)
}

func TestApplyReturnsIndeterminateSelectionSkipResultAfterLock(t *testing.T) {
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

	environment := fixture.environment()
	environment.Platform = func() config.Platform { return platform }
	result, err := Apply(environment)
	if err != nil || !result.Report.HasSkip() ||
		result.TargetsChanged || result.StateChanged ||
		!reportHasSkipReason(result.Report, "indeterminate") {
		t.Fatalf("Apply(indeterminate selection) = (%#v, %v), want skip result", result, err)
	}
	assertApplyBookkeepingPresent(t, fixture)
}

func TestApplyRejectsBusyLock(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	machine := fixture.machine([]string{"base"}, nil)
	publishMutationMachine(t, fixture.controls.Config, machine)
	release, err := acquireLock(filepath.Dir(fixture.controls.Lock), fixture.controls.Lock)
	if err != nil {
		t.Fatalf("lock.Acquire() error = %v", err)
	}
	defer func() { _ = release() }()

	result, err := Apply(fixture.environment())
	if !errors.Is(err, ErrBusy) || result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(locked) = (%#v, %v), want ErrBusy", result, err)
	}
	assertFailure(t, err, false)
}

func TestAnalysisAndSelectionRejectSymlinkedConfigRootBeforeReadingMachine(t *testing.T) {
	tests := []struct {
		name string
		run  func(Environment) error
	}{
		{
			name: "select add",
			run: func(environment Environment) error {
				_, err := SelectAdd(environment, "app")
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMutationFixture(t, "base = []")
			externalRoot := filepath.Join(fixture.root, "external-config")
			writeMutationFile(
				t,
				filepath.Join(externalRoot, "machine.toml"),
				"version = [malformed",
			)
			if err := os.Symlink(externalRoot, filepath.Dir(fixture.controls.Config)); err != nil {
				t.Fatalf("os.Symlink(config root) error = %v", err)
			}

			err := test.run(fixture.environment())
			if !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("operation error = %v, want control-root symlink rejection", err)
			}
			assertFailure(t, err, false)
			assertApplyBookkeepingMissing(t, fixture)
		})
	}
}

func TestApplyLockedUsesLatestMachineAfterLock(t *testing.T) {
	fixture := newMutationFixture(t, `base = ["app"]`)
	initial := fixture.machine([]string{"base"}, nil)
	publishMutationMachine(t, fixture.controls.Config, initial)
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
	environment := fixture.environment()
	prepared, err := prepareMutationEnvironment(environment)
	if err != nil {
		t.Fatalf("prepareMutationEnvironment() error = %v", err)
	}
	release, err := acquireLock(filepath.Dir(prepared.LockPath), prepared.LockPath)
	if err != nil {
		t.Fatalf("acquireLock() error = %v", err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Errorf("release() error = %v", err)
		}
	}()

	publishMutationMachine(t, fixture.controls.Config, fixture.machine(nil, nil))
	result, err := applyLocked(prepared)
	if err != nil || result.Report.HasSkip() ||
		result.TargetsChanged || !result.StateChanged {
		t.Fatalf("applyLocked(latest machine) = (%#v, %v), want empty current selection", result, err)
	}
	if _, statErr := os.Lstat(filepath.Join(fixture.home, ".app")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target error = %v, want stale pre-lock selection ignored", statErr)
	}
	if _, statErr := os.Lstat(fixture.controls.State); statErr != nil {
		t.Fatalf("state error = %v, want committed empty v5 state", statErr)
	}
}

func TestApplyAcquiresLockBeforeReadingMachine(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	writeMutationFile(t, fixture.controls.Config, "malformed = [")
	release, err := acquireLock(filepath.Dir(fixture.controls.Lock), fixture.controls.Lock)
	if err != nil {
		t.Fatalf("acquireLock() error = %v", err)
	}
	defer func() { _ = release() }()

	environment := fixture.environment()
	environment.Platform = func() config.Platform {
		t.Fatal("Apply resolved platform before acquiring the busy lock")
		return config.Platform{}
	}
	_, err = Apply(environment)
	if !errors.Is(err, ErrBusy) || strings.Contains(err.Error(), "machine config") {
		t.Fatalf("Apply(busy, malformed machine) error = %v, want lock failure first", err)
	}
	assertFailure(t, err, false)
}

func TestApplyResolvesPlatformOnceInsideOwnedLock(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	publishMutationMachine(
		t,
		fixture.controls.Config,
		fixture.machine([]string{"base"}, nil),
	)
	environment := fixture.environment()
	platformCalls := 0
	environment.Platform = func() config.Platform {
		platformCalls++
		return testPlatform()
	}

	result, err := Apply(environment)
	if err != nil || result.Report.HasSkip() {
		t.Fatalf("Apply() = (%#v, %v), want successful result", result, err)
	}
	if platformCalls != 1 {
		t.Fatalf("platform resolver calls = %d, want one locked analysis", platformCalls)
	}
}

func TestSelectAddAcquiresLockBeforeMachineAndPlatformAnalysis(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	writeMutationFile(t, fixture.controls.Config, "malformed = [")
	release, err := acquireLock(filepath.Dir(fixture.controls.Lock), fixture.controls.Lock)
	if err != nil {
		t.Fatalf("acquireLock() error = %v", err)
	}
	defer func() { _ = release() }()
	environment := fixture.environment()
	environment.Platform = func() config.Platform {
		t.Fatal("SelectAdd resolved platform before acquiring the busy lock")
		return config.Platform{}
	}

	_, err = SelectAdd(environment, "app")
	if !errors.Is(err, ErrBusy) || strings.Contains(err.Error(), "machine config") {
		t.Fatalf("SelectAdd(busy, malformed machine) error = %v, want lock failure first", err)
	}
	assertFailure(t, err, false)
}

func TestApplyLockedUsesFreshFilesystemAnalysis(t *testing.T) {
	fixture := newMutationFixture(t, `base = ["app"]`)
	publishMutationMachine(t, fixture.controls.Config, fixture.machine([]string{"base"}, nil))
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
	environment := fixture.environment()
	prepared, err := prepareMutationEnvironment(environment)
	if err != nil {
		t.Fatalf("prepareMutationEnvironment() error = %v", err)
	}
	release, err := acquireLock(filepath.Dir(prepared.LockPath), prepared.LockPath)
	if err != nil {
		t.Fatalf("acquireLock() error = %v", err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Errorf("release() error = %v", err)
		}
	}()

	target := filepath.Join(fixture.home, ".app")
	writeMutationFile(t, target, "arrived while locked")
	result, err := applyLocked(prepared)
	if err != nil || !result.Report.HasSkip() ||
		countOps(result.Report, OpSkip) != 1 ||
		result.TargetsChanged || result.StateChanged {
		t.Fatalf("applyLocked(filesystem drift) = (%#v, %v), want fresh conflict", result, err)
	}
	if contents, readErr := os.ReadFile(target); readErr != nil || string(contents) != "arrived while locked" {
		t.Fatalf("changed target = (%q, %v), want preserved", contents, readErr)
	}
	if _, statErr := os.Lstat(fixture.controls.State); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state error = %v, want missing", statErr)
	}
}

func TestJoinReleaseFailureOverridesResultWithTypedPartialFailure(t *testing.T) {
	runErr := errors.New("synthetic apply failure")
	releaseErr := errors.New("synthetic release failure")

	err := joinReleaseFailure(runErr, releaseErr, true)
	if !errors.Is(err, runErr) || !errors.Is(err, releaseErr) {
		t.Fatalf("joinReleaseFailure() = %v, want both failures", err)
	}
	if !strings.Contains(err.Error(), "release mutation lock") {
		t.Fatalf("joinReleaseFailure() = %q, want release context", err)
	}
	assertFailure(t, err, true)
	clean := joinReleaseFailure(nil, releaseErr, false)
	assertFailure(t, clean, false)
}

func TestSelectionMutationLockedReportsReleaseFailureAfterPublication(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	releaseErr := errors.New("synthetic selection release failure")
	wantMachine := fixture.machine([]string{"base"}, nil)
	publishedBeforeRelease := false

	result, err := runSelectionMutationLocked(
		fixture.environment(),
		func() error {
			machine, exists, loadErr := config.LoadMachine(fixture.controls.Config)
			publishedBeforeRelease = loadErr == nil &&
				exists &&
				machine.Repository == wantMachine.Repository
			return releaseErr
		},
		func() (SelectionResult, error) {
			return SelectionResult{Machine: wantMachine, Changed: true}, nil
		},
	)

	if !result.Changed || result.Machine.Repository != wantMachine.Repository {
		t.Fatalf("runSelectionMutationLocked() result = %#v, want published selection", result)
	}
	if !publishedBeforeRelease {
		t.Fatal("selection was not published before the release callback")
	}
	if !errors.Is(err, releaseErr) {
		t.Fatalf("runSelectionMutationLocked() error = %v, want release failure", err)
	}
	assertFailure(t, err, true)
	machine, exists, loadErr := config.LoadMachine(fixture.controls.Config)
	if loadErr != nil || !exists || machine.Repository != wantMachine.Repository {
		t.Fatalf(
			"LoadMachine(published selection) = (%#v, %t, %v), want published machine",
			machine,
			exists,
			loadErr,
		)
	}
}

func TestControlErrorExposesOnlyControlPathSentinel(t *testing.T) {
	cause := errors.New("control")
	err := controlError{cause: cause}
	if !errors.Is(err, ErrControlPaths) || !errors.Is(err, cause) {
		t.Fatalf("controlError = %v, want control sentinel and cause", err)
	}
	if errors.Is(errors.New("control"), ErrControlPaths) {
		t.Fatal("ordinary error matched ErrControlPaths")
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

func assertApplyBookkeepingPresent(t *testing.T, fixture mutationFixture) {
	t.Helper()
	if info, err := os.Lstat(filepath.Dir(fixture.controls.State)); err != nil || !info.IsDir() {
		t.Fatalf("state root = (%v, %v), want directory bookkeeping", info, err)
	}
	if info, err := os.Lstat(fixture.controls.Lock); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("lock = (%v, %v), want regular-file bookkeeping", info, err)
	}
}

func assertFailure(
	t *testing.T,
	err error,
	mayHaveChanged bool,
) {
	t.Helper()
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want *Failure", err)
	}
	if failure.MayHaveChanged != mayHaveChanged {
		t.Fatalf("failure = %#v, want may_have_changed=%t", failure, mayHaveChanged)
	}
}

func countOps(report Report, op Op) int {
	count := 0
	for _, line := range report.Lines {
		if line.Op == op {
			count++
		}
	}
	return count
}

func countLineOps(lines []Line, op Op) int {
	count := 0
	for _, line := range lines {
		if line.Op == op {
			count++
		}
	}
	return count
}

func assertMutationMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("os.Lstat(%q) error = %v", path, err)
	}
	const modeMask = os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky
	if got := info.Mode() & modeMask; got != want {
		t.Fatalf("mode(%q) = %04o, want %04o", path, got, want)
	}
}

func reportHasSkipReason(report Report, fragment string) bool {
	for _, line := range report.Lines {
		if strings.Contains(line.Reason, fragment) {
			return true
		}
	}
	return false
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
