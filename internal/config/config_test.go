package config

import (
	"io/fs"
	"os"
	"path/filepath"
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
