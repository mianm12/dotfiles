package config

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSnapshot_PreservesMachineAndObjectEvidence(t *testing.T) {
	path := writeConfig(t, "profile = \"mac\"\nrepo = \"~/repo\"\n[data]\nold = \"value\"\n")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("os.Chmod() error = %v", err)
	}

	snapshot, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if !snapshot.Exists() || snapshot.Profile() != "mac" {
		t.Fatalf("snapshot = %#v, want existing mac config", snapshot)
	}
	if repo, ok := snapshot.Repo(); !ok || repo != "~/repo" {
		t.Fatalf("Repo() = (%q, %t), want (~/repo, true)", repo, ok)
	}
	data := snapshot.Data()
	data["old"] = "changed"
	if got := snapshot.Data()["old"]; got != "value" {
		t.Fatalf("mutating Data() changed snapshot: got %q", got)
	}
	precondition := snapshot.Precondition()
	if !precondition.Exists() || precondition.Kind() != fs.FileMode(0) || precondition.Mode() != 0o640 {
		t.Fatalf("Precondition() = %#v, want regular 0640", precondition)
	}
	bytes := precondition.Bytes()
	bytes[0] = 'X'
	if got := string(snapshot.Precondition().Bytes()); got[0] == 'X' {
		t.Fatal("mutating Precondition.Bytes() changed snapshot")
	}
}

func TestLoadSnapshot_MissingHasSealedMissingPrecondition(t *testing.T) {
	snapshot, err := LoadSnapshot(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if snapshot.Exists() || snapshot.Precondition().Exists() {
		t.Fatalf("missing snapshot = %#v, want missing", snapshot)
	}
}

func TestPublish_CreatesPrivateConfigAndEquivalentCandidateDoesNotRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	snapshot, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	candidate, err := NewCandidate(snapshot, Machine{Profile: "mac", Data: map[string]string{"email": "me@example.com"}})
	if err != nil {
		t.Fatalf("NewCandidate() error = %v", err)
	}
	changed, err := Publish(path, candidate)
	if err != nil || !changed {
		t.Fatalf("Publish() = (%t, %v), want changed", changed, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %04o, want 0600", info.Mode().Perm())
	}
	stored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(stored, candidate.Bytes()) {
		t.Fatalf("stored config = %q, %v; want candidate bytes", stored, err)
	}

	current, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot(current) error = %v", err)
	}
	equivalent, err := NewCandidate(current, current.Machine())
	if err != nil {
		t.Fatalf("NewCandidate(equivalent) error = %v", err)
	}
	before := info.ModTime()
	changed, err = Publish(path, equivalent)
	if err != nil || changed {
		t.Fatalf("Publish(equivalent) = (%t, %v), want no-op", changed, err)
	}
	after, err := os.Stat(path)
	if err != nil || !after.ModTime().Equal(before) {
		t.Fatalf("equivalent publish rewrote config: before %v after %v err %v", before, after.ModTime(), err)
	}
}

func TestPublish_PreconditionChangesPreserveCurrentConfigAndCleanTemporary(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, path string)
	}{
		{name: "bytes", change: func(t *testing.T, path string) { writeConfigAt(t, path, "profile = \"linux\"\n", 0o600) }},
		{name: "mode", change: func(t *testing.T, path string) {
			if err := os.Chmod(path, 0o640); err != nil {
				t.Fatalf("os.Chmod() error = %v", err)
			}
		}},
		{name: "kind", change: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatalf("os.Remove() error = %v", err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatalf("os.Mkdir() error = %v", err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "config.toml")
			writeConfigAt(t, path, "profile = \"mac\"\n", 0o600)
			snapshot, err := LoadSnapshot(path)
			if err != nil {
				t.Fatalf("LoadSnapshot() error = %v", err)
			}
			candidate, err := NewCandidate(snapshot, Machine{Profile: "linux"})
			if err != nil {
				t.Fatalf("NewCandidate() error = %v", err)
			}
			tt.change(t, path)
			_, err = Publish(path, candidate)
			if !errors.Is(err, ErrPreconditionChanged) {
				t.Fatalf("Publish() error = %v, want ErrPreconditionChanged", err)
			}
			entries, readErr := os.ReadDir(root)
			if readErr != nil {
				t.Fatalf("os.ReadDir() error = %v", readErr)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".config.toml-") {
					t.Fatalf("temporary file remained after failed publish: %q", entry.Name())
				}
			}
		})
	}
}

func TestPublish_RenameFailurePreservesOldConfigAndCleansTemporary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	old := []byte("profile = \"mac\"\n")
	writeConfigAt(t, path, string(old), 0o600)
	snapshot, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	candidate, err := NewCandidate(snapshot, Machine{Profile: "linux"})
	if err != nil {
		t.Fatalf("NewCandidate() error = %v", err)
	}
	renameErr := errors.New("rename failed")
	changed, err := publish(path, candidate, publishOperations{rename: func(string, string) error {
		return renameErr
	}})
	if changed || !errors.Is(err, renameErr) {
		t.Fatalf("publish() = (%t, %v), want rename failure", changed, err)
	}
	current, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(current, old) {
		t.Fatalf("old config after failure = %q, %v; want %q", current, readErr, old)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatalf("os.ReadDir() error = %v", readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("directory after failure = %#v, want only config.toml", entries)
	}
}

func writeConfigAt(t *testing.T, path, content string, mode fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}

func TestLoad_MissingConfig(t *testing.T) {
	got, exists, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if exists {
		t.Error("Load() exists = true, want false")
	}
	if got.Profile != "" || got.Repo != nil || len(got.Data) != 0 {
		t.Errorf("Load() = %#v, want empty Machine", got)
	}
}

func TestLoad_RejectsDanglingConfigSymlink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.Symlink(filepath.Join(root, "missing.toml"), path); err != nil {
		t.Fatalf("os.Symlink(%q) error = %v", path, err)
	}

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want dangling symlink error")
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	path := writeConfig(t, `
profile = "mac"
repo = "~/src/dotfiles"

[data]
email = "me@example.com"
machine = "work-mbp"
`)

	got, exists, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if !exists {
		t.Error("Load() exists = false, want true")
	}
	wantProfile := "mac"
	wantRepo := "~/src/dotfiles"
	if got.Profile != wantProfile {
		t.Errorf("Load().Profile = %q, want %q", got.Profile, wantProfile)
	}
	if got.Repo == nil {
		t.Errorf("Load().Repo = nil, want %q", wantRepo)
	} else if *got.Repo != wantRepo {
		t.Errorf("Load().Repo = %q, want %q", *got.Repo, wantRepo)
	}
	if got.Data["email"] != "me@example.com" {
		t.Errorf("Load().Data[%q] = %q, want %q", "email", got.Data["email"], "me@example.com")
	}
	if got.Data["machine"] != "work-mbp" {
		t.Errorf("Load().Data[%q] = %q, want %q", "machine", got.Data["machine"], "work-mbp")
	}
}

func TestLoad_RejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "missing profile", content: `repo = "~/repo"`},
		{name: "empty profile", content: `profile = ""`},
		{name: "empty repo", content: "profile = \"mac\"\nrepo = \"\""},
		{name: "unknown top-level key", content: "profile = \"mac\"\nunknown = true"},
		{name: "wrong data value type", content: "profile = \"mac\"\n[data]\nvalue = 1"},
		{name: "invalid data key", content: "profile = \"mac\"\n[data]\nUpper = \"value\""},
		{name: "invalid toml", content: `profile =`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Load(writeConfig(t, tt.content))
			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}
		})
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}
