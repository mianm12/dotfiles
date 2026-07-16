package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
