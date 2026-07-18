package lock

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mianm12/dotfiles/internal/storage"
)

const (
	helperEnvironment = "DOT_PROCESS_LOCK_HELPER"
	helperRootEnv     = "DOT_PROCESS_LOCK_ROOT"
	helperPathEnv     = "DOT_PROCESS_LOCK_PATH"
	helperActionEnv   = "DOT_PROCESS_LOCK_ACTION"
	probeBusyExitCode = 42
	crashExitCode     = 23
)

func TestProcessLockHelper(t *testing.T) {
	if os.Getenv(helperEnvironment) != "1" {
		t.Skip("helper process only")
	}

	owner, err := Acquire(os.Getenv(helperRootEnv), os.Getenv(helperPathEnv))
	if err != nil {
		if errors.Is(err, ErrBusy) && os.Getenv(helperActionEnv) == "probe" {
			os.Exit(probeBusyExitCode)
		}
		_, _ = fmt.Fprintf(os.Stderr, "helper acquire: %v\n", err)
		os.Exit(2)
	}

	switch os.Getenv(helperActionEnv) {
	case "probe":
		if err := owner.Release(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "helper release: %v\n", err)
			os.Exit(3)
		}
		return
	case "crash":
		_, _ = fmt.Fprintln(os.Stdout, "ready")
		os.Exit(crashExitCode)
	case "hold":
		_, _ = fmt.Fprintln(os.Stdout, "ready")
		var signal [1]byte
		if _, err := os.Stdin.Read(signal[:]); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "helper wait: %v\n", err)
			os.Exit(4)
		}
		if err := owner.Release(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "helper release: %v\n", err)
			os.Exit(5)
		}
		return
	default:
		_, _ = fmt.Fprintln(os.Stderr, "unknown helper action")
		os.Exit(6)
	}
}

func TestAcquire_CrossProcessBusyAndRelease(t *testing.T) {
	root, path := lockFixturePaths(t)
	holder := startHolder(t, root, path, "hold")

	owner, err := Acquire(root, path)
	if owner != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("Acquire() = (%#v, %v), want nil ErrBusy", owner, err)
	}
	assertPathMode(t, root, storage.PrivateDirectoryMode)
	assertPathMode(t, path, storage.PrivateFileMode)

	holder.release(t)
	owner, err = Acquire(root, path)
	if err != nil {
		t.Fatalf("Acquire() after release error = %v", err)
	}
	if err := owner.Release(); err != nil {
		t.Fatalf("Ownership.Release() error = %v", err)
	}
}

func TestAcquire_RecoversAfterHolderProcessCrash(t *testing.T) {
	root, path := lockFixturePaths(t)
	holder := startHolder(t, root, path, "crash")
	holder.waitForExitCode(t, crashExitCode)

	owner, err := Acquire(root, path)
	if err != nil {
		t.Fatalf("Acquire() after process crash error = %v", err)
	}
	if err := owner.Release(); err != nil {
		t.Fatalf("Ownership.Release() error = %v", err)
	}
}

func TestAcquire_DistinguishesIOErrorFromBusy(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", root, err)
	}
	path := filepath.Join(root, "lock")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", path, err)
	}

	owner, err := Acquire(root, path)
	if owner != nil || err == nil {
		t.Fatalf("Acquire() = (%#v, %v), want IO error", owner, err)
	}
	if errors.Is(err, ErrBusy) {
		t.Fatalf("Acquire() error = %v, must not classify IO failure as busy", err)
	}
	if !errors.Is(err, ErrIO) {
		t.Fatalf("Acquire() error = %v, want ErrIO", err)
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Errorf("Acquire() error = %q, want abnormal file detail", err)
	}
}

func TestAcquire_WritesOnlyWhenCalledAndRejectsInvalidPaths(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "state")
	path := filepath.Join(root, "lock")
	assertDirectoryEmpty(t, base)

	if owner, err := Acquire("relative-state", "relative-lock"); owner != nil || err == nil {
		t.Fatalf("Acquire(relative) = (%#v, %v), want validation error", owner, err)
	}
	if owner, err := Acquire(root, filepath.Join(base, "outside-lock")); owner != nil || err == nil {
		t.Fatalf("Acquire(outside lock) = (%#v, %v), want validation error", owner, err)
	}
	assertDirectoryEmpty(t, base)

	owner, err := Acquire(root, path)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	assertPathMode(t, root, storage.PrivateDirectoryMode)
	assertPathMode(t, path, storage.PrivateFileMode)
	if err := owner.Release(); err != nil {
		t.Fatalf("Ownership.Release() error = %v", err)
	}
}

func TestOwnership_ReuseDoesNotUnlockOuterGuard(t *testing.T) {
	root, path := lockFixturePaths(t)
	owner, err := Acquire(root, path)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	nested, err := owner.Reuse(root, path)
	if err != nil {
		t.Fatalf("Ownership.Reuse() error = %v", err)
	}

	if err := nested.Release(); err != nil {
		t.Fatalf("Guard.Release() error = %v", err)
	}
	if code, stderr := runProbe(t, root, path); code != probeBusyExitCode {
		t.Fatalf("probe while outer owner held exit = %d, want %d; stderr=%q", code, probeBusyExitCode, stderr)
	}

	if err := owner.Release(); err != nil {
		t.Fatalf("Ownership.Release() error = %v", err)
	}
	if code, stderr := runProbe(t, root, path); code != 0 {
		t.Fatalf("probe after outer release exit = %d, want 0; stderr=%q", code, stderr)
	}
}

