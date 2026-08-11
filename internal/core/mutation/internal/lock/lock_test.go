package lock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireIsExclusiveAndReleaseAllowsReacquire(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(root, "lock")
	firstRelease, err := Acquire(root, path)
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	if secondRelease, err := Acquire(root, path); secondRelease != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("Acquire(second) = (release_present=%t, %v), want ErrBusy", secondRelease != nil, err)
	}
	if err := firstRelease(); err != nil {
		t.Fatalf("release(first) error = %v", err)
	}
	release, err := Acquire(root, path)
	if err != nil {
		t.Fatalf("Acquire(after release) error = %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release(reacquired) error = %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("lock info = (%v, %v), want private regular file", info, err)
	}
}

func TestValidateRejectsAbnormalExistingLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(root, "lock")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(lock directory) error = %v", err)
	}
	if err := Validate(root, path); err == nil {
		t.Fatal("Validate(lock directory) error = nil")
	}
}

func TestReleaseClassifiesBackendFailure(t *testing.T) {
	unlockErr := errors.New("synthetic unlock failure")
	release := releaseBackend(&stubBackend{unlockErr: unlockErr}, "/state/lock")

	err := release()
	if !errors.Is(err, ErrIO) || !errors.Is(err, unlockErr) {
		t.Fatalf("release() error = %v, want ErrIO wrapping backend failure", err)
	}
}

type stubBackend struct {
	unlockErr error
}

func (*stubBackend) TryLock() (bool, error) { return true, nil }

func (backend *stubBackend) Unlock() error { return backend.unlockErr }
