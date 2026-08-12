package converge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
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
	if err != nil || first.Status != ApplyStatusApplied ||
		!first.TargetsChanged || !first.StateChanged {
		t.Fatalf("Apply(first) = (%#v, %v)", first, err)
	}
	if destination, err := os.Readlink(filepath.Join(fixture.home, ".app")); err != nil || destination != source {
		t.Fatalf("managed link = (%q, %v), want %q", destination, err, source)
	}
	second, err := Apply(request)
	if err != nil || second.Status != ApplyStatusApplied ||
		second.TargetsChanged || second.StateChanged {
		t.Fatalf("Apply(second) = (%#v, %v), want no-op", second, err)
	}
}

func TestApplyReturnsBlockedOutcomeAfterLockWithoutBusinessMutation(t *testing.T) {
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
	if err != nil || result.Status != ApplyStatusBlocked ||
		len(result.Report.Plan.Issues) == 0 ||
		result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(ordinary file) = (%#v, %v), want blocked outcome", result, err)
	}
	if contents, readErr := os.ReadFile(target); readErr != nil || string(contents) != "private" {
		t.Fatalf("ordinary target = (%q, %v), want unchanged", contents, readErr)
	}
	if _, statErr := os.Lstat(fixture.controls.State); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state error = %v, want missing", statErr)
	}
	assertApplyBookkeepingPresent(t, fixture)
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
	assertFailure(t, err, FailureStageAnalysis, false, RecoveryNone)
	if _, statErr := os.Lstat(fixture.controls.State); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state error = %v, want missing", statErr)
	}
	assertApplyBookkeepingPresent(t, fixture)
}

func TestApplyReturnsTargetTopologyBlockedOutcomeAfterLock(t *testing.T) {
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
	if err != nil || result.Status != ApplyStatusBlocked ||
		len(result.Report.Plan.Issues) == 0 ||
		result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(target topology) = (%#v, %v), want blocked outcome", result, err)
	}
	assertApplyBookkeepingPresent(t, fixture)
}

func TestApplyReturnsRepositoryControlTopologyBlockedOutcomeAfterLock(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	fixture.controls.Config = filepath.Join(
		fixture.repository,
		"control",
		"machine.toml",
	)
	machine := fixture.machine([]string{"base"}, nil)
	publishMutationMachine(t, fixture.controls.Config, machine)

	result, err := Apply(fixture.environment())
	if err != nil || result.Status != ApplyStatusBlocked ||
		result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(control topology) = (%#v, %v), want blocked outcome", result, err)
	}
	assertApplyBookkeepingPresent(t, fixture)
}

func TestApplyReturnsIndeterminateSelectionBlockedOutcomeAfterLock(t *testing.T) {
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
	if err != nil || result.Status != ApplyStatusBlocked ||
		result.TargetsChanged || result.StateChanged ||
		!planIssueReasonContains(result.Report.Plan, "indeterminate") {
		t.Fatalf("Apply(indeterminate selection) = (%#v, %v), want blocked outcome", result, err)
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
	assertFailure(t, err, FailureStageLock, false, RecoveryNone)
}

func TestAnalysisAndSelectionRejectSymlinkedConfigRootBeforeReadingMachine(t *testing.T) {
	tests := []struct {
		name string
		run  func(Environment) error
	}{
		{
			name: "analyze",
			run: func(environment Environment) error {
				_, err := Analyze(environment)
				return err
			},
		},
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
			assertFailure(t, err, FailureStageInput, false, RecoveryPaths)
			assertApplyBookkeepingMissing(t, fixture)
		})
	}
}

