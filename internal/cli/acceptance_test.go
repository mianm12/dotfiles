package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/buildinfo"
	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/lock"
)

func TestAcceptance02_InitConflictLeavesSelectionArtifactsAndStateUntouched(t *testing.T) {
	fixture := newCLIFixture(t, "base = [\"app\"]")
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.config/app/config"
`, map[string]string{"config": "portable"})
	target := filepath.Join(fixture.home, ".config", "app", "config")
	writeCLIFile(t, target, "personal")
	before := snapshotCLIPaths(t, target)

	code, _, stderr := fixture.run("init", fixture.repository, "--profile", "base")
	if code != exitError || !strings.Contains(stderr, "plan conflict") {
		t.Fatalf("init = (%d, %q), want runtime conflict", code, stderr)
	}
	assertCLIPathsUnchanged(t, before)
	assertCLIMissing(t, fixture.config)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
}

func TestAcceptance01_InitProfilesThenApplyIsNoop(t *testing.T) {
	fixture := newCLIFixture(t, "base = [\"app\"]")
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})

	code, _, stderr := fixture.run("init", fixture.repository, "--profile", "base")
	if code != exitOK || stderr == "" {
		t.Fatalf("init = (%d, %q), want success with missing-state warning", code, stderr)
	}
	machine := fixture.loadMachine(t)
	if machine.Repository != fixture.repository ||
		!reflect.DeepEqual(machine.Profiles, []string{"base"}) ||
		len(machine.ExtraModules) != 0 {
		t.Fatalf("machine = %#v, want initialized base selection", machine)
	}
	target := filepath.Join(fixture.home, ".app")
	assertCLILink(t, target, filepath.Join(fixture.repository, "modules", "app", "config"))

	before := snapshotCLIPaths(t, fixture.config, fixture.state, fixture.lock, target)
	code, stdout, stderr := fixture.run("status")
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "app  converged") {
		t.Fatalf("status after init = (%d, %q, %q), want converged", code, stdout, stderr)
	}
	assertCLIPathsUnchanged(t, before)

	code, _, stderr = fixture.run("apply")
	if code != exitOK || stderr != "" {
		t.Fatalf("apply after init = (%d, %q), want clean success", code, stderr)
	}
	assertCLIPathsUnchanged(t, before)
}

func TestB6InitDryRunIsStrictlyReadOnly(t *testing.T) {
	fixture := newCLIFixture(t, "base = [\"app\"]")
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})

	code, stdout, stderr := fixture.run(
		"init",
		fixture.repository,
		"--profile",
		"base",
		"--dry-run",
	)
	if code != exitOK || !strings.Contains(stdout, "create-link") || stderr == "" {
		t.Fatalf("init dry-run = (%d, %q, %q)", code, stdout, stderr)
	}
	assertCLIMissing(t, fixture.config)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, fixture.lock)
	assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
}

func TestAcceptance03_ExplicitNotApplicableDoesNotChangeSelection(t *testing.T) {
	fixture := newCLIFixture(t, "base = []")
	otherOS := "macos"
	if runtime.GOOS == "darwin" {
		otherOS = "linux"
	}
	fixture.writeModule(t, "other-platform", `
[match]
os = ["`+otherOS+`"]

[[links]]
id = "config"
source = "config"
target = "~/.other-platform"
`, map[string]string{"config": "other"})
	fixture.writeMachine(t, []string{"base"}, nil)
	before := snapshotCLIPaths(t, fixture.config)

	code, _, stderr := fixture.run("apply", "other-platform")
	if code != exitError || !strings.Contains(stderr, "not applicable") {
		t.Fatalf("apply other-platform = (%d, %q), want not-applicable failure", code, stderr)
	}
	assertCLIPathsUnchanged(t, before)
	assertCLIMissing(t, fixture.state)
	assertCLIMissing(t, filepath.Join(fixture.home, ".other-platform"))
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want unchanged", extras)
	}
}

func TestAcceptance07_ApplyActivatesExtraAndRepeatsWithoutMutation(t *testing.T) {
	fixture := newCLIFixture(t, "base = []")
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
	fixture.writeMachine(t, []string{"base"}, nil)

	code, _, stderr := fixture.run("apply", "extra")
	if code != exitOK || stderr == "" {
		t.Fatalf("apply extra = (%d, %q), want success with missing-state warning", code, stderr)
	}
	target := filepath.Join(fixture.home, ".extra")
	assertCLILink(t, target, filepath.Join(fixture.repository, "modules", "extra", "config"))
	machine := fixture.loadMachine(t)
	if !reflect.DeepEqual(machine.ExtraModules, []string{"extra"}) {
		t.Fatalf("extra_modules = %v, want [extra]", machine.ExtraModules)
	}

	before := snapshotCLIPaths(t, fixture.config, fixture.state, fixture.lock, target)
	code, _, stderr = fixture.run("apply", "extra")
	if code != exitOK || stderr != "" {
		t.Fatalf("repeated apply extra = (%d, %q), want clean success", code, stderr)
	}
	assertCLIPathsUnchanged(t, before)
}

func TestAcceptance08_RemoveExtraKeepsLocalAndRejectsProfileModule(t *testing.T) {
	fixture := newCLIFixture(t, "base = [\"profiled\"]")
	fixture.writeModule(t, "profiled", `
[[links]]
id = "config"
source = "config"
target = "~/.profiled"
`, map[string]string{"config": "profiled"})
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"

[[locals]]
id = "local"
example = "local.example"
target = "~/.extra.local"
`, map[string]string{
		"config":        "extra",
		"local.example": "local",
	})
	fixture.writeMachine(t, []string{"base"}, []string{"extra"})

	code, _, stderr := fixture.run("apply")
	if code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}
	extraTarget := filepath.Join(fixture.home, ".extra")
	localTarget := filepath.Join(fixture.home, ".extra.local")
	profileTarget := filepath.Join(fixture.home, ".profiled")
	code, stdout, stderr := fixture.run("status")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "profiled  converged") ||
		!strings.Contains(stdout, "extra  converged") {
		t.Fatalf("status after apply = (%d, %q, %q), want converged modules", code, stdout, stderr)
	}

	beforeDryRun := snapshotCLIPaths(
		t,
		fixture.config,
		fixture.state,
		fixture.lock,
		extraTarget,
		localTarget,
		profileTarget,
	)
	code, _, stderr = fixture.run("remove", "extra", "--dry-run")
	if code != exitOK {
		t.Fatalf("remove extra dry-run = (%d, %q)", code, stderr)
	}
	assertCLIPathsUnchanged(t, beforeDryRun)

	code, _, stderr = fixture.run("remove", "extra")
	if code != exitOK || stderr == "" {
		t.Fatalf("remove extra = (%d, %q)", code, stderr)
	}
	assertCLIMissing(t, extraTarget)
	if data, err := os.ReadFile(localTarget); err != nil || string(data) != "local" {
		t.Fatalf("local after remove = (%q, %v), want preserved", data, err)
	}
	assertCLILink(
		t,
		profileTarget,
		filepath.Join(fixture.repository, "modules", "profiled", "config"),
	)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules after remove = %v, want empty", extras)
	}

	before := snapshotCLIPaths(t, fixture.config, fixture.state, fixture.lock, localTarget, profileTarget)
	code, _, stderr = fixture.run("apply")
	if code != exitOK || stderr != "" {
		t.Fatalf("apply after remove = (%d, %q), want zero mutation", code, stderr)
	}
	assertCLIPathsUnchanged(t, before)

	code, _, stderr = fixture.run("remove", "extra")
	if code != exitOK {
		t.Fatalf("repeated remove known inactive module = (%d, %q)", code, stderr)
	}
	assertCLIPathsUnchanged(t, before)

	code, _, stderr = fixture.run("remove", "profiled")
	if code != exitError || !strings.Contains(stderr, "active profile") {
		t.Fatalf("remove profiled = (%d, %q), want refusal", code, stderr)
	}
	assertCLIPathsUnchanged(t, before)
}

