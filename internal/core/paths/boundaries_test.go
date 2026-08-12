package paths_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
)

func TestResolveTargetParentSymlinkResolutionChangeIsDetected(t *testing.T) {
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

func TestValidateRejectsTargetAndControlConflictsBeforeMutation(t *testing.T) {
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
			name: "lexical parent target contains child target",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				return controlsOutsideHome(root), []corepaths.Placement{
					{Label: "parent", Target: "~/tree"},
					{Label: "child", Target: "~/tree/child"},
				}
			},
			wantErr: corepaths.ErrTargetConflict,
		},
		{
			name: "lexical target relationship precedes blocked child resolution",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				if err := os.WriteFile(filepath.Join(home, "tree"), []byte("user"), 0o600); err != nil {
					t.Fatalf("os.WriteFile(tree) error = %v", err)
				}
				return controlsOutsideHome(root), []corepaths.Placement{
					{Label: "parent", Target: "~/tree"},
					{Label: "child", Target: "~/tree/child"},
				}
			},
			wantErr: corepaths.ErrTargetConflict,
		},
		{
			name: "lexical child target is contained by parent target",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				return controlsOutsideHome(root), []corepaths.Placement{
					{Label: "child", Target: "~/tree/child"},
					{Label: "parent", Target: "~/tree"},
				}
			},
			wantErr: corepaths.ErrTargetConflict,
		},
		{
			name: "resolved parent alias contains child target",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				if err := os.MkdirAll(filepath.Join(home, "real"), 0o700); err != nil {
					t.Fatalf("os.MkdirAll(real) error = %v", err)
				}
				if err := os.Symlink("real", filepath.Join(home, "alias")); err != nil {
					t.Fatalf("os.Symlink(alias) error = %v", err)
				}
				return controlsOutsideHome(root), []corepaths.Placement{
					{Label: "alias-parent", Target: "~/alias/tree"},
					{Label: "real-child", Target: "~/real/tree/child"},
				}
			},
			wantErr: corepaths.ErrTargetConflict,
		},
		{
			name: "resolved child alias is contained by parent target",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				if err := os.MkdirAll(filepath.Join(home, "real"), 0o700); err != nil {
					t.Fatalf("os.MkdirAll(real) error = %v", err)
				}
				if err := os.Symlink("real", filepath.Join(home, "alias")); err != nil {
					t.Fatalf("os.Symlink(alias) error = %v", err)
				}
				return controlsOutsideHome(root), []corepaths.Placement{
					{Label: "alias-child", Target: "~/alias/tree/child"},
					{Label: "real-parent", Target: "~/real/tree"},
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
			name: "lexical control boundary precedes blocked target resolution",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				controls := controlsOutsideHome(root)
				controls.Config = filepath.Join(home, ".config", "dot", "config.toml")
				if err := os.MkdirAll(filepath.Dir(controls.Config), 0o700); err != nil {
					t.Fatalf("os.MkdirAll(config root) error = %v", err)
				}
				if err := os.WriteFile(controls.Config, []byte("machine"), 0o600); err != nil {
					t.Fatalf("os.WriteFile(config) error = %v", err)
				}
				return controls, []corepaths.Placement{
					{Label: "inside-config", Target: "~/.config/dot/config.toml/child"},
				}
			},
			wantErr: corepaths.ErrControlBoundary,
		},
		{
			name: "target is inside state path",
			setup: func(t *testing.T, root, home string) (corepaths.Controls, []corepaths.Placement) {
				controls := controlsOutsideHome(root)
				controls.State = filepath.Join(home, ".local", "state", "dot")
				controls.Lock = filepath.Join(home, ".local", "state", "lock")
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
				controls.State = filepath.Join(home, ".local", "state", "dot", "state.json")
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
			resolved, err := validatePathSet(home, controls, placements)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Validate() = (%#v, %v), want %v", resolved, err, test.wantErr)
			}
			if resolved != nil {
				t.Fatalf("Validate() returned partial result: %#v", resolved)
			}
			labels := corepaths.PlacementLabels(err)
			if len(labels) == 0 {
				t.Fatalf("PlacementLabels() = nil, want affected placement labels")
			}
			for _, label := range labels {
				if !slices.ContainsFunc(placements, func(placement corepaths.Placement) bool {
					return placement.Label == label
				}) {
					t.Fatalf("PlacementLabels() = %v, unknown label %q", labels, label)
				}
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("Validate() mutated fixture\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestValidateRejectsControlPathsContainedByTarget(t *testing.T) {
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
				controls.Lock = filepath.Join(home, ".local", "state", "dot", "lock")
			},
		},
		{
			name:   "lock",
			target: "~/.local",
			configure: func(_ *testing.T, _, home string, controls *corepaths.Controls) {
				controls.State = filepath.Join(home, ".local", "state", "dot", "state.json")
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

			resolved, err := validatePathSet(
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

func TestResolveControlsRejectsRootRelationshipsReadOnly(t *testing.T) {
	pairs := []struct {
		name        string
		left, right int
	}{
		{name: "repository-config", left: 0, right: 1},
		{name: "repository-state", left: 0, right: 2},
		{name: "config-state", left: 1, right: 2},
	}
	relations := []struct {
		name  string
		roots func(string) (string, string)
	}{
		{
			name: "equal",
			roots: func(base string) (string, string) {
				return base, base
			},
		},
		{
			name: "left-ancestor",
			roots: func(base string) (string, string) {
				return base, filepath.Join(base, "nested")
			},
		},
		{
			name: "right-ancestor",
			roots: func(base string) (string, string) {
				return filepath.Join(base, "nested"), base
			},
		},
	}

	for _, pair := range pairs {
		for _, relation := range relations {
			t.Run(pair.name+"/"+relation.name, func(t *testing.T) {
				root := t.TempDir()
				roots := []string{
					filepath.Join(root, "repository"),
					filepath.Join(root, "config-control"),
					filepath.Join(root, "state-control"),
				}
				roots[pair.left], roots[pair.right] = relation.roots(
					filepath.Join(root, "overlap"),
				)
				controls := controlsFromRoots(roots[0], roots[1], roots[2])
				before := snapshotTree(t, root)

				_, err := corepaths.ResolveControls(controls)
				if !errors.Is(err, corepaths.ErrControlTopology) {
					t.Fatalf("ResolveControls() error = %v, want topology conflict", err)
				}
				if !containsAll(err.Error(), roots[pair.left], roots[pair.right]) {
					t.Fatalf("topology error = %q, want both paths", err)
				}

				resolved, validateErr := validatePathSet(
					filepath.Join(root, "home"),
					controls,
					nil,
				)
				if !errors.Is(validateErr, corepaths.ErrControlTopology) || resolved != nil {
					t.Fatalf(
						"Validate(empty) = (%#v, %v), want empty topology conflict",
						resolved,
						validateErr,
					)
				}
				if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
					t.Fatalf("control validation mutated fixture\nbefore=%v\nafter=%v", before, after)
				}
			})
		}
	}
}

func TestResolveControlsRejectsResolvedControlAliases(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string) corepaths.Controls
	}{
		{
			name: "config root resolves to repository",
			setup: func(t *testing.T, root string) corepaths.Controls {
				repository := filepath.Join(root, "repository")
				if err := os.MkdirAll(repository, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(repository) error = %v", err)
				}
				configRoot := filepath.Join(root, "config-alias")
				if err := os.Symlink(repository, configRoot); err != nil {
					t.Fatalf("os.Symlink(config root) error = %v", err)
				}
				return controlsFromRoots(
					repository,
					configRoot,
					filepath.Join(root, "state-control"),
				)
			},
		},
		{
			name: "state root resolves below repository",
			setup: func(t *testing.T, root string) corepaths.Controls {
				repository := filepath.Join(root, "repository")
				stateDestination := filepath.Join(repository, "state")
				if err := os.MkdirAll(stateDestination, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(state destination) error = %v", err)
				}
				stateRoot := filepath.Join(root, "state-alias")
				if err := os.Symlink(stateDestination, stateRoot); err != nil {
					t.Fatalf("os.Symlink(state root) error = %v", err)
				}
				return controlsFromRoots(
					repository,
					filepath.Join(root, "config-control"),
					stateRoot,
				)
			},
		},
		{
			name: "machine config resolves inside repository",
			setup: func(t *testing.T, root string) corepaths.Controls {
				repository := filepath.Join(root, "repository")
				destination := filepath.Join(repository, "machine.toml")
				if err := os.MkdirAll(repository, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(repository) error = %v", err)
				}
				if err := os.WriteFile(destination, []byte("config"), 0o600); err != nil {
					t.Fatalf("os.WriteFile(destination) error = %v", err)
				}
				controls := controlsFromRoots(
					repository,
					filepath.Join(root, "config-control"),
					filepath.Join(root, "state-control"),
				)
				if err := os.MkdirAll(filepath.Dir(controls.Config), 0o700); err != nil {
					t.Fatalf("os.MkdirAll(config root) error = %v", err)
				}
				if err := os.Symlink(destination, controls.Config); err != nil {
					t.Fatalf("os.Symlink(machine config) error = %v", err)
				}
				return controls
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			controls := test.setup(t, root)
			before := snapshotTree(t, root)

			_, err := corepaths.ResolveControls(controls)

			if !errors.Is(err, corepaths.ErrControlTopology) {
				t.Fatalf("ResolveControls() error = %v, want resolved conflict", err)
			}
			if err == nil || !containsAll(err.Error(), "resolved") {
				t.Fatalf("topology error = %q, want resolved paths", err)
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("control validation mutated fixture\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestResolvedControlsKeepOneTopologySnapshot(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	firstRepository := filepath.Join(root, "first-repository")
	secondRepository := filepath.Join(root, "second-repository")
	for _, path := range []string{
		home,
		firstRepository,
		secondRepository,
		filepath.Join(root, "config-control"),
		filepath.Join(root, "state-control"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", path, err)
		}
	}
	repositoryAlias := filepath.Join(root, "repository")
	if err := os.Symlink(firstRepository, repositoryAlias); err != nil {
		t.Fatalf("os.Symlink(first repository) error = %v", err)
	}
	targetAlias := filepath.Join(home, "managed")
	if err := os.Symlink(firstRepository, targetAlias); err != nil {
		t.Fatalf("os.Symlink(target alias) error = %v", err)
	}
	controls := corepaths.Controls{
		Repository: repositoryAlias,
		Config:     filepath.Join(root, "config-control", "machine.toml"),
		State:      filepath.Join(root, "state-control", "state.json"),
		Lock:       filepath.Join(root, "state-control", "dot.lock"),
	}
	resolvedControls, err := corepaths.ResolveControls(controls)
	if err != nil {
		t.Fatalf("ResolveControls() error = %v", err)
	}
	target, err := corepaths.ResolveTarget(home, "~/managed/file")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	if err := os.Remove(repositoryAlias); err != nil {
		t.Fatalf("os.Remove(repository alias) error = %v", err)
	}
	if err := os.Symlink(secondRepository, repositoryAlias); err != nil {
		t.Fatalf("os.Symlink(second repository) error = %v", err)
	}

	overlaps, err := resolvedControls.TargetOverlaps(target)
	if err != nil || !overlaps {
		t.Fatalf("ResolvedControls.TargetOverlaps() = (%t, %v), want frozen overlap", overlaps, err)
	}
	freshControls, err := corepaths.ResolveControls(controls)
	if err != nil {
		t.Fatalf("ResolveControls(fresh) error = %v", err)
	}
	freshOverlap, err := freshControls.TargetOverlaps(target)
	if err != nil || freshOverlap {
		t.Fatalf("ResolvedControls.TargetOverlaps(fresh) = (%t, %v), want retargeted topology", freshOverlap, err)
	}
}

func TestResolvedControlsZeroValueFailsClosed(t *testing.T) {
	var controls corepaths.ResolvedControls
	if _, err := controls.Paths(); !errors.Is(err, corepaths.ErrControlTopology) {
		t.Fatalf("ResolvedControls.Paths() error = %v, want ErrControlTopology", err)
	}
	if resolved, err := controls.Validate("/home", nil); !errors.Is(err, corepaths.ErrControlTopology) || resolved != nil {
		t.Fatalf("ResolvedControls.Validate() = (%#v, %v), want fail closed", resolved, err)
	}
	if overlaps, err := controls.TargetOverlaps(corepaths.Target{}); !errors.Is(err, corepaths.ErrControlTopology) || overlaps {
		t.Fatalf("ResolvedControls.TargetOverlaps() = (%t, %v), want fail closed", overlaps, err)
	}
}

func TestResolveControlsReportsLexicalOverlapBeforeBlockedResolution(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	if err := os.WriteFile(repository, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(repository) error = %v", err)
	}
	controls := corepaths.Controls{
		Repository: repository,
		Config:     filepath.Join(repository, "config.toml"),
		State:      filepath.Join(root, "state-control", "state.json"),
		Lock:       filepath.Join(root, "state-control", "lock"),
	}
	before := snapshotTree(t, root)

	_, err := corepaths.ResolveControls(controls)

	if !errors.Is(err, corepaths.ErrControlTopology) {
		t.Fatalf("ResolveControls() error = %v, want lexical topology conflict", err)
	}
	if errors.Is(err, corepaths.ErrPathBlocked) {
		t.Fatalf("ResolveControls() error = %v, want topology diagnosed before resolution", err)
	}
	if !containsAll(err.Error(), repository, "machine config root") {
		t.Fatalf("topology error = %q, want root identities", err)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("control validation mutated fixture\nbefore=%v\nafter=%v", before, after)
	}
}

func TestValidateLockBoundaryDoesNotRequireRepository(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config", "machine.toml")
	statePath := filepath.Join(root, "state", "state.json")
	lockPath := filepath.Join(root, "state", "lock")
	before := snapshotTree(t, root)

	if err := corepaths.ValidateLockBoundary(configPath, statePath, lockPath); err != nil {
		t.Fatalf("ValidateLockBoundary() error = %v", err)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("lock boundary validation mutated fixture\nbefore=%v\nafter=%v", before, after)
	}
}

func TestValidateLockBoundaryRejectsConfigStateFamilyOverlap(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "controls", "machine.toml")
	statePath := filepath.Join(root, "controls", "state", "state.json")
	lockPath := filepath.Join(root, "controls", "state", "lock")
	before := snapshotTree(t, root)

	err := corepaths.ValidateLockBoundary(configPath, statePath, lockPath)
	if !errors.Is(err, corepaths.ErrControlTopology) {
		t.Fatalf("ValidateLockBoundary() error = %v, want topology conflict", err)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("lock boundary validation mutated fixture\nbefore=%v\nafter=%v", before, after)
	}
}

func TestValidateLockBoundaryRejectsResolvedControlAliases(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string) (string, string, string)
	}{
		{
			name: "config root aliases state root",
			setup: func(t *testing.T, root string) (string, string, string) {
				stateRoot := filepath.Join(root, "state-control")
				if err := os.MkdirAll(stateRoot, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(state root) error = %v", err)
				}
				configRoot := filepath.Join(root, "config-alias")
				if err := os.Symlink(stateRoot, configRoot); err != nil {
					t.Fatalf("os.Symlink(config root) error = %v", err)
				}
				return filepath.Join(configRoot, "machine.toml"),
					filepath.Join(stateRoot, "state.json"),
					filepath.Join(stateRoot, "lock")
			},
		},
		{
			name: "state and lock entries alias one file",
			setup: func(t *testing.T, root string) (string, string, string) {
				stateRoot := filepath.Join(root, "state-control")
				if err := os.MkdirAll(stateRoot, 0o700); err != nil {
					t.Fatalf("os.MkdirAll(state root) error = %v", err)
				}
				destination := filepath.Join(root, "shared-control-file")
				if err := os.WriteFile(destination, []byte("shared"), 0o600); err != nil {
					t.Fatalf("os.WriteFile(shared) error = %v", err)
				}
				statePath := filepath.Join(stateRoot, "state.json")
				lockPath := filepath.Join(stateRoot, "lock")
				if err := os.Symlink(destination, statePath); err != nil {
					t.Fatalf("os.Symlink(state) error = %v", err)
				}
				if err := os.Symlink(destination, lockPath); err != nil {
					t.Fatalf("os.Symlink(lock) error = %v", err)
				}
				return filepath.Join(root, "config-control", "machine.toml"),
					statePath,
					lockPath
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			configPath, statePath, lockPath := test.setup(t, root)
			before := snapshotTree(t, root)

			err := corepaths.ValidateLockBoundary(configPath, statePath, lockPath)

			if !errors.Is(err, corepaths.ErrControlTopology) {
				t.Fatalf("ValidateLockBoundary() error = %v, want resolved topology conflict", err)
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("lock boundary validation mutated fixture\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestResolveControlsRequiresDistinctStateSiblings(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string, *corepaths.Controls)
	}{
		{
			name: "same lexical path",
			setup: func(_ *testing.T, _ string, controls *corepaths.Controls) {
				controls.Lock = controls.State
			},
		},
		{
			name: "different roots",
			setup: func(_ *testing.T, root string, controls *corepaths.Controls) {
				controls.Lock = filepath.Join(root, "other-state-control", "lock")
			},
		},
		{
			name: "resolved aliases",
			setup: func(t *testing.T, root string, controls *corepaths.Controls) {
				destination := filepath.Join(root, "shared-control-file")
				if err := os.WriteFile(destination, []byte("shared"), 0o600); err != nil {
					t.Fatalf("os.WriteFile(shared) error = %v", err)
				}
				if err := os.MkdirAll(filepath.Dir(controls.State), 0o700); err != nil {
					t.Fatalf("os.MkdirAll(state root) error = %v", err)
				}
				if err := os.Symlink(destination, controls.State); err != nil {
					t.Fatalf("os.Symlink(state) error = %v", err)
				}
				if err := os.Symlink(destination, controls.Lock); err != nil {
					t.Fatalf("os.Symlink(lock) error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			controls := controlsOutsideHome(root)
			test.setup(t, root, &controls)
			before := snapshotTree(t, root)

			_, err := corepaths.ResolveControls(controls)

			if !errors.Is(err, corepaths.ErrControlTopology) {
				t.Fatalf("ResolveControls() error = %v, want sibling conflict", err)
			}
			if err == nil || !containsAll(err.Error(), controls.State, controls.Lock) {
				t.Fatalf("topology error = %q, want state and lock", err)
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("control validation mutated fixture\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestResolveControlsAllowsSeparatedSiblingRoots(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	actual := filepath.Join(root, "actual")
	for _, directory := range []string{shared, actual} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatalf("os.Symlink(alias) error = %v", err)
	}
	controls := controlsFromRoots(
		filepath.Join(shared, "repository"),
		filepath.Join(alias, "config-control"),
		filepath.Join(alias, "state-control"),
	)
	before := snapshotTree(t, root)

	if _, err := corepaths.ResolveControls(controls); err != nil {
		t.Fatalf("ResolveControls() error = %v", err)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("control validation mutated fixture\nbefore=%v\nafter=%v", before, after)
	}
}

func controlsFromRoots(repository, configRoot, stateRoot string) corepaths.Controls {
	return corepaths.Controls{
		Repository: repository,
		Config:     filepath.Join(configRoot, "config.toml"),
		State:      filepath.Join(stateRoot, "state.json"),
		Lock:       filepath.Join(stateRoot, "lock"),
	}
}

func validatePathSet(
	home string,
	controls corepaths.Controls,
	placements []corepaths.Placement,
) ([]corepaths.ResolvedPlacement, error) {
	resolvedControls, err := corepaths.ResolveControls(controls)
	if err != nil {
		return nil, err
	}
	return resolvedControls.Validate(home, placements)
}

func containsAll(text string, values ...string) bool {
	for _, value := range values {
		if !strings.Contains(text, value) {
			return false
		}
	}
	return true
}

func controlsOutsideHome(root string) corepaths.Controls {
	return corepaths.Controls{
		Repository: filepath.Join(root, "repository"),
		Config:     filepath.Join(root, "config-control", "config.toml"),
		State:      filepath.Join(root, "state-control", "state.json"),
		Lock:       filepath.Join(root, "state-control", "lock"),
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
