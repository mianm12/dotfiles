package config

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublish_PrePublishFaultMatrixPreservesDestinationAndCleansTemporary(t *testing.T) {
	faultErr := errors.New("injected publisher failure")
	tests := []struct {
		name    string
		missing bool
		stage   string
		wantErr error
	}{
		{name: "existing create temp", stage: "create", wantErr: faultErr},
		{name: "existing chmod", stage: "chmod", wantErr: faultErr},
		{name: "existing write", stage: "write", wantErr: faultErr},
		{name: "existing short write", stage: "short-write", wantErr: io.ErrShortWrite},
		{name: "existing sync", stage: "sync", wantErr: faultErr},
		{name: "existing close", stage: "close", wantErr: faultErr},
		{name: "existing final replace", stage: "final", wantErr: faultErr},
		{name: "missing create temp", missing: true, stage: "create", wantErr: faultErr},
		{name: "missing chmod", missing: true, stage: "chmod", wantErr: faultErr},
		{name: "missing write", missing: true, stage: "write", wantErr: faultErr},
		{name: "missing short write", missing: true, stage: "short-write", wantErr: io.ErrShortWrite},
		{name: "missing sync", missing: true, stage: "sync", wantErr: faultErr},
		{name: "missing close", missing: true, stage: "close", wantErr: faultErr},
		{name: "missing final no-replace", missing: true, stage: "final", wantErr: faultErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "config.toml")
			old := []byte("profile = \"mac\"\n")
			if !tt.missing {
				writeConfigAt(t, path, string(old), 0o640)
			}
			snapshot, err := LoadSnapshot(path)
			if err != nil {
				t.Fatalf("LoadSnapshot() error = %v", err)
			}
			candidate, err := NewCandidate(snapshot, Machine{Profile: "linux"})
			if err != nil {
				t.Fatalf("NewCandidate() error = %v", err)
			}
			operations := faultPublishOperations(tt.stage, faultErr)

			result, err := publish(path, candidate, operations)
			if result.Changed() || result.Committed() || !errors.Is(err, tt.wantErr) {
				t.Fatalf("publish() = (%#v, %v), want uncommitted %v", result, err, tt.wantErr)
			}
			if tt.missing {
				if _, statErr := os.Lstat(path); !errors.Is(statErr, fs.ErrNotExist) {
					t.Fatalf("missing destination after failure Lstat error = %v, want missing", statErr)
				}
			} else {
				assertRegularConfigState(t, path, old, 0o640)
			}
			assertNoConfigTemporary(t, root)
		})
	}
}

func TestPublish_CleanupFailureJoinsPrimaryAndPreservesDestination(t *testing.T) {
	primaryErr := errors.New("chmod failed")
	removeErr := errors.New("remove failed")
	for _, missing := range []bool{false, true} {
		name := "existing"
		if missing {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "config.toml")
			old := []byte("profile = \"mac\"\n")
			if !missing {
				writeConfigAt(t, path, string(old), 0o640)
			}
			snapshot, err := LoadSnapshot(path)
			if err != nil {
				t.Fatalf("LoadSnapshot() error = %v", err)
			}
			candidate, err := NewCandidate(snapshot, Machine{Profile: "linux"})
			if err != nil {
				t.Fatalf("NewCandidate() error = %v", err)
			}
			operations := faultPublishOperations("chmod", primaryErr)
			operations.remove = func(string) error { return removeErr }

			result, err := publish(path, candidate, operations)
			if result.Changed() || result.Committed() || !errors.Is(err, primaryErr) || !errors.Is(err, removeErr) {
				t.Fatalf("publish() = (%#v, %v), want joined uncommitted primary/remove errors", result, err)
			}
			if missing {
				if _, statErr := os.Lstat(path); !errors.Is(statErr, fs.ErrNotExist) {
					t.Fatalf("missing destination after cleanup failure Lstat error = %v", statErr)
				}
			} else {
				assertRegularConfigState(t, path, old, 0o640)
			}
			entries, readErr := os.ReadDir(root)
			if readErr != nil {
				t.Fatalf("os.ReadDir() error = %v", readErr)
			}
			foundTemporary := false
			for _, entry := range entries {
				foundTemporary = foundTemporary || strings.HasPrefix(entry.Name(), ".config.toml-")
			}
			if !foundTemporary {
				t.Fatal("cleanup failure did not retain the temporary file named in the error path")
			}
		})
	}
}

func TestPublish_PostLinkCleanupFailureReportsCommittedConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	snapshot, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	candidate, err := NewCandidate(snapshot, Machine{Profile: "mac"})
	if err != nil {
		t.Fatalf("NewCandidate() error = %v", err)
	}
	removeErr := errors.New("post-link remove failed")
	operations := defaultPublishOperations()
	operations.remove = func(string) error { return removeErr }

	result, err := publish(path, candidate, operations)
	if !result.Changed() || !result.Committed() || !errors.Is(err, removeErr) {
		t.Fatalf("publish() = (%#v, %v), want changed+committed with cleanup error", result, err)
	}
	assertRegularConfigState(t, path, candidate.Bytes(), 0o600)
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatalf("os.ReadDir() error = %v", readErr)
	}
	foundTemporary := false
	for _, entry := range entries {
		foundTemporary = foundTemporary || strings.HasPrefix(entry.Name(), ".config.toml-")
	}
	if !foundTemporary {
		t.Fatal("post-link cleanup failure did not leave the reported temporary path")
	}
}

func faultPublishOperations(stage string, faultErr error) publishOperations {
	operations := defaultPublishOperations()
	if stage == "create" {
		operations.createTemp = func(string, string) (publishFile, error) { return nil, faultErr }
		return operations
	}
	realCreateTemp := operations.createTemp
	operations.createTemp = func(directory, pattern string) (publishFile, error) {
		file, err := realCreateTemp(directory, pattern)
		if err != nil {
			return nil, err
		}
		return &faultPublishFile{publishFile: file, stage: stage, err: faultErr}, nil
	}
	if stage == "final" {
		operations.rename = func(string, string) error { return faultErr }
		operations.link = func(string, string) error { return faultErr }
	}
	return operations
}

type faultPublishFile struct {
	publishFile
	stage string
	err   error
}

func (file *faultPublishFile) Chmod(mode fs.FileMode) error {
	if file.stage == "chmod" {
		return file.err
	}
	return file.publishFile.Chmod(mode)
}

func (file *faultPublishFile) Write(data []byte) (int, error) {
	switch file.stage {
	case "write":
		return 0, file.err
	case "short-write":
		if len(data) == 0 {
			return 0, nil
		}
		return len(data) - 1, nil
	default:
		return file.publishFile.Write(data)
	}
}

func (file *faultPublishFile) Sync() error {
	if file.stage == "sync" {
		return file.err
	}
	return file.publishFile.Sync()
}

func (file *faultPublishFile) Close() error {
	if file.stage == "close" {
		if err := file.publishFile.Close(); err != nil {
			return errors.Join(file.err, err)
		}
		return file.err
	}
	return file.publishFile.Close()
}

func assertRegularConfigState(t *testing.T, path string, wantBytes []byte, wantMode fs.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("os.Lstat(%q) error = %v", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != wantMode {
		t.Fatalf("config kind/mode = %v/%04o, want regular/%04o", info.Mode().Type(), info.Mode().Perm(), wantMode)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, wantBytes) {
		t.Fatalf("config bytes = %q, %v; want %q", got, err, wantBytes)
	}
}