func TestAcceptance13_SelectionInterruptionConvergesOnRerun(t *testing.T) {
	fixture := newCLIFixture(t, "base = []")
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
	fixture.writeMachine(t, []string{"base"}, nil)

	fixture.env.afterSelectionPublish = func() error {
		return errors.New("injected interruption")
	}
	code, _, stderr := fixture.runInjected("apply", "extra")
	if code != exitError || !strings.Contains(stderr, "selection was saved") {
		t.Fatalf("interrupted apply = (%d, %q), want persisted-selection failure", code, stderr)
	}
	if extras := fixture.loadMachine(t).ExtraModules; !reflect.DeepEqual(extras, []string{"extra"}) {
		t.Fatalf("extra_modules after interruption = %v, want [extra]", extras)
	}
	assertCLIMissing(t, filepath.Join(fixture.home, ".extra"))
	assertCLIMissing(t, fixture.state)

	fixture.env.afterSelectionPublish = nil
	code, _, stderr = fixture.run("apply")
	if code != exitOK {
		t.Fatalf("recovery apply = (%d, %q)", code, stderr)
	}
	target := filepath.Join(fixture.home, ".extra")
	assertCLILink(t, target, filepath.Join(fixture.repository, "modules", "extra", "config"))
	before := snapshotCLIPaths(t, fixture.config, fixture.state, fixture.lock, target)
	code, _, stderr = fixture.run("apply")
	if code != exitOK || stderr != "" {
		t.Fatalf("repeated recovery apply = (%d, %q)", code, stderr)
	}
	assertCLIPathsUnchanged(t, before)
}

