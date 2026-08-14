package cli

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mianm12/dotfiles/internal/buildinfo"
	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/storage"
)

type cliTestEnv struct {
	root       string
	home       string
	repository string
	config     string
	state      string
	lock       string
	env        environment
}

func newCLITestEnv(t *testing.T, profiles string) *cliTestEnv {
	t.Helper()
	root := t.TempDir()
	fixture := &cliTestEnv{
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
	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "dot.toml"),
		"version = 1\n[profiles]\n"+profiles+"\n",
	)
	fixture.env = environment{
		stdin: strings.NewReader(""),
		getwd: func() (string, error) {
			return fixture.repository, nil
		},
		userHomeDir: func() (string, error) {
			return fixture.home, nil
		},
		platform: func() config.Platform {
			return cliTestPlatform("linux", "ubuntu", "x86_64")
		},
		build: buildinfo.Info{Version: "test", Commit: "test", BuildTime: "test"},
	}
	paths := map[string]string{
		"root":       fixture.root,
		"HOME":       fixture.home,
		"repository": fixture.repository,
		"config":     fixture.config,
		"state":      fixture.state,
		"lock":       fixture.lock,
	}
	for name, path := range paths {
		if !filepath.IsAbs(path) {
			t.Fatalf("%s path %q is not absolute", name, path)
		}
		relative, err := filepath.Rel(fixture.root, path)
		if err != nil ||
			relative == ".." ||
			strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Fatalf("%s path %q is outside synthetic root %q", name, path, fixture.root)
		}
	}
	t.Setenv("HOME", fixture.home)
	return fixture
}

func cliTestPlatform(osName, distro, architecture string) config.Platform {
	return config.Platform{
		OS:     cliTestPlatformField("os", osName),
		Distro: cliTestPlatformField("distro", distro),
		Arch:   cliTestPlatformField("arch", architecture),
	}
}

func cliTestPlatformField(name, value string) config.PlatformField {
	if value != "" {
		return config.KnownPlatformField(value)
	}
	return config.UnknownPlatformField(name + " is unavailable in test")
}

func (fixture *cliTestEnv) run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(args, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func (fixture *cliTestEnv) runInjected(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	env := fixture.env
	env.stdout = &stdout
	env.stderr = &stderr
	code := run(args, env)
	return code, stdout.String(), stderr.String()
}

func (fixture *cliTestEnv) runProcess(args ...string) (int, string, string) {
	return fixture.runProcessAt("", args...)
}

func (fixture *cliTestEnv) runProcessAt(
	directory string,
	args ...string,
) (int, string, string) {
	commandArgs := []string{"-test.run=^TestCLIHelperProcess$", "--"}
	commandArgs = append(commandArgs, args...)
	processContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(processContext, os.Args[0], commandArgs...)
	command.Dir = directory
	command.Env = append(os.Environ(), "DOT_CLI_TEST_HELPER_PROCESS=1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	if processContext.Err() != nil {
		return -1, stdout.String(), stderr.String() + processContext.Err().Error()
	}
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
	if os.Getenv("DOT_CLI_TEST_HELPER_PROCESS") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		os.Exit(exitUsage)
	}
	os.Exit(Run(os.Args[separator+1:], os.Stdin, os.Stdout, os.Stderr))
}

func (fixture *cliTestEnv) writeMachine(
	t *testing.T,
	profiles, extras []string,
) {
	t.Helper()
	publishTestMachine(t, fixture.config, config.Machine{
		Version:      1,
		Repository:   fixture.repository,
		Profiles:     append([]string(nil), profiles...),
		ExtraModules: append([]string(nil), extras...),
	})
}

func publishTestMachine(t *testing.T, path string, machine config.Machine) {
	t.Helper()
	data, err := config.MarshalMachine(machine)
	if err != nil {
		t.Fatalf("MarshalMachine() error = %v", err)
	}
	if _, err := storage.PublishPrivateFile(path, data); err != nil {
		t.Fatalf("PublishPrivateFile(machine) error = %v", err)
	}
}

func (fixture *cliTestEnv) loadMachine(t *testing.T) config.Machine {
	t.Helper()
	machine, exists, err := config.LoadMachine(fixture.config)
	if err != nil || !exists {
		t.Fatalf("LoadMachine() = (%#v, %t, %v)", machine, exists, err)
	}
	return machine
}

func (fixture *cliTestEnv) writeModule(
	t *testing.T,
	id, manifest string,
	files map[string]string,
) {
	t.Helper()
	root := filepath.Join(fixture.repository, "modules", id)
	writeCLIFile(
		t,
		filepath.Join(root, "module.toml"),
		strings.TrimSpace(manifest)+"\n",
	)
	for relative, content := range files {
		writeCLIFile(t, filepath.Join(root, filepath.FromSlash(relative)), content)
	}
}

func (fixture *cliTestEnv) writeState(t *testing.T, snapshot state.Snapshot) {
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

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) {
	return 0, errors.New("synthetic stdout failure")
}

func runWithFailedStdout(t *testing.T, args []string) string {
	t.Helper()
	var stderr bytes.Buffer
	code := Run(args, strings.NewReader(""), failedWriter{}, &stderr)
	if code != exitError {
		t.Fatalf("Run(%q) = %d, want %d", args, code, exitError)
	}
	return stderr.String()
}

func assertOutputFailure(t *testing.T, stderr, rerun string) {
	t.Helper()
	if !strings.Contains(stderr, "may be partially complete") ||
		!strings.Contains(stderr, "result output failed") ||
		!strings.Contains(stderr, "synthetic stdout failure") ||
		!strings.Contains(stderr, "rerun "+rerun) {
		t.Fatalf("stderr = %q, want partial-completion advice for %q", stderr, rerun)
	}
}

