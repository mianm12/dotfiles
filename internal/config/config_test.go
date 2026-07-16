package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingConfig(t *testing.T) {
	_, exists, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if exists {
		t.Fatal("Load() exists = true, want false")
	}
}

func TestLoadValidConfig(t *testing.T) {
	path := writeConfig(t, `
profile = "mac"
repo = "~/src/dotfiles"

[data]
email = "me@example.com"
machine = "work-mbp"
`)

	got, exists, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !exists {
		t.Fatal("Load() exists = false, want true")
	}
	if got.Profile != "mac" || got.Repo == nil || *got.Repo != "~/src/dotfiles" {
		t.Fatalf("Load() = %#v", got)
	}
	if got.Data["email"] != "me@example.com" || got.Data["machine"] != "work-mbp" {
		t.Fatalf("Load() data = %#v", got.Data)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
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
		t.Fatalf("write config: %v", err)
	}
	return path
}