func TestAcceptance15_LockBusyAndReadOnlyCommandsNeverCreateLock(t *testing.T) {
	t.Run("second mutation fails", func(t *testing.T) {
		fixture := newCLIFixture(t, "base = []")
		fixture.writeMachine(t, []string{"base"}, nil)
		owner, err := lock.Acquire(filepath.Dir(fixture.lock), fixture.lock)
		if err != nil {
			t.Fatalf("lock.Acquire() error = %v", err)
		}
		defer func() {
			if err := owner.Release(); err != nil {
				t.Fatalf("owner.Release() error = %v", err)
			}
		}()

		code, _, stderr := fixture.runProcess("apply")
		if code != exitError || !strings.Contains(stderr, "another dot process") {
			t.Fatalf("locked apply = (%d, %q), want busy failure", code, stderr)
		}
	})

	t.Run("status and dry-run are strictly read-only", func(t *testing.T) {
		fixture := newCLIFixture(t, "base = []")
		fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
		fixture.writeMachine(t, []string{"base"}, nil)
		before := snapshotCLIPaths(t, fixture.config)

		code, _, stderr := fixture.run("status")
		if code != exitOK || stderr == "" {
			t.Fatalf("status = (%d, %q)", code, stderr)
		}
		code, stdout, stderr := fixture.run("apply", "extra", "--dry-run")
		if code != exitOK || !strings.Contains(stdout, "create-link") || stderr == "" {
			t.Fatalf("dry-run = (%d, %q, %q)", code, stdout, stderr)
		}
		assertCLIPathsUnchanged(t, before)
		assertCLIMissing(t, fixture.lock)
		assertCLIMissing(t, fixture.state)
		assertCLIMissing(t, filepath.Join(fixture.home, ".extra"))
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("dry-run extra_modules = %v, want unchanged", extras)
		}
	})
}