type pathSnapshot struct {
	path     string
	info     fs.FileInfo
	mode     fs.FileMode
	data     string
	link     string
	modified int64
	size     int64
}

type filesystemSnapshot struct {
	root    string
	entries []pathSnapshot
}

func snapshotTree(t *testing.T, root string) filesystemSnapshot {
	t.Helper()
	var paths []string
	if err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	}); err != nil {
		t.Fatalf("filepath.WalkDir(%q) error = %v", root, err)
	}
	return filesystemSnapshot{root: root, entries: snapshotExactPaths(t, paths...)}
}

func snapshotPaths(t *testing.T, paths ...string) filesystemSnapshot {
	t.Helper()
	return filesystemSnapshot{entries: snapshotExactPaths(t, paths...)}
}

func snapshotExactPaths(t *testing.T, paths ...string) []pathSnapshot {
	t.Helper()
	result := make([]pathSnapshot, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("os.Lstat(%q) error = %v", path, err)
		}
		entry := pathSnapshot{
			path:     path,
			info:     info,
			mode:     info.Mode(),
			modified: info.ModTime().UnixNano(),
			size:     info.Size(),
		}
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

func assertSnapshotUnchanged(t *testing.T, before filesystemSnapshot) {
	t.Helper()
	var after []pathSnapshot
	if before.root != "" {
		after = snapshotTree(t, before.root).entries
	} else {
		paths := make([]string, len(before.entries))
		for index := range before.entries {
			paths[index] = before.entries[index].path
		}
		after = snapshotExactPaths(t, paths...)
	}
	assertSnapshotEntriesEqual(t, before.entries, after)
}

func assertOnlyLockBookkeepingChanged(
	t *testing.T,
	before filesystemSnapshot,
	fixture *cliTestEnv,
) {
	t.Helper()
	after := snapshotTree(t, before.root).entries
	stateRoot := filepath.Dir(fixture.state)
	ignore := func(entry pathSnapshot) bool {
		if entry.path == fixture.lock {
			return true
		}
		fromHome, err := filepath.Rel(fixture.home, entry.path)
		if err != nil || fromHome == ".." ||
			strings.HasPrefix(fromHome, ".."+string(filepath.Separator)) {
			return false
		}
		toStateRoot, err := filepath.Rel(entry.path, stateRoot)
		return err == nil && toStateRoot != ".." &&
			!strings.HasPrefix(toStateRoot, ".."+string(filepath.Separator))
	}
	filter := func(entries []pathSnapshot) []pathSnapshot {
		filtered := make([]pathSnapshot, 0, len(entries))
		for _, entry := range entries {
			if !ignore(entry) {
				filtered = append(filtered, entry)
			}
		}
		return filtered
	}
	assertSnapshotEntriesEqual(t, filter(before.entries), filter(after))
	if info, err := os.Lstat(fixture.lock); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("lock bookkeeping = (%v, %v), want regular file", info, err)
	}
}

func assertSnapshotEntriesEqual(t *testing.T, before, after []pathSnapshot) {
	t.Helper()
	if len(after) != len(before) {
		t.Fatalf(
			"filesystem entry count changed: before=%d after=%d",
			len(before),
			len(after),
		)
	}
	for index := range before {
		oldEntry := before[index]
		newEntry := after[index]
		if oldEntry.path != newEntry.path ||
			oldEntry.mode != newEntry.mode ||
			oldEntry.data != newEntry.data ||
			oldEntry.link != newEntry.link ||
			oldEntry.modified != newEntry.modified ||
			oldEntry.size != newEntry.size ||
			!os.SameFile(oldEntry.info, newEntry.info) {
			t.Fatalf(
				"path changed\nbefore=%#v\nafter=%#v",
				oldEntry,
				newEntry,
			)
		}
	}
}

func assertApplyNoMutation(
	t *testing.T,
	fixture *cliTestEnv,
	run func(...string) (int, string, string),
	module ...string,
) {
	t.Helper()
	before := snapshotTree(t, fixture.root)
	args := append([]string{"apply"}, module...)
	code, stdout, stderr := run(args...)
	if code != exitOK || stderr != "" {
		t.Fatalf("repeated apply = (%d, %q, %q)", code, stdout, stderr)
	}
	assertCLINoMutationResult(t, stdout)
	assertSnapshotUnchanged(t, before)
}

func assertCLINoMutationResult(t *testing.T, stdout string) {
	t.Helper()
	if !strings.Contains(stdout, "converged") {
		t.Fatalf("stdout = %q, want converged", stdout)
	}
}

func writeModuleManifest(t *testing.T, fixture *cliTestEnv, id, manifest string) {
	t.Helper()
	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "modules", id, "module.toml"),
		strings.TrimSpace(manifest)+"\n",
	)
}

func loadTestState(t *testing.T, fixture *cliTestEnv) state.Snapshot {
	t.Helper()
	loaded, err := state.Load(fixture.state, fixture.home)
	if err != nil {
		t.Fatalf("state.Load() error = %v", err)
	}
	return loaded.Snapshot
}

func writeLinkState(
	t *testing.T,
	fixture *cliTestEnv,
	target, destination string,
) {
	t.Helper()
	relative, err := filepath.Rel(fixture.home, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("target %q is outside HOME %q", target, fixture.home)
	}
	fixture.writeState(t, state.Snapshot{
		Home: fixture.home,
		Links: map[state.Key]state.LinkRecord{
			{ModuleID: "app", PlacementID: "config"}: {
				Target: filepath.ToSlash(relative),
				Dest:   destination,
			},
		},
	})
}
