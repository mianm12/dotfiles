package paths_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
)

func TestExpandKeepsLexicalNestingForTheLoop(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	controls, err := corepaths.NormalizeControls(controlsOutsideHome(root))
	if err != nil {
		t.Fatalf("NormalizeControls() error = %v", err)
	}
	placements, err := controls.Expand(home, []corepaths.Placement{
		{Label: "parent", Target: "~/.config/app"},
		{Label: "child", Target: "~/.config/app/child"},
	})
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if len(placements) != 2 || !corepaths.TargetsConflict(placements[0].Target, placements[1].Target) {
		t.Fatalf("Expand() = %#v, want two lexically nested targets", placements)
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
	inside, err := corepaths.ResolveTarget(home, "~/.config/dot/extra")
	if err != nil {
		t.Fatalf("ResolveTarget(inside) error = %v", err)
	}
	outside, err := corepaths.ResolveTarget(home, "~/.zshrc")
	if err != nil {
		t.Fatalf("ResolveTarget(outside) error = %v", err)
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
	if _, err := controls.Expand("/tmp/home", nil); !errors.Is(err, corepaths.ErrControlTopology) {
		t.Fatalf("Expand() error = %v, want ErrControlTopology", err)
	}
}

func controlsOutsideHome(root string) corepaths.Controls {
	return corepaths.Controls{
		Repository: filepath.Join(root, "repository"),
		Config:     filepath.Join(root, "config-control", "config.toml"),
		State:      filepath.Join(root, "state-control", "state.json"),
		Lock:       filepath.Join(root, "state-control", "lock"),
	}
}