func TestOwnership_RejectsWrongOrReleasedOwner(t *testing.T) {
	root, path := lockFixturePaths(t)
	owner, err := Acquire(root, path)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	tests := []struct {
		name string
		root string
		path string
	}{
		{name: "different root", root: filepath.Join(t.TempDir(), "state"), path: path},
		{name: "different lock", root: root, path: filepath.Join(root, "other-lock")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard, err := owner.Reuse(tt.root, tt.path)
			if guard != nil || !errors.Is(err, ErrOwnership) {
				t.Fatalf("Reuse() = (%#v, %v), want nil ErrOwnership", guard, err)
			}
		})
	}

	if err := owner.Release(); err != nil {
		t.Fatalf("Ownership.Release() error = %v", err)
	}
	if err := owner.Release(); !errors.Is(err, ErrOwnership) {
		t.Fatalf("second Ownership.Release() error = %v, want ErrOwnership", err)
	}
	if guard, err := owner.Reuse(root, path); guard != nil || !errors.Is(err, ErrOwnership) {
		t.Fatalf("Reuse() after release = (%#v, %v), want nil ErrOwnership", guard, err)
	}

	var zero Ownership
	if guard, err := zero.Reuse(root, path); guard != nil || !errors.Is(err, ErrOwnership) {
		t.Fatalf("zero Ownership.Reuse() = (%#v, %v), want nil ErrOwnership", guard, err)
	}
}

func TestOwnership_ReleaseIOErrorCanBeRetried(t *testing.T) {
	unlockErr := errors.New("unlock failed")
	fileLock := &stubBackend{unlockErr: unlockErr}
	owner := &Ownership{
		backend:    fileLock,
		root:       "/state",
		path:       "/state/lock",
		references: 1,
	}

	err := owner.Release()
	if !errors.Is(err, ErrIO) || !errors.Is(err, unlockErr) {
		t.Fatalf("Ownership.Release() error = %v, want ErrIO wrapping unlock failure", err)
	}
	fileLock.unlockErr = nil
	if err := owner.Release(); err != nil {
		t.Fatalf("Ownership.Release() retry error = %v", err)
	}
	if fileLock.unlockCalls != 2 {
		t.Errorf("Unlock() calls = %d, want 2", fileLock.unlockCalls)
	}
}

type stubBackend struct {
	unlockErr   error
	unlockCalls int
}

func (backend *stubBackend) TryLock() (bool, error) {
	return true, nil
}

func (backend *stubBackend) Unlock() error {
	backend.unlockCalls++
	return backend.unlockErr
}

type helperProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stderr  *bytes.Buffer
}

func startHolder(t *testing.T, root, path, action string) *helperProcess {
	t.Helper()
	command := exec.Command(testExecutable(t), "-test.run=^TestProcessLockHelper$")
	command.Env = append(os.Environ(),
		helperEnvironment+"=1",
		helperRootEnv+"="+root,
		helperPathEnv+"="+path,
		helperActionEnv+"="+action,
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("helper StdinPipe() error = %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("helper StdoutPipe() error = %v", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("helper Start() error = %v", err)
	}
	process := &helperProcess{command: command, stdin: stdin, stderr: &stderr}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})

	ready := make(chan error, 1)
	go func() {
		line, readErr := bufio.NewReader(stdout).ReadString('\n')
		if readErr == nil && line != "ready\n" {
			readErr = fmt.Errorf("unexpected helper readiness %q", line)
		}
		ready <- readErr
	}()
	select {
	case err := <-ready:
		if err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatalf("helper readiness error = %v; stderr=%q", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("helper readiness timed out; stderr=%q", stderr.String())
	}
	return process
}

func (process *helperProcess) release(t *testing.T) {
	t.Helper()
	if _, err := process.stdin.Write([]byte{1}); err != nil {
		t.Fatalf("signal helper release error = %v", err)
	}
	if err := process.command.Wait(); err != nil {
		t.Fatalf("helper Wait() error = %v; stderr=%q", err, process.stderr.String())
	}
}

func (process *helperProcess) waitForExitCode(t *testing.T, want int) {
	t.Helper()
	err := process.command.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != want {
		t.Fatalf("helper Wait() error = %v, want exit %d; stderr=%q", err, want, process.stderr.String())
	}
}

func runProbe(t *testing.T, root, path string) (int, string) {
	t.Helper()
	command := exec.Command(testExecutable(t), "-test.run=^TestProcessLockHelper$")
	command.Env = append(os.Environ(),
		helperEnvironment+"=1",
		helperRootEnv+"="+root,
		helperPathEnv+"="+path,
		helperActionEnv+"=probe",
	)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return 0, stderr.String()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), stderr.String()
	}
	t.Fatalf("probe Run() error = %v", err)
	return -1, stderr.String()
}

func testExecutable(t *testing.T) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	return path
}

func lockFixturePaths(t *testing.T) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state", "dot")
	return root, filepath.Join(root, "lock")
}

func assertPathMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat(%q) error = %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode(%q) = %04o, want %04o", path, got, want)
	}
}

func assertDirectoryEmpty(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("os.ReadDir(%q) error = %v", path, err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory %q entries = %v, want empty", path, entries)
	}
}
