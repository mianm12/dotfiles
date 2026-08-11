package converge

import (
	"bytes"
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
		Platform:   testPlatform(),
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
	result, err = SelectAdd(environment, "app")
	if err != nil || !result.Changed || len(result.Machine.ExtraModules) != 1 {
		t.Fatalf("SelectAdd() = (%#v, %v), want app selected", result, err)
	}
	repeated, err := SelectAdd(environment, "app")
	if err != nil || repeated.Changed {
		t.Fatalf("SelectAdd(repeat) = (%#v, %v), want no-op", repeated, err)
	}
}

func TestInitializeRejectsInvalidProfileBeforeLockBookkeeping(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	result, err := Initialize(
		fixture.environment(),
		fixture.repository,
		[]string{"missing-profile"},
	)
	if err == nil || result.Changed {
		t.Fatalf("Initialize(invalid profile) = (%#v, %v), want failure", result, err)
	}
	for _, path := range []string{fixture.controls.Config, fixture.controls.State, fixture.controls.Lock} {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("control path %q error = %v, want missing", path, statErr)
		}
	}
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

	result, err := Apply(fixture.environment())
	if err == nil || len(result.Report.Plan.Issues) == 0 || result.TargetsChanged || result.StateChanged {
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

	result, err := Apply(fixture.environment())
	if err == nil || len(result.Report.Plan.Issues) == 0 || result.TargetsChanged || result.StateChanged {
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

	result, err := Apply(fixture.environment())
	if err == nil || result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(control topology) = (%#v, %v), want read-only blocker", result, err)
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("Apply(control topology) error = %v, want ErrBlocked", err)
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

	environment := fixture.environment()
	environment.Platform = platform
	result, err := Apply(environment)
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
	release, err := acquireLock(filepath.Dir(fixture.controls.Lock), fixture.controls.Lock)
	if err != nil {
		t.Fatalf("lock.Acquire() error = %v", err)
	}
	defer func() { _ = release() }()

	result, err := Apply(fixture.environment())
	if !errors.Is(err, ErrBusy) || result.TargetsChanged || result.StateChanged {
		t.Fatalf("Apply(locked) = (%#v, %v), want ErrBusy", result, err)
	}
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
			if !errors.Is(err, ErrControl) || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("operation error = %v, want control-root symlink rejection", err)
			}
			assertApplyBookkeepingMissing(t, fixture)
		})
	}
}

func TestApplyLockedRejectsMachineDriftWithoutExecutingPreflightPlan(t *testing.T) {
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
	report, err := Analyze(environment)
	if err != nil || !report.Plan.Executable() || len(report.Plan.Steps) != 1 {
		t.Fatalf("Analyze() = (%#v, %v), want one executable preflight step", report, err)
	}
	preflight, result, err := prepareApply(environment)
	if err != nil || len(result.Report.Plan.Steps) != 0 {
		t.Fatalf("prepareApply() = (%#v, %#v, %v), want executable preflight", preflight, result, err)
	}
	release, err := acquire(environment, preflight.controls)
	if err != nil {
		t.Fatalf("acquire() error = %v", err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Errorf("release() error = %v", err)
		}
	}()

	publishMutationMachine(t, fixture.controls.Config, fixture.machine(nil, nil))
	result, err = applyLocked(environment, preflight.fingerprint)
	if !errors.Is(err, ErrBlocked) ||
		!strings.Contains(err.Error(), "machine config changed") ||
		result.TargetsChanged || result.StateChanged {
		t.Fatalf("applyLocked(machine drift) = (%#v, %v), want ErrBlocked", result, err)
	}
	if _, statErr := os.Lstat(filepath.Join(fixture.home, ".app")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target error = %v, want old plan unexecuted", statErr)
	}
	if _, statErr := os.Lstat(fixture.controls.State); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state error = %v, want missing", statErr)
	}
}

func TestMachineFingerprintUsesSelectionSetSemantics(t *testing.T) {
	fixture := newMutationFixture(t, "base = []")
	left, err := machineFingerprint(fixture.machine(
		[]string{"work", "base", "work"},
		[]string{"shell", "editor", "shell"},
	))
	if err != nil {
		t.Fatalf("machineFingerprint(left) error = %v", err)
	}
	right, err := machineFingerprint(fixture.machine(
		[]string{"base", "work"},
		[]string{"editor", "shell"},
	))
	if err != nil {
		t.Fatalf("machineFingerprint(right) error = %v", err)
	}
	if !bytes.Equal(left, right) {
		t.Fatalf("semantic fingerprints differ:\nleft=%q\nright=%q", left, right)
	}

	changed, err := machineFingerprint(fixture.machine(
		[]string{"base"},
		[]string{"editor", "shell"},
	))
	if err != nil {
		t.Fatalf("machineFingerprint(changed) error = %v", err)
	}
	if bytes.Equal(left, changed) {
		t.Fatal("semantic fingerprint ignored a changed profile set")
	}
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
	report, err := Analyze(environment)
	if err != nil || !report.Plan.Executable() || len(report.Plan.Steps) != 1 {
		t.Fatalf("Analyze() = (%#v, %v), want one executable preflight step", report, err)
	}
	preflight, _, err := prepareApply(environment)
	if err != nil {
		t.Fatalf("prepareApply() error = %v", err)
	}
	release, err := acquire(environment, preflight.controls)
	if err != nil {
		t.Fatalf("acquire() error = %v", err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Errorf("release() error = %v", err)
		}
	}()

	target := filepath.Join(fixture.home, ".app")
	writeMutationFile(t, target, "arrived while locked")
	result, err := applyLocked(environment, preflight.fingerprint)
	if !errors.Is(err, ErrBlocked) ||
		len(result.Report.Plan.Issues) != 1 ||
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

func TestJoinReleaseErrorPreservesFailureAndPartialClassification(t *testing.T) {
	runErr := errors.New("synthetic apply failure")
	releaseErr := errors.New("synthetic release failure")

	err := joinReleaseError(runErr, releaseErr)
	if !errors.Is(err, runErr) || !errors.Is(err, releaseErr) {
		t.Fatalf("joinReleaseError() = %v, want both failures", err)
	}
	if !strings.Contains(err.Error(), "release mutation lock") ||
		!errors.Is(err, ErrPartial) {
		t.Fatalf("joinReleaseError() = %q, want release and partial classification", err)
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