func TestAcceptance16_ProfileMissingFailsAndDeletedExtraStateCanBeRemoved(t *testing.T) {
	t.Run("active profile references missing module", func(t *testing.T) {
		fixture := newCLIFixture(t, "base = [\"gone\"]")
		fixture.writeMachine(t, []string{"base"}, nil)
		before := snapshotCLIPaths(t, fixture.config)

		code, _, stderr := fixture.run("apply")
		if code != exitError || !strings.Contains(stderr, "references missing module") {
			t.Fatalf("apply = (%d, %q), want missing profile module failure", code, stderr)
		}
		assertCLIPathsUnchanged(t, before)
		assertCLIMissing(t, fixture.state)
		assertCLIMissing(t, fixture.lock)
	})

	t.Run("deleted manifest with extra and state cleans safely", func(t *testing.T) {
		fixture := newCLIFixture(t, "base = []")
		fixture.writeMachine(t, []string{"base"}, []string{"gone"})
		target := filepath.Join(fixture.home, ".gone")
		destination := filepath.Join(fixture.repository, "modules", "gone", "removed")
		if err := os.Symlink(destination, target); err != nil {
			t.Fatalf("os.Symlink() error = %v", err)
		}
		resolved, err := corepaths.ResolveTarget(fixture.home, "~/.gone")
		if err != nil {
			t.Fatalf("ResolveTarget() error = %v", err)
		}
		fixture.writeState(t, state.Snapshot{
			Home: fixture.home,
			Modules: map[string]state.Module{
				"gone": {Placements: map[string]state.Placement{
					"config": {
						Kind:            state.KindLink,
						Target:          target,
						ResolvedTarget:  resolved.Resolved(),
						LinkDestination: destination,
					},
				}},
			},
		})

		code, _, stderr := fixture.run("remove", "gone")
		if code != exitOK {
			t.Fatalf("remove gone = (%d, %q)", code, stderr)
		}
		assertCLIMissing(t, target)
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("extra_modules = %v, want empty", extras)
		}
		loaded, err := state.Load(fixture.state, fixture.home)
		if err != nil {
			t.Fatalf("state.Load() error = %v", err)
		}
		if _, exists := loaded.Snapshot.Modules["gone"]; exists {
			t.Fatalf("state still contains gone: %#v", loaded.Snapshot)
		}
		before := snapshotCLIPaths(t, fixture.config, fixture.state, fixture.lock)
		code, _, stderr = fixture.run("apply")
		if code != exitOK || stderr != "" {
			t.Fatalf("apply after deleted-module cleanup = (%d, %q)", code, stderr)
		}
		assertCLIPathsUnchanged(t, before)
		assertCLIMissing(t, target)
	})
}

func TestAcceptance17And18_ScopedLoadingIgnoresUnrelatedBrokenModuleButRejectsTarget(t *testing.T) {
	fixture := newCLIFixture(t, "base = []")
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "extra"})
	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "modules", "broken", "module.toml"),
		"unknown = true\n",
	)
	fixture.writeMachine(t, []string{"base"}, nil)

	code, _, stderr := fixture.run("apply", "extra")
	if code != exitOK {
		t.Fatalf("apply extra with unrelated broken module = (%d, %q)", code, stderr)
	}
	assertCLILink(
		t,
		filepath.Join(fixture.home, ".extra"),
		filepath.Join(fixture.repository, "modules", "extra", "config"),
	)

	before := snapshotCLIPaths(t, fixture.config, fixture.state, filepath.Join(fixture.home, ".extra"))
	code, _, stderr = fixture.run("apply", "broken")
	if code != exitError ||
		!strings.Contains(stderr, `invalid configuration: module "broken"`) {
		t.Fatalf("apply broken = (%d, %q), want strict target error", code, stderr)
	}
	assertCLIPathsUnchanged(t, before)

	fixture.writeModule(t, "missing-source", `
[[links]]
id = "config"
source = "missing"
target = "~/.missing-source"
`, nil)
	before = snapshotCLIPaths(t, fixture.config, fixture.state, filepath.Join(fixture.home, ".extra"))
	code, _, stderr = fixture.run("apply", "missing-source")
	if code != exitError || !strings.Contains(stderr, "inspect source") {
		t.Fatalf("apply missing-source = (%d, %q), want source error", code, stderr)
	}
	assertCLIPathsUnchanged(t, before)
	assertCLIMissing(t, filepath.Join(fixture.home, ".missing-source"))
}

