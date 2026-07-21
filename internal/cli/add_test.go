package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	addrunner "github.com/mianm12/dotfiles/internal/add"
	"github.com/mianm12/dotfiles/internal/buildinfo"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
)

func TestAdd_RequiresPathsAndRejectsModesBeforeRuntime(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing path", args: []string{"add"}, want: "requires at least 1 arg"},
		{name: "empty module", args: []string{"add", "--module=", "/input"}, want: "--module must not be empty"},
		{name: "mutually exclusive modes", args: []string{"add", "--template", "--scaffold", "/input"}, want: "must not be used together"},
		{name: "M1 template", args: []string{"add", "--template", "--dry-run", "/input"}, want: "requires M2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			stdout, stderr, code := runInjectedAdd(t, test.args, func(addrunner.RunOptions) (addrunner.Result, error) {
				called = true
				return addrunner.Result{}, nil
			})
			if code != exitError || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("add = stdout %q, stderr %q, exit %d; want early error %q", stdout, stderr, code, test.want)
			}
			if called {
				t.Fatal("add called mutation runner after an early flag error")
			}
		})
	}
}

func TestAdd_CommandAndFlagsAreRegistered(t *testing.T) {
	root, err := newRootCommand(environment{})
	if err != nil {
		t.Fatalf("newRootCommand() error = %v", err)
	}
	command, _, err := root.Find([]string{"add"})
	if err != nil || command == root {
		t.Fatalf("root.Find(add) = command %v, error %v", command, err)
	}
	for _, test := range []struct {
		name      string
		shorthand string
	}{
		{name: moduleFlagName, shorthand: "m"},
		{name: templateFlagName},
		{name: scaffoldFlagName},
		{name: dryRunFlagName, shorthand: "n"},
	} {
		flag := command.Flags().Lookup(test.name)
		if flag == nil || flag.Shorthand != test.shorthand {
			t.Errorf("add flag %q = %#v, want shorthand %q", test.name, flag, test.shorthand)
		}
	}
}

func TestAdd_InvalidResultWithoutErrorFailsClosed(t *testing.T) {
	stdout, stderr, code := runInjectedAdd(t, []string{"add", "/input"}, func(addrunner.RunOptions) (addrunner.Result, error) {
		return addrunner.Result{}, nil
	})
	if code != exitError || stdout != "" || !strings.Contains(stderr, addrunner.ErrExecutionProtocol.Error()) {
		t.Fatalf("invalid add result = stdout %q, stderr %q, exit %d; want protocol error", stdout, stderr, code)
	}
}

