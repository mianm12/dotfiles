package executor

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mianm12/dotfiles/internal/planner"
)

func TestExecuteHook_DirectAndShellUseCanonicalRuntime(t *testing.T) {
	for _, test := range []struct {
		name string
		mode fs.FileMode
	}{
		{name: "direct", mode: 0o755},
		{name: "shell", mode: 0o644},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHookExecutorFixture(t, test.mode, `#!/bin/sh
printf '%s\n' "$PWD|$PARENT_MARKER|$HOME|$XDG_CONFIG_HOME|$XDG_STATE_HOME|$XDG_DATA_HOME|$DOT_MODULE|$DOT_OS|$DOT_PROFILE|$DOT_REPO|$DOT_TARGET"
IFS= read -r value
printf 'stdout:%s\n' "$value"
printf 'stderr:%s\n' "$value" >&2
printf touched > "$HOME/hook-marker"
`)
			t.Setenv("PARENT_MARKER", "inherited")
			for _, key := range hookEnvironmentKeys {
				t.Setenv(key, "must-be-overridden")
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			result, err := ExecuteHook(fixture.action, HookStreams{
				Stdin:  strings.NewReader("input\n"),
				Stdout: &stdout,
				Stderr: &stderr,
			})
			if err != nil {
				t.Fatalf("ExecuteHook() error = %v", err)
			}
			if result.StateEffect != fixture.action.OnSuccess {
				t.Fatalf("ExecuteHook() state effect = %#v, want %#v", result.StateEffect, fixture.action.OnSuccess)
			}
			resolvedModule, resolveErr := filepath.EvalSymlinks(fixture.module)
			if resolveErr != nil {
				t.Fatalf("resolve module path: %v", resolveErr)
			}
			wantEnvironment := strings.Join([]string{
				resolvedModule,
				"inherited",
				fixture.home,
				filepath.Join(fixture.home, ".config"),
				filepath.Join(fixture.home, ".local", "state"),
				filepath.Join(fixture.home, ".local", "share"),
				fixture.action.Module,
				fixture.action.GOOS,
				fixture.action.Profile,
				fixture.repository,
				fixture.target,
			}, "|") + "\n"
			if got := stdout.String(); got != wantEnvironment+"stdout:input\n" {
				t.Fatalf("stdout = %q, want %q", got, wantEnvironment+"stdout:input\n")
			}
			if got := stderr.String(); got != "stderr:input\n" {
				t.Fatalf("stderr = %q, want realtime child stderr", got)
			}
			if content, readErr := os.ReadFile(filepath.Join(fixture.home, "hook-marker")); readErr != nil || string(content) != "touched" {
				t.Fatalf("synthetic HOME marker = %q, %v", content, readErr)
			}
		})
	}
}

func TestExecuteHook_StreamsOutputBeforeProcessExit(t *testing.T) {
	fixture := newHookExecutorFixture(t, 0o644, `printf ready
IFS= read -r value
printf ':done:%s' "$value"
`)
	stdinReader, stdinWriter := io.Pipe()
	stdout := newSignalWriter("ready")
	resultChannel := make(chan planner.HookStateEffect, 1)
	errorChannel := make(chan error, 1)
	go func() {
		result, err := ExecuteHook(fixture.action, HookStreams{
			Stdin:  stdinReader,
			Stdout: stdout,
			Stderr: io.Discard,
		})
		resultChannel <- result.StateEffect
		errorChannel <- err
	}()

	select {
	case <-stdout.signal:
	case <-time.After(3 * time.Second):
		_ = stdinWriter.CloseWithError(errors.New("test timed out"))
		t.Fatal("hook stdout was not forwarded before process exit")
	}
	if _, err := io.WriteString(stdinWriter, "continue\n"); err != nil {
		t.Fatalf("write hook stdin: %v", err)
	}
	if err := stdinWriter.Close(); err != nil {
		t.Fatalf("close hook stdin: %v", err)
	}
	if err := <-errorChannel; err != nil {
		t.Fatalf("ExecuteHook() error = %v", err)
	}
	if effect := <-resultChannel; effect != fixture.action.OnSuccess {
		t.Fatalf("state effect = %#v, want %#v", effect, fixture.action.OnSuccess)
	}
	if got := stdout.String(); got != "ready:done:continue" {
		t.Fatalf("stdout = %q, want realtime complete output", got)
	}
}