func TestB6ExitCodesAndStatusConflict(t *testing.T) {
	fixture := newCLIFixture(t, "base = [\"app\"]")
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
	fixture.writeMachine(t, []string{"base"}, nil)
	writeCLIFile(t, filepath.Join(fixture.home, ".app"), "personal")

	for _, args := range [][]string{
		{"apply", "one", "two"},
		{"remove"},
		{"apply", "--unknown"},
		{"init", fixture.repository},
		{"version", "extra"},
		{"help", "does-not-exist"},
		{"help", "apply", "extra"},
		{"unknown"},
	} {
		code, _, _ := fixture.run(args...)
		if code != exitUsage {
			t.Fatalf("run(%v) code = %d, want %d", args, code, exitUsage)
		}
	}
	code, _, _ := fixture.run("apply", "missing")
	if code != exitError {
		t.Fatalf("apply missing code = %d, want %d", code, exitError)
	}
	code, stdout, stderr := fixture.run("status")
	if code != exitOK || !strings.Contains(stdout, "conflict") || stderr == "" {
		t.Fatalf("status conflict = (%d, %q, %q), want successful status", code, stdout, stderr)
	}
}

func TestB6HelpListsOnlyPublicCommands(t *testing.T) {
	fixture := newCLIFixture(t, "")
	code, stdout, stderr := fixture.run("help")
	if code != exitOK || stderr != "" {
		t.Fatalf("help = (%d, %q)", code, stderr)
	}
	for _, command := range []string{"init", "status", "apply", "remove", "version"} {
		if !strings.Contains(stdout, command) {
			t.Fatalf("help missing %q:\n%s", command, stdout)
		}
	}
	for _, removed := range []string{"add", "doctor", "diff"} {
		if strings.Contains(stdout, "\n  "+removed+" ") {
			t.Fatalf("help still lists %q:\n%s", removed, stdout)
		}
	}
}

