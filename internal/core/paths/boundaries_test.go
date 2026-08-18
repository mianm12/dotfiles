package paths_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
)

func TestParsedTargetsKeepLexicalNestingForTheLoop(t *testing.T) {
	parent, err := corepaths.ParseTarget("~/.config/app")
	if err != nil {
		t.Fatalf("ParseTarget(parent) error = %v", err)
	}
	child, err := corepaths.ParseTarget("~/.config/app/child")
	if err != nil {
		t.Fatalf("ParseTarget(child) error = %v", err)
	}
	if !corepaths.TargetsConflict(parent, child) {
		t.Fatal("parsed parent and child must remain lexically nested")
	}
}

func TestTargetOverlapsUsesLexicalPrefixes(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	controls, err := corepaths.NormalizeControls(corepaths.Controls{
		Repository: filepath.Join(root, "repository"),
		Config:     filepath.Join(home, ".config", "dot", "config.toml"),
		State:      filepath.Join(home, ".local", "state", "dot", "state.json"),
		Lock:       filepath.Join(home, ".local", "state", "dot", "lock"),
	})
	if err != nil {
		t.Fatalf("NormalizeControls() error = %v", err)
	}
	inside, err := corepaths.ParseTarget("~/.config/dot/extra")
	if err != nil {
		t.Fatalf("ParseTarget(inside) error = %v", err)
	}
	outside, err := corepaths.ParseTarget("~/.zshrc")
	if err != nil {
		t.Fatalf("ParseTarget(outside) error = %v", err)
	}
	overlaps, err := controls.TargetOverlaps(home, inside)
	if err != nil || !overlaps {
		t.Fatalf("TargetOverlaps(control child) = (%t, %v), want true", overlaps, err)
	}
	overlaps, err = controls.TargetOverlaps(home, outside)
	if err != nil || overlaps {
		t.Fatalf("TargetOverlaps(user target) = (%t, %v), want false", overlaps, err)
	}
}

func TestNormalizeControlsRejectsLexicalPrefixOverlap(t *testing.T) {
	root := t.TempDir()
	stateRoot := filepath.Join(root, "state")
	_, err := corepaths.NormalizeControls(corepaths.Controls{
		Repository: filepath.Join(root, "repo"),
		Config:     filepath.Join(root, "repo", "config.toml"),
		State:      filepath.Join(stateRoot, "state.json"),
		Lock:       filepath.Join(stateRoot, "lock"),
	})
	if !errors.Is(err, corepaths.ErrControlTopology) {
		t.Fatalf("NormalizeControls(repo/config overlap) error = %v, want ErrControlTopology", err)
	}
}

func TestValidateLockBoundaryRejectsSymlinkRoot(t *testing.T) {
	root := t.TempDir()
	realRoot := filepath.Join(root, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatalf("os.Mkdir(real) error = %v", err)
	}
	configRoot := filepath.Join(root, "config")
	if err := os.Symlink(realRoot, configRoot); err != nil {
		t.Fatalf("os.Symlink(config root) error = %v", err)
	}
	stateRoot := filepath.Join(root, "state")
	if err := os.Mkdir(stateRoot, 0o700); err != nil {
		t.Fatalf("os.Mkdir(state) error = %v", err)
	}
	err := corepaths.ValidateLockBoundary(
		filepath.Join(configRoot, "config.toml"),
		filepath.Join(stateRoot, "state.json"),
		filepath.Join(stateRoot, "lock"),
	)
	if !errors.Is(err, corepaths.ErrControlTopology) {
		t.Fatalf("ValidateLockBoundary(symlink root) error = %v, want ErrControlTopology", err)
	}
}

func TestValidateLockBoundaryAcceptsMissingRoots(t *testing.T) {
	root := t.TempDir()
	err := corepaths.ValidateLockBoundary(
		filepath.Join(root, "config", "config.toml"),
		filepath.Join(root, "state", "state.json"),
		filepath.Join(root, "state", "lock"),
	)
	if err != nil {
		t.Fatalf("ValidateLockBoundary(missing roots) error = %v", err)
	}
}

func TestZeroLexicalControlsRejectsUse(t *testing.T) {
	var controls corepaths.LexicalControls
	if _, err := controls.Paths(); !errors.Is(err, corepaths.ErrControlTopology) {
		t.Fatalf("Paths() error = %v, want ErrControlTopology", err)
	}
	target, err := corepaths.ParseTarget("~/.app")
	if err != nil {
		t.Fatalf("ParseTarget() error = %v", err)
	}
	if _, err := controls.TargetOverlaps("/tmp/home", target); !errors.Is(err, corepaths.ErrControlTopology) {
		t.Fatalf("TargetOverlaps() error = %v, want ErrControlTopology", err)
	}
}
