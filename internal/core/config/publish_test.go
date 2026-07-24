package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	coreconfig "github.com/mianm12/dotfiles/internal/core/config"
)

func TestPublishMachineRoundTripsPrivatelyAndSkipsIdenticalContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "control", "config.toml")
	machine := coreconfig.Machine{
		Version:      1,
		Repository:   filepath.Join(root, "repository"),
		Profiles:     []string{"base", "work"},
		ExtraModules: []string{"tmux"},
	}

	changed, err := coreconfig.PublishMachine(path, machine)
	if err != nil || !changed {
		t.Fatalf("PublishMachine(first) = (%t, %v), want changed", changed, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat(config) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %04o, want 0600", got)
	}
	parentInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("os.Stat(parent) error = %v", err)
	}
	if got := parentInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("config parent mode = %04o, want 0700", got)
	}
	loaded, exists, err := coreconfig.LoadMachine(path)
	if err != nil || !exists || !reflect.DeepEqual(loaded, machine) {
		t.Fatalf("LoadMachine() = (%#v, %t, %v), want %#v", loaded, exists, err, machine)
	}

	changed, err = coreconfig.PublishMachine(path, machine)
	if err != nil || changed {
		t.Fatalf("PublishMachine(repeated) = (%t, %v), want no-op", changed, err)
	}
}