func TestAddExecutionIncomplete_RequiresStateAndEveryOutcome(t *testing.T) {
	for _, test := range []struct {
		name           string
		stateCommitted bool
		outcomes       []addrunner.ItemOutcome
		want           bool
	}{
		{name: "state missing", outcomes: []addrunner.ItemOutcome{{Status: addrunner.OutcomeSucceeded}}, want: true},
		{name: "succeeded", stateCommitted: true, outcomes: []addrunner.ItemOutcome{{Status: addrunner.OutcomeSucceeded}}},
		{name: "failed", stateCommitted: true, outcomes: []addrunner.ItemOutcome{{Status: addrunner.OutcomeFailed}}, want: true},
		{name: "deferred", stateCommitted: true, outcomes: []addrunner.ItemOutcome{{Status: addrunner.OutcomeDeferred}}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := addExecutionIncomplete(test.stateCommitted, test.outcomes); got != test.want {
				t.Fatalf("addExecutionIncomplete() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestAdd_DryRunIsReadOnlyAndAmbiguityExitsThree(t *testing.T) {
	t.Run("read only", func(t *testing.T) {
		fixture := newAddCLIFixture(t, []string{"alpha"})
		target := fixture.writeTarget(t, ".config/app/config", "user config\n")
		before := snapshotCLITree(t, fixture.root)

		stdout, stderr, code := fixture.run(t, "add", "--dry-run", "-m", "alpha", target)
		if code != exitActionable || stderr != "" {
			t.Fatalf("dry-run add = stdout %q, stderr %q, exit %d; want actionable", stdout, stderr, code)
		}
		for _, want := range []string{
			"repo=" + fixture.repository + " profile=all os=" + runtime.GOOS,
			"link  ~/.config/app/config  (add dry-run)",
		} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("dry-run stdout = %q, want %q", stdout, want)
			}
		}
		if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
			t.Fatalf("dry-run changed isolated tree\nbefore=%v\nafter=%v", before, after)
		}
	})

	t.Run("ambiguous module", func(t *testing.T) {
		fixture := newAddCLIFixture(t, []string{"alpha", "beta"})
		target := fixture.writeTarget(t, "shared", "ambiguous\n")
		before := snapshotCLITree(t, fixture.root)

		stdout, stderr, code := fixture.run(t, "add", "--dry-run", target)
		if code != exitConflict || stdout != "" || !strings.Contains(stderr, "specify -m") {
			t.Fatalf("ambiguous add = stdout %q, stderr %q, exit %d; want conflict", stdout, stderr, code)
		}
		if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
			t.Fatalf("ambiguous add changed isolated tree\nbefore=%v\nafter=%v", before, after)
		}
	})
}

func TestAdd_DevelopmentNoticeDoesNotChangeExitCode(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		name := "mutation"
		args := []string{"add", "-m", "alpha"}
		wantCode := exitOK
		if dryRun {
			name = "dry-run"
			args = append(args, "--dry-run")
			wantCode = exitActionable
		}
		t.Run(name, func(t *testing.T) {
			fixture := newAddCLIFixture(t, []string{"alpha"})
			target := fixture.writeTarget(t, "development", "content\n")
			argsWithTarget := append(append([]string(nil), args...), target)
			stdout, stderr, code := fixture.runWithBuild(t, buildinfo.Info{Version: "dev"}, argsWithTarget...)
			if code != wantCode || !strings.Contains(stdout, "~/development") ||
				!strings.Contains(stderr, "notice: development build skipped the requires version comparison") {
				t.Fatalf("development add = stdout %q, stderr %q, exit %d", stdout, stderr, code)
			}
		})
	}
}

func TestAdd_ExplicitModuleErrorsProvideManualManifestGuidance(t *testing.T) {
	fixture := newAddCLIFixture(t, []string{"alpha"})
	target := fixture.writeTarget(t, "guided", "content\n")

	_, stderr, code := fixture.run(t, "add", "--dry-run", "-m", "new-module", target)
	if code != exitError || !strings.Contains(stderr, "mkdir -p modules/new-module") ||
		!strings.Contains(stderr, `add "new-module" to [profiles].all`) {
		t.Fatalf("missing module guidance = stderr %q, exit %d", stderr, code)
	}

	makeDirectory(t, filepath.Join(fixture.repository, "modules", "beta"))
	_, stderr, code = fixture.run(t, "add", "--dry-run", "-m", "beta", target)
	if code != exitError || !strings.Contains(stderr, `module "beta" is not in the effective profile "all"`) ||
		!strings.Contains(stderr, `add "beta" to [profiles].all`) {
		t.Fatalf("inactive module guidance = stderr %q, exit %d", stderr, code)
	}
}