func TestB6PublicRunBlackBoxUsesSyntheticHome(t *testing.T) {
	fixture := newCLIFixture(t, "base = [\"app\"]")
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
	t.Setenv("HOME", fixture.home)

	var stdout, stderr bytes.Buffer
	code := Run(
		[]string{"init", fixture.repository, "--profile", "base"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != exitOK {
		t.Fatalf("Run(init) = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	target := filepath.Join(fixture.home, ".app")
	assertCLILink(t, target, filepath.Join(fixture.repository, "modules", "app", "config"))
	before := snapshotCLIPaths(t, fixture.config, fixture.state, fixture.lock, target)

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"apply"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK || stderr.String() != "" {
		t.Fatalf("Run(apply) = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertCLIPathsUnchanged(t, before)
}

type cliFixture struct {
	root       string
	home       string
	repository string
	config     string
	state      string
	lock       string
	env        environment
}

func newCLIFixture(t *testing.T, profiles string) *cliFixture {
	t.Helper()
	root := t.TempDir()
	fixture := &cliFixture{
		root:       root,
		home:       filepath.Join(root, "home"),
		repository: filepath.Join(root, "repository"),
	}
	fixture.config = filepath.Join(fixture.home, ".config", "dot", "config.toml")
	fixture.state = filepath.Join(fixture.home, ".local", "state", "dot", "state.json")
	fixture.lock = filepath.Join(fixture.home, ".local", "state", "dot", "lock")
	for _, directory := range []string{fixture.home, fixture.repository} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}
	writeCLIFile(t, filepath.Join(fixture.repository, "dot.toml"), "version = 1\n[profiles]\n"+profiles+"\n")
	fixture.env = environment{
		stdin: strings.NewReader(""),
		getwd: func() (string, error) { return fixture.repository, nil },
		userHomeDir: func() (string, error) {
			return fixture.home, nil
		},
		platform: func() config.Platform {
			return config.Platform{OS: "linux", Distro: "ubuntu", Arch: "x86_64"}
		},
		build: buildinfo.Info{Version: "test", Commit: "test", BuildTime: "test"},
	}
	t.Setenv("HOME", fixture.home)
	return fixture
}

func (fixture *cliFixture) run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(args, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func (fixture *cliFixture) runInjected(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	env := fixture.env
	env.stdout = &stdout
	env.stderr = &stderr
	code := run(args, env)
	return code, stdout.String(), stderr.String()
}

func (fixture *cliFixture) runProcess(args ...string) (int, string, string) {
	commandArgs := []string{"-test.run=^TestCLIHelperProcess$", "--"}
	commandArgs = append(commandArgs, args...)
	command := exec.Command(os.Args[0], commandArgs...)
	command.Env = append(os.Environ(), "DOT_CLI_HELPER_PROCESS=1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return exitOK, stdout.String(), stderr.String()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), stdout.String(), stderr.String()
	}
	return -1, stdout.String(), stderr.String() + err.Error()
}

func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("DOT_CLI_HELPER_PROCESS") != "1" {
		return
	}
	separator := slicesIndex(os.Args, "--")
	if separator < 0 {
		os.Exit(exitUsage)
	}
	os.Exit(Run(os.Args[separator+1:], os.Stdin, os.Stdout, os.Stderr))
}

func slicesIndex(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func (fixture *cliFixture) writeMachine(t *testing.T, profiles, extras []string) {
	t.Helper()
	if _, err := config.PublishMachine(fixture.config, config.Machine{
		Version:      1,
		Repository:   fixture.repository,
		Profiles:     append([]string(nil), profiles...),
		ExtraModules: append([]string(nil), extras...),
	}); err != nil {
		t.Fatalf("PublishMachine() error = %v", err)
	}
}

func (fixture *cliFixture) loadMachine(t *testing.T) config.Machine {
	t.Helper()
	machine, exists, err := config.LoadMachine(fixture.config)
	if err != nil || !exists {
		t.Fatalf("LoadMachine() = (%#v, %t, %v)", machine, exists, err)
	}
	return machine
}

func (fixture *cliFixture) writeModule(
	t *testing.T,
	id, manifest string,
	files map[string]string,
) {
	t.Helper()
	root := filepath.Join(fixture.repository, "modules", id)
	writeCLIFile(t, filepath.Join(root, "module.toml"), strings.TrimSpace(manifest)+"\n")
	for relative, content := range files {
		writeCLIFile(t, filepath.Join(root, filepath.FromSlash(relative)), content)
	}
}

func (fixture *cliFixture) writeState(t *testing.T, snapshot state.Snapshot) {
	t.Helper()
	data, err := state.Marshal(snapshot)
	if err != nil {
		t.Fatalf("state.Marshal() error = %v", err)
	}
	writeCLIFile(t, fixture.state, string(data))
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}

func assertCLILink(t *testing.T, target, destination string) {
	t.Helper()
	actual, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("os.Readlink(%q) error = %v", target, err)
	}
	if actual != destination {
		t.Fatalf("link %q = %q, want %q", target, actual, destination)
	}
}

func assertCLIMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("path %q error = %v, want missing", path, err)
	}
}

type cliPathSnapshot struct {
	path string
	mode fs.FileMode
	data string
	link string
}

func snapshotCLIPaths(t *testing.T, paths ...string) []cliPathSnapshot {
	t.Helper()
	result := make([]cliPathSnapshot, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("os.Lstat(%q) error = %v", path, err)
		}
		entry := cliPathSnapshot{path: path, mode: info.Mode()}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			entry.link, err = os.Readlink(path)
		case info.Mode().IsRegular():
			var data []byte
			data, err = os.ReadFile(path)
			entry.data = string(data)
		}
		if err != nil {
			t.Fatalf("snapshot %q error = %v", path, err)
		}
		result = append(result, entry)
	}
	return result
}

func assertCLIPathsUnchanged(t *testing.T, before []cliPathSnapshot) {
	t.Helper()
	paths := make([]string, len(before))
	for index := range before {
		paths[index] = before[index].path
	}
	after := snapshotCLIPaths(t, paths...)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("paths changed\nbefore=%#v\nafter=%#v", before, after)
	}
}