func TestExecuteHook_RevalidatesScriptBeforeLaunch(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, hookExecutorFixture)
	}{
		{
			name: "bytes changed",
			mutate: func(t *testing.T, fixture hookExecutorFixture) {
				writeHookExecutorFile(t, fixture.action.ScriptPath, "printf changed > \"$HOME/launched\"\n", 0o644)
			},
		},
		{
			name: "execution class changed",
			mutate: func(t *testing.T, fixture hookExecutorFixture) {
				if err := os.Chmod(fixture.action.ScriptPath, 0o755); err != nil {
					t.Fatalf("chmod hook: %v", err)
				}
			},
		},
		{
			name: "not regular",
			mutate: func(t *testing.T, fixture hookExecutorFixture) {
				if err := os.Remove(fixture.action.ScriptPath); err != nil {
					t.Fatalf("remove hook: %v", err)
				}
				if err := os.Mkdir(fixture.action.ScriptPath, 0o755); err != nil {
					t.Fatalf("replace hook with directory: %v", err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHookExecutorFixture(t, 0o644, "printf launched > \"$HOME/launched\"\n")
			test.mutate(t, fixture)
			result, err := ExecuteHook(fixture.action, HookStreams{
				Stdin:  strings.NewReader(""),
				Stdout: io.Discard,
				Stderr: io.Discard,
			})
			if !errors.Is(err, ErrHookPrecondition) {
				t.Fatalf("ExecuteHook() error = %v, want ErrHookPrecondition", err)
			}
			if result.StateEffect != fixture.action.OnFailure {
				t.Fatalf("state effect = %#v, want %#v", result.StateEffect, fixture.action.OnFailure)
			}
			if _, statErr := os.Lstat(filepath.Join(fixture.home, "launched")); !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("hook launched despite failed precondition: %v", statErr)
			}
		})
	}
}