func TestAdd_LinkThenApplyIsImmediatelyConverged(t *testing.T) {
	fixture := newAddCLIFixture(t, []string{"alpha"})
	target := fixture.writeTarget(t, ".config/app/config", "user config\n")
	realHomeBefore := snapshotCLIPath(t, fixture.realHome)

	stdout, stderr, code := fixture.run(t, "add", "-m", "alpha", target)
	if code != exitOK || stderr != "" {
		t.Fatalf("link add = stdout %q, stderr %q, exit %d; want success", stdout, stderr, code)
	}
	for _, want := range []string{
		"repo=" + fixture.repository + " profile=all os=" + runtime.GOOS,
		"link  ~/.config/app/config  (added)",
		"Next: run git add for the published source paths, then git commit.",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("link add stdout = %q, want %q", stdout, want)
		}
	}
	source := filepath.Join(fixture.repository, "modules", "alpha", ".config", "app", "config")
	if destination, err := os.Readlink(target); err != nil || destination != source {
		t.Fatalf("link add target = %q, %v; want %q", destination, err, source)
	}
	if content, err := os.ReadFile(source); err != nil || string(content) != "user config\n" {
		t.Fatalf("link add source = %q, %v", content, err)
	}
	tracked := exec.Command("git", "-C", fixture.repository, "ls-files", "--error-unmatch", "--", "modules/alpha/.config/app/config")
	tracked.Env = os.Environ()
	if output, err := tracked.CombinedOutput(); err == nil {
		t.Fatalf("dot add staged or tracked its source unexpectedly: %s", output)
	}
	state := readPlanState(t, fixture.home)
	if entry, ok := state.Entries["~/.config/app/config"]; !ok || entry.Kind != "symlink" || entry.Module != "alpha" {
		t.Fatalf("link add state = %#v", entry)
	}
	afterAdd := snapshotCLITree(t, fixture.root)

	stdout, stderr, code = fixture.run(t, "apply")
	if code != exitOK || stderr != "" || !strings.HasSuffix(stdout, "Already up to date.\n") {
		t.Fatalf("apply after add = stdout %q, stderr %q, exit %d; want converged", stdout, stderr, code)
	}
	if afterApply := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(afterApply, afterAdd) {
		t.Fatalf("apply after add changed isolated tree\nafter add=%v\nafter apply=%v", afterAdd, afterApply)
	}
	if realHomeAfter := snapshotCLIPath(t, fixture.realHome); !reflect.DeepEqual(realHomeAfter, realHomeBefore) {
		t.Fatalf("add/apply changed real HOME sentinel: before=%q after=%q", realHomeBefore, realHomeAfter)
	}
}

