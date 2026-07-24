package paths_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
)

func TestAcceptance11_ParentSymlinkResolutionChangeIsDetected(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	firstParent := filepath.Join(root, "first")
	secondParent := filepath.Join(root, "second")
	for _, directory := range []string{home, firstParent, secondParent} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}

	parentLink := filepath.Join(home, "parent")
	if err := os.Symlink(firstParent, parentLink); err != nil {
		t.Fatalf("os.Symlink(first parent) error = %v", err)
	}
	beforeResolve := snapshotTree(t, root)
	first, err := corepaths.ResolveTarget(home, "~/parent/missing/config")
	if err != nil {
		t.Fatalf("ResolveTarget(first) error = %v", err)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, beforeResolve) {
		t.Fatalf("ResolveTarget(first) mutated fixture\nbefore=%v\nafter=%v", beforeResolve, after)
	}
	resolvedFirstParent, err := filepath.EvalSymlinks(firstParent)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(first parent) error = %v", err)
	}
	if got, want := first.Resolved(), filepath.Join(resolvedFirstParent, "missing", "config"); got != want {
		t.Fatalf("first resolved target = %q, want %q", got, want)
	}

	if err := os.Remove(parentLink); err != nil {
		t.Fatalf("os.Remove(parent link) error = %v", err)
	}
	if err := os.Symlink(secondParent, parentLink); err != nil {
		t.Fatalf("os.Symlink(second parent) error = %v", err)
	}
	beforeReresolve := snapshotTree(t, root)
	second, err := corepaths.ResolveTarget(home, "~/parent/missing/config")
	if err != nil {
		t.Fatalf("ResolveTarget(second) error = %v", err)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, beforeReresolve) {
		t.Fatalf("ResolveTarget(second) mutated fixture\nbefore=%v\nafter=%v", beforeReresolve, after)
	}
	resolvedSecondParent, err := filepath.EvalSymlinks(secondParent)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(second parent) error = %v", err)
	}
	if got, want := second.Resolved(), filepath.Join(resolvedSecondParent, "missing", "config"); got != want {
		t.Fatalf("second resolved target = %q, want %q", got, want)
	}
	if first.Resolved() == second.Resolved() {
		t.Fatalf("parent symlink change was not observable: first=%q second=%q", first.Resolved(), second.Resolved())
	}
}