func TestApplyLockedUsesLatestMachineWithoutPreflightFingerprint(t *testing.T) {
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
	if err != nil || result.Status != ApplyStatusApplied ||
		result.TargetsChanged || !result.StateChanged {
		t.Fatalf("applyLocked(latest machine) = (%#v, %v), want empty current selection", result, err)
	}
	if _, statErr := os.Lstat(filepath.Join(fixture.home, ".app")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target error = %v, want stale pre-lock selection ignored", statErr)
	}
	if _, statErr := os.Lstat(fixture.controls.State); statErr != nil {
		t.Fatalf("state error = %v, want committed empty v4 state", statErr)
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
	assertFailure(t, err, FailureStageLock, false, RecoveryNone)
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
	if err != nil || result.Status != ApplyStatusApplied {
		t.Fatalf("Apply() = (%#v, %v), want applied outcome", result, err)
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
	assertFailure(t, err, FailureStageLock, false, RecoveryNone)
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
	if err != nil || result.Status != ApplyStatusBlocked ||
		countIssues(result.Report.Plan, IssueBlocker) != 1 ||
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

func TestApplyLockedRefreshesResolvedControls(t *testing.T) {
	fixture := newMutationFixture(t, `base = ["app"]`)
	secondRepository := filepath.Join(fixture.root, "second-repository")
	writeMutationFile(
		t,
		filepath.Join(secondRepository, "dot.toml"),
		"version = 1\n[profiles]\nbase = [\"app\"]\n",
	)
	repositoryAlias := filepath.Join(fixture.root, "repository-alias")
	if err := os.Symlink(fixture.repository, repositoryAlias); err != nil {
		t.Fatalf("os.Symlink(first repository) error = %v", err)
	}
	machine := fixture.machine([]string{"base"}, nil)
	machine.Repository = repositoryAlias
	publishMutationMachine(t, fixture.controls.Config, machine)
	writeMutationFile(t, filepath.Join(fixture.repository, "modules", "app", "module.toml"), `
[[links]]
id = "config"
source = "config"
target = "~/.first"
`)
	writeMutationFile(
		t,
		filepath.Join(fixture.repository, "modules", "app", "config"),
		"first",
	)
	writeMutationFile(t, filepath.Join(secondRepository, "modules", "app", "module.toml"), `
[[links]]
id = "config"
source = "config"
target = "~/.second"
`)
	writeMutationFile(
		t,
		filepath.Join(secondRepository, "modules", "app", "config"),
		"second",
	)
	environment := fixture.environment()
	prelock, err := Analyze(environment)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	firstTarget := filepath.Join(fixture.home, ".first")
	if len(prelock.Plan.Actions) != 1 ||
		prelock.Plan.Actions[0].Target != firstTarget {
		t.Fatalf("pre-lock analysis = %#v, want first-repository target", prelock.Plan)
	}
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
	if err := os.Remove(repositoryAlias); err != nil {
		t.Fatalf("os.Remove(repository alias) error = %v", err)
	}
	if err := os.Symlink(secondRepository, repositoryAlias); err != nil {
		t.Fatalf("os.Symlink(second repository) error = %v", err)
	}

	result, err := applyLocked(prepared)
	if err != nil || result.Status != ApplyStatusApplied ||
		!result.TargetsChanged || !result.StateChanged {
		t.Fatalf("applyLocked() = (%#v, %v), want fresh second-repository plan", result, err)
	}
	if _, err := os.Lstat(firstTarget); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preflight target error = %v, want missing", err)
	}
	wantSource := filepath.Join(repositoryAlias, "modules", "app", "config")
	if destination, err := os.Readlink(filepath.Join(fixture.home, ".second")); err != nil || destination != wantSource {
		t.Fatalf("locked target = (%q, %v), want %q", destination, err, wantSource)
	}
}

func TestJoinReleaseFailureOverridesOutcomeWithTypedPartialFailure(t *testing.T) {
	runErr := errors.New("synthetic apply failure")
	releaseErr := errors.New("synthetic release failure")

	err := joinReleaseFailure(runErr, releaseErr)
	if !errors.Is(err, runErr) || !errors.Is(err, releaseErr) {
		t.Fatalf("joinReleaseFailure() = %v, want both failures", err)
	}
	if !strings.Contains(err.Error(), "release mutation lock") {
		t.Fatalf("joinReleaseFailure() = %q, want release context", err)
	}
	assertFailure(t, err, FailureStageLockRelease, true, RecoveryRerunApply)
}

func TestAnalysisRecoveryClassifiesOnlyActionableDomainFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want Recovery
	}{
		{name: "uninitialized", err: errMachineUninitialized, want: RecoveryInit},
		{name: "control", err: controlError{cause: errors.New("control")}, want: RecoveryPaths},
		{name: "invalid state", err: state.ErrInvalid, want: RecoveryArchiveState},
		{name: "legacy state", err: state.ErrLegacyVersion, want: RecoveryArchiveState},
		{name: "too-new state", err: state.ErrTooNew, want: RecoveryArchiveState},
		{name: "home mismatch", err: state.ErrHomeMismatch, want: RecoveryArchiveState},
		{name: "ordinary state I/O", err: fmt.Errorf("read state: %w", os.ErrPermission), want: RecoveryNone},
		{name: "configuration", err: errors.New("malformed manifest"), want: RecoveryNone},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := recoveryForAnalysisFailure(test.err); got != test.want {
				t.Fatalf("recoveryForAnalysisFailure(%v) = %q, want %q", test.err, got, test.want)
			}
		})
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
	stage FailureStage,
	partial bool,
	recovery Recovery,
) {
	t.Helper()
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want *Failure", err)
	}
	if failure.Stage != stage || failure.Partial != partial || failure.Recovery != recovery {
		t.Fatalf(
			"failure = %#v, want stage=%s partial=%t recovery=%s",
			failure,
			stage,
			partial,
			recovery,
		)
	}
}

func planIssueReasonContains(plan Plan, fragment string) bool {
	for _, issue := range plan.Issues {
		if strings.Contains(issue.Reason, fragment) {
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