func TestExecuteHook_FailureAndSkipPreserveState(t *testing.T) {
	fixture := newHookExecutorFixture(t, 0o644, "printf failure >&2\nexit 23\n")
	var stderr bytes.Buffer
	result, err := ExecuteHook(fixture.action, HookStreams{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: &stderr,
	})
	if err == nil || stderr.String() != "failure" {
		t.Fatalf("failed hook = stderr %q, error %v", stderr.String(), err)
	}
	if result.StateEffect != fixture.action.OnFailure {
		t.Fatalf("failed state effect = %#v, want %#v", result.StateEffect, fixture.action.OnFailure)
	}

	fixture.action.Verb = planner.HookSkip
	fixture.action.OnSuccess = planner.HookStateEffect{Kind: planner.HookStatePreserve}
	result, err = ExecuteHook(fixture.action, HookStreams{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if !errors.Is(err, ErrUnsupportedHookAction) {
		t.Fatalf("skipped ExecuteHook() error = %v, want ErrUnsupportedHookAction", err)
	}
	if result.StateEffect != fixture.action.OnFailure {
		t.Fatalf("skipped state effect = %#v, want %#v", result.StateEffect, fixture.action.OnFailure)
	}
}

func TestMergeHookEnvironment_OverridesEachCanonicalKeyExactlyOnce(t *testing.T) {
	environment := planner.HookEnvironment{
		Home:          "/fixture/home",
		XDGConfigHome: "/fixture/home/.config",
		XDGStateHome:  "/fixture/home/.local/state",
		XDGDataHome:   "/fixture/home/.local/share",
		DotModule:     "app",
		DotOS:         "linux",
		DotProfile:    "work",
		DotRepo:       "/fixture/repo",
		DotTarget:     "/fixture/home/app",
	}
	got := mergeHookEnvironment([]string{
		"PATH=/bin",
		"HOME=/old-one",
		"HOME=/old-two",
		"DOT_MODULE=old",
		"PARENT=value",
	}, environment)
	want := []string{
		"PATH=/bin",
		"PARENT=value",
		"HOME=/fixture/home",
		"XDG_CONFIG_HOME=/fixture/home/.config",
		"XDG_STATE_HOME=/fixture/home/.local/state",
		"XDG_DATA_HOME=/fixture/home/.local/share",
		"DOT_MODULE=app",
		"DOT_OS=linux",
		"DOT_PROFILE=work",
		"DOT_REPO=/fixture/repo",
		"DOT_TARGET=/fixture/home/app",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeHookEnvironment() = %#v, want %#v", got, want)
	}
}

type hookExecutorFixture struct {
	home       string
	repository string
	module     string
	target     string
	action     planner.HookAction
}

func newHookExecutorFixture(t *testing.T, mode fs.FileMode, content string) hookExecutorFixture {
	t.Helper()
	protectRealHookHome(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repo")
	module := filepath.Join(repository, "modules", "app")
	target := filepath.Join(home, "app")
	script := filepath.Join(module, "setup.sh")
	for _, directory := range []string{home, module, target} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", directory, err)
		}
	}
	writeHookExecutorFile(t, script, content, mode)
	executionMode := planner.HookExecutionShell
	invocation := planner.HookInvocation{Mode: executionMode, Program: "sh", Arguments: []string{script}}
	if mode.Perm()&0o111 != 0 {
		executionMode = planner.HookExecutionDirect
		invocation = planner.HookInvocation{Mode: executionMode, Program: script}
	}
	fingerprint := planner.HookFingerprint(executionMode, []byte(content))
	action := planner.HookAction{
		Verb:           planner.HookRun,
		StateKey:       "app/setup.sh",
		Module:         "app",
		Script:         "setup.sh",
		ScriptPath:     script,
		WorkingDir:     module,
		TargetRoot:     "~/app",
		TargetRootPath: target,
		Profile:        "work",
		GOOS:           runtime.GOOS,
		Repository:     repository,
		Invocation:     invocation,
		Environment: planner.HookEnvironment{
			Home:          home,
			XDGConfigHome: filepath.Join(home, ".config"),
			XDGStateHome:  filepath.Join(home, ".local", "state"),
			XDGDataHome:   filepath.Join(home, ".local", "share"),
			DotModule:     "app",
			DotOS:         runtime.GOOS,
			DotProfile:    "work",
			DotRepo:       repository,
			DotTarget:     target,
		},
		Fingerprint: fingerprint,
		OnSuccess: planner.HookStateEffect{
			Kind:        planner.HookStateUpsert,
			Key:         "app/setup.sh",
			Fingerprint: fingerprint,
		},
		OnFailure: planner.HookStateEffect{Kind: planner.HookStatePreserve},
	}
	return hookExecutorFixture{home: home, repository: repository, module: module, target: target, action: action}
}

type hookPathMetadata struct {
	exists  bool
	mode    fs.FileMode
	size    int64
	modTime time.Time
	info    fs.FileInfo
}

func protectRealHookHome(t *testing.T) {
	t.Helper()
	realHome, err := os.UserHomeDir()
	if err != nil || realHome == "" || !filepath.IsAbs(realHome) {
		t.Fatalf("resolve real HOME: %q, %v", realHome, err)
	}
	paths := []string{realHome, filepath.Join(realHome, "hook-marker"), filepath.Join(realHome, "launched")}
	before := make(map[string]hookPathMetadata, len(paths))
	for _, path := range paths {
		before[path] = snapshotHookPath(t, path)
	}
	t.Cleanup(func() {
		for _, path := range paths {
			after := snapshotHookPath(t, path)
			if before[path].exists != after.exists {
				t.Errorf("real HOME sentinel %q existence changed", path)
				continue
			}
			if !after.exists {
				continue
			}
			previous := before[path]
			if !os.SameFile(previous.info, after.info) || previous.mode != after.mode ||
				previous.size != after.size || !previous.modTime.Equal(after.modTime) {
				t.Errorf("real HOME sentinel %q metadata changed", path)
			}
		}
	})
}

func snapshotHookPath(t *testing.T, path string) hookPathMetadata {
	t.Helper()
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return hookPathMetadata{}
	}
	if err != nil {
		t.Fatalf("inspect real HOME sentinel %q: %v", path, err)
	}
	return hookPathMetadata{
		exists:  true,
		mode:    info.Mode(),
		size:    info.Size(),
		modTime: info.ModTime(),
		info:    info,
	}
}

func writeHookExecutorFile(t *testing.T, path, content string, mode fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %q: %v", path, err)
	}
}

type signalWriter struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
	want   string
	signal chan struct{}
	once   sync.Once
}

func newSignalWriter(want string) *signalWriter {
	return &signalWriter{want: want, signal: make(chan struct{})}
}

func (writer *signalWriter) Write(content []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	written, err := writer.buffer.Write(content)
	if strings.Contains(writer.buffer.String(), writer.want) {
		writer.once.Do(func() { close(writer.signal) })
	}
	return written, err
}

func (writer *signalWriter) String() string {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.buffer.String()
}