func TestAcceptance12_RejectsTargetAndControlConflictsBeforeMutation(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T, string, string) (corepaths.Controls, []corepaths.Placement)
		wantErr error
	}{
		{
			name: "lexically equal targets",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				return controlsOutsideHome(root), []corepaths.Placement{
					{Label: "first", Target: "~/.config/../same"},
					{Label: "second", Target: "~/same"},
				}
			},
			wantErr: corepaths.ErrTargetConflict,
		},
		{
			name: "resolved targets equal through parent symlink",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				if err := os.MkdirAll(filepath.Join(home, "real"), 0o700); err != nil {
					t.Fatalf("os.MkdirAll(real) error = %v", err)
				}
				if err := os.Symlink("real", filepath.Join(home, "alias")); err != nil {
					t.Fatalf("os.Symlink(alias) error = %v", err)
				}
				return controlsOutsideHome(root), []corepaths.Placement{
					{Label: "alias", Target: "~/alias/missing"},
					{Label: "real", Target: "~/real/missing"},
				}
			},
			wantErr: corepaths.ErrTargetConflict,
		},
		{
			name: "directory link contains another placement",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				return controlsOutsideHome(root), []corepaths.Placement{
					{Label: "directory", Target: "~/tree", DirectoryLink: true},
					{Label: "child", Target: "~/tree/child"},
				}
			},
			wantErr: corepaths.ErrTargetConflict,
		},
		{
			name: "target is inside repository by resolved path",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				repository := filepath.Join(root, "repository")
				if err := os.MkdirAll(repository, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(repository) error = %v", err)
				}
				if err := os.Symlink(repository, filepath.Join(home, "repo-alias")); err != nil {
					t.Fatalf("os.Symlink(repository alias) error = %v", err)
				}
				controls := controlsOutsideHome(root)
				controls.Repository = repository
				return controls, []corepaths.Placement{
					{Label: "inside-repository", Target: "~/repo-alias/config"},
				}
			},
			wantErr: corepaths.ErrControlBoundary,
		},
		{
			name: "target reaches repository symlink entry through a resolved parent alias",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				repository := filepath.Join(home, "repository")
				actualRepository := filepath.Join(root, "actual-repository")
				if err := os.MkdirAll(actualRepository, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(actual repository) error = %v", err)
				}
				if err := os.Symlink(actualRepository, repository); err != nil {
					t.Fatalf("os.Symlink(repository) error = %v", err)
				}
				if err := os.Symlink(".", filepath.Join(home, "home-alias")); err != nil {
					t.Fatalf("os.Symlink(home alias) error = %v", err)
				}
				controls := controlsOutsideHome(root)
				controls.Repository = repository
				return controls, []corepaths.Placement{
					{Label: "repository-entry", Target: "~/home-alias/repository"},
				}
			},
			wantErr: corepaths.ErrControlBoundary,
		},
		{
			name: "target is inside machine config path",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				controls := controlsOutsideHome(root)
				controls.Config = filepath.Join(home, ".config", "dot")
				return controls, []corepaths.Placement{
					{Label: "inside-config", Target: "~/.config/dot/config.toml"},
				}
			},
			wantErr: corepaths.ErrControlBoundary,
		},
		{
			name: "target is inside state path",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				controls := controlsOutsideHome(root)
				controls.State = filepath.Join(home, ".local", "state", "dot")
				return controls, []corepaths.Placement{
					{Label: "inside-state", Target: "~/.local/state/dot/state.json"},
				}
			},
			wantErr: corepaths.ErrControlBoundary,
		},
		{
			name: "target equals lock path",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				controls := controlsOutsideHome(root)
				controls.Lock = filepath.Join(home, ".local", "state", "dot", "lock")
				return controls, []corepaths.Placement{
					{Label: "lock", Target: "~/.local/state/dot/lock"},
				}
			},
			wantErr: corepaths.ErrControlBoundary,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.MkdirAll(home, 0o700); err != nil {
				t.Fatalf("os.MkdirAll(home) error = %v", err)
			}
			controls, placements := test.setup(t, root, home)
			before := snapshotTree(t, root)
			resolved, err := corepaths.Validate(home, controls, placements)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Validate() = (%#v, %v), want %v", resolved, err, test.wantErr)
			}
			if resolved != nil {
				t.Fatalf("Validate() returned partial result: %#v", resolved)
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("Validate() mutated fixture\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestAcceptance12_RejectsControlPathsContainedByTarget(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		configure func(*testing.T, string, string, *corepaths.Controls)
	}{
		{
			name:   "repository",
			target: "~/managed",
			configure: func(_ *testing.T, _, home string, controls *corepaths.Controls) {
				controls.Repository = filepath.Join(home, "managed", "repository")
			},
		},
		{
			name:   "machine config",
			target: "~/.config",
			configure: func(_ *testing.T, _, home string, controls *corepaths.Controls) {
				controls.Config = filepath.Join(home, ".config", "dot", "config.toml")
			},
		},
		{
			name:   "state",
			target: "~/.local",
			configure: func(_ *testing.T, _, home string, controls *corepaths.Controls) {
				controls.State = filepath.Join(home, ".local", "state", "dot", "state.json")
			},
		},
		{
			name:   "lock",
			target: "~/.local",
			configure: func(_ *testing.T, _, home string, controls *corepaths.Controls) {
				controls.Lock = filepath.Join(home, ".local", "state", "dot", "lock")
			},
		},
		{
			name:   "repository by resolved path",
			target: "~/alias/managed",
			configure: func(t *testing.T, root, home string, controls *corepaths.Controls) {
				actual := filepath.Join(root, "actual")
				if err := os.MkdirAll(actual, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(actual) error = %v", err)
				}
				if err := os.Symlink(actual, filepath.Join(home, "alias")); err != nil {
					t.Fatalf("os.Symlink(alias) error = %v", err)
				}
				controls.Repository = filepath.Join(actual, "managed", "repository")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.MkdirAll(home, 0o700); err != nil {
				t.Fatalf("os.MkdirAll(home) error = %v", err)
			}
			controls := controlsOutsideHome(root)
			test.configure(t, root, home, &controls)
			before := snapshotTree(t, root)

			resolved, err := corepaths.Validate(
				home,
				controls,
				[]corepaths.Placement{{Label: "container", Target: test.target}},
			)

			if !errors.Is(err, corepaths.ErrControlBoundary) {
				t.Fatalf("Validate() = (%#v, %v), want control boundary error", resolved, err)
			}
			if resolved != nil {
				t.Fatalf("Validate() returned partial result: %#v", resolved)
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("Validate() mutated fixture\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func controlsOutsideHome(root string) corepaths.Controls {
	controlRoot := filepath.Join(root, "control")
	return corepaths.Controls{
		Repository: filepath.Join(controlRoot, "repository"),
		Config:     filepath.Join(controlRoot, "config.toml"),
		State:      filepath.Join(controlRoot, "state.json"),
		Lock:       filepath.Join(controlRoot, "lock"),
	}
}

type treeEntry struct {
	mode fs.FileMode
	link string
	data string
}

func snapshotTree(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	snapshot := make(map[string]treeEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		record := treeEntry{mode: info.Mode()}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			record.link, err = os.Readlink(path)
		case info.Mode().IsRegular():
			var content []byte
			content, err = os.ReadFile(path)
			record.data = string(content)
		}
		if err != nil {
			return err
		}
		snapshot[relative] = record
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot tree %q: %v", root, err)
	}
	return snapshot
}