func TestAdd_LinkBreaksHardLinkSharing(t *testing.T) {
	fixture := newAddCLIFixture(t, []string{"alpha"})
	target := fixture.writeTarget(t, "hardlink/managed", "shared bytes\n")
	sibling := filepath.Join(fixture.home, "hardlink", "sibling")
	if err := os.Link(target, sibling); err != nil {
		t.Fatalf("create hard-link sibling: %v", err)
	}
	siblingBefore, err := os.Lstat(sibling)
	if err != nil {
		t.Fatalf("lstat sibling: %v", err)
	}

	stdout, stderr, code := fixture.run(t, "add", "-m", "alpha", target)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "link  ~/hardlink/managed  (added)") {
		t.Fatalf("hard-link add = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	siblingAfter, err := os.Lstat(sibling)
	if err != nil || !os.SameFile(siblingBefore, siblingAfter) {
		t.Fatalf("hard-link sibling identity changed: before=%v after=%v err=%v", siblingBefore, siblingAfter, err)
	}
	if content, err := os.ReadFile(sibling); err != nil || string(content) != "shared bytes\n" {
		t.Fatalf("hard-link sibling = %q, %v", content, err)
	}
	if _, err := os.Readlink(target); err != nil {
		t.Fatalf("add target is not symlink: %v", err)
	}
	source := filepath.Join(fixture.repository, "modules", "alpha", "hardlink", "managed")
	sourceInfo, err := os.Lstat(source)
	if err != nil || os.SameFile(sourceInfo, siblingAfter) {
		t.Fatalf("published source shares target inode: source=%v sibling=%v err=%v", sourceInfo, siblingAfter, err)
	}
}

func TestAdd_MultipleInputsCommitsSuccessfulPrefix(t *testing.T) {
	fixture := newAddCLIFixture(t, []string{"alpha"})
	first := fixture.writeTarget(t, "a-first", "first\n")
	blockedParent := filepath.Join(fixture.home, "z-blocked")
	second := fixture.writeTarget(t, "z-blocked/second", "second\n")
	if err := os.Chmod(blockedParent, 0o500); err != nil {
		t.Fatalf("make second target parent read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blockedParent, 0o700) })

	stdout, stderr, code := fixture.run(t, "add", "-m", "alpha", first, second)
	if code != exitError || !strings.Contains(stderr, "permission denied") {
		t.Fatalf("partial add = stdout %q, stderr %q, exit %d; want second mutation error", stdout, stderr, code)
	}
	for _, want := range []string{
		"link  ~/a-first  (added)",
		"skip  ~/z-blocked/second  (add failed)",
		"Next: run git add for the published source paths, then git commit.",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("partial add stdout = %q, want %q", stdout, want)
		}
	}
	if _, err := os.Readlink(first); err != nil {
		t.Fatalf("successful prefix target did not commit: %v", err)
	}
	if content, err := os.ReadFile(second); err != nil || string(content) != "second\n" {
		t.Fatalf("failed suffix target = %q, %v; want original", content, err)
	}
	state := readPlanState(t, fixture.home)
	if _, ok := state.Entries["~/a-first"]; !ok {
		t.Fatal("successful prefix state was not committed")
	}
	if _, ok := state.Entries["~/z-blocked/second"]; ok {
		t.Fatal("failed suffix was committed to state")
	}
}

func TestAdd_ScaffoldKeepsTargetAndApplyDoesNotRebuild(t *testing.T) {
	fixture := newAddCLIFixture(t, []string{"alpha"})
	writeCLIFile(t, filepath.Join(fixture.repository, "modules", "alpha", "dot.toml"), `[files.".config/app/bootstrap.template"]
mode = "0600"
`)
	target := fixture.writeTarget(t, ".config/app/bootstrap", "user bootstrap\n")
	targetBefore, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("lstat target: %v", err)
	}

	stdout, stderr, code := fixture.run(t, "add", "--scaffold", "-m", "alpha", target)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "scaffold  ~/.config/app/bootstrap  (added)") {
		t.Fatalf("scaffold add = stdout %q, stderr %q, exit %d; want success", stdout, stderr, code)
	}
	targetAfter, err := os.Lstat(target)
	if err != nil || !os.SameFile(targetBefore, targetAfter) {
		t.Fatalf("scaffold add replaced target: before=%v after=%v err=%v", targetBefore, targetAfter, err)
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "user bootstrap\n" {
		t.Fatalf("scaffold target = %q, %v", content, err)
	}
	source := filepath.Join(fixture.repository, "modules", "alpha", ".config", "app", "bootstrap.template")
	if content, err := os.ReadFile(source); err != nil || string(content) != "user bootstrap\n" {
		t.Fatalf("scaffold source = %q, %v", content, err)
	}
	if entry := readPlanState(t, fixture.home).Entries["~/.config/app/bootstrap"]; entry.Kind != "scaffold" {
		t.Fatalf("scaffold state = %#v", entry)
	}
	afterAdd := snapshotCLITree(t, fixture.root)

	stdout, stderr, code = fixture.run(t, "apply")
	if code != exitOK || stderr != "" || !strings.HasSuffix(stdout, "Already up to date.\n") {
		t.Fatalf("apply after scaffold add = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	if afterApply := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(afterApply, afterAdd) {
		t.Fatalf("apply after scaffold add changed tree\nafter add=%v\nafter apply=%v", afterAdd, afterApply)
	}

	if err := os.Remove(target); err != nil {
		t.Fatalf("remove user-owned scaffold target: %v", err)
	}
	stdout, stderr, code = fixture.run(t, "apply")
	if code != exitActionable || !strings.Contains(stderr, "scaffold target was deleted") {
		t.Fatalf("apply after scaffold deletion = stdout %q, stderr %q, exit %d", stdout, stderr, code)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("apply rebuilt deleted scaffold target: %v", err)
	}
}

type addCLIFixture struct {
	root       string
	home       string
	repository string
	realHome   string
}

func newAddCLIFixture(t *testing.T, modules []string) addCLIFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	realHome := filepath.Join(root, "real-home")
	makeDirectory(t, home)
	makeDirectory(t, repository)
	makeDirectory(t, realHome)
	writeCLIFile(t, filepath.Join(realHome, "sentinel"), "unchanged\n")
	writeCLIFile(t, filepath.Join(home, ".config", "dot", "config.toml"), "profile = \"all\"\n")
	quoted := make([]string, len(modules))
	for index, module := range modules {
		quoted[index] = `"` + module + `"`
		makeDirectory(t, filepath.Join(repository, "modules", module))
	}
	writeCLIFile(t, filepath.Join(repository, "dot.toml"),
		"requires = \">=0.0.0\"\n[profiles]\nall = ["+strings.Join(quoted, ", ")+"]\n")
	fixture := addCLIFixture{root: root, home: home, repository: repository, realHome: realHome}
	fixture.setEnvironment(t)
	runGitForAddFixture(t, repository, "init", "--quiet")
	runGitForAddFixture(t, repository, "add", "--", "dot.toml")
	return fixture
}

func (fixture addCLIFixture) writeTarget(t *testing.T, relative, content string) string {
	t.Helper()
	target := filepath.Join(fixture.home, filepath.FromSlash(relative))
	writeCLIFile(t, target, content)
	return target
}

func (fixture addCLIFixture) setEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", fixture.realHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fixture.home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(fixture.home, ".local", "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(fixture.home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(fixture.home, ".cache"))
	t.Setenv("DOT_CONFIG", filepath.Join(fixture.home, ".config", "dot", "config.toml"))
	t.Setenv("DOT_REPO", fixture.repository)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(fixture.root, "missing-global-gitconfig"))
}

func (fixture addCLIFixture) run(t *testing.T, commandArgs ...string) (string, string, int) {
	t.Helper()
	return fixture.runWithBuild(t, buildinfo.Info{Version: "v0.0.0", Commit: "test", BuildTime: "test"}, commandArgs...)
}

func (fixture addCLIFixture) runWithBuild(
	t *testing.T,
	build buildinfo.Info,
	commandArgs ...string,
) (string, string, int) {
	t.Helper()
	fixture.setEnvironment(t)
	args := append([]string(nil), commandArgs...)
	args = append(args, "--home", fixture.home, "--repo", fixture.repository)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(args, environment{
		stdout:      &stdout,
		stderr:      &stderr,
		lookupEnv:   os.LookupEnv,
		userHomeDir: os.UserHomeDir,
		build:       build,
		goos:        runtime.GOOS,
	})
	return stdout.String(), stderr.String(), code
}

func runInjectedAdd(
	t *testing.T,
	args []string,
	runner func(addrunner.RunOptions) (addrunner.Result, error),
) (string, string, int) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(args, environment{
		stdout:      &stdout,
		stderr:      &stderr,
		lookupEnv:   os.LookupEnv,
		userHomeDir: os.UserHomeDir,
		build:       buildinfo.Info{Version: "v0.0.0"},
		goos:        runtime.GOOS,
		addRun:      runner,
		addLoad: func(dotruntime.Overrides, string) (dotruntime.LoadedInputs, error) {
			return dotruntime.LoadedInputs{}, errors.New("unexpected injected add load")
		},
	})
	return stdout.String(), stderr.String(), code
}

func runGitForAddFixture(t *testing.T, repository string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", repository}, args...)
	command := exec.Command("git", commandArgs...)
	command.Env = os.Environ()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
