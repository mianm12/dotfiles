package state_test

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	corestate "github.com/mianm12/dotfiles/internal/core/state"
)

func TestLoadRequiresDirectRegularFile(t *testing.T) {
	tests := []struct {
		name string
		path func(*testing.T, string, string) string
	}{
		{
			name: "symlink to regular file",
			path: func(t *testing.T, root, home string) string {
				target := filepath.Join(root, "actual.json")
				writeStateDocument(t, target, fmt.Sprintf(
					`{"version":2,"home":%q,"modules":{}}`,
					home,
				))
				path := filepath.Join(root, "state.json")
				if err := os.Symlink(target, path); err != nil {
					t.Fatalf("os.Symlink(state) error = %v", err)
				}
				return path
			},
		},
		{
			name: "directory",
			path: func(t *testing.T, root, _ string) string {
				path := filepath.Join(root, "state.json")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatalf("os.Mkdir(state) error = %v", err)
				}
				return path
			},
		},
		{
			name: "socket",
			path: func(t *testing.T, _, _ string) string {
				return stateUnixSocket(t, temporaryStateSocketPath(t))
			},
		},
		{
			name: "dangling symlink",
			path: func(t *testing.T, root, _ string) string {
				path := filepath.Join(root, "state.json")
				if err := os.Symlink("missing.json", path); err != nil {
					t.Fatalf("os.Symlink(dangling state) error = %v", err)
				}
				return path
			},
		},
		{
			name: "device",
			path: func(*testing.T, string, string) string {
				return os.DevNull
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatalf("os.Mkdir(home) error = %v", err)
			}
			path := test.path(t, root, home)
			before := snapshotTree(t, root)

			loaded, err := corestate.Load(path, home)

			if !errors.Is(err, corestate.ErrInvalid) {
				t.Fatalf("Load() = (%#v, %v), want invalid direct file", loaded, err)
			}
			if loaded.Missing ||
				loaded.NeedsRewrite ||
				loaded.Warning != "" ||
				loaded.Snapshot.Home != "" ||
				loaded.Snapshot.Modules != nil {
				t.Fatalf("Load(error) returned partial result %#v", loaded)
			}
			assertTreeUnchanged(t, root, before)
		})
	}
}

func TestLoadCanonicalizesEmptyModulesInMemory(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(home) error = %v", err)
	}
	statePath := filepath.Join(root, "state.json")
	target := filepath.Join(home, ".config", "kept")
	document := fmt.Sprintf(
		`{"version":2,"home":%q,"modules":{"empty_explicit":{"placements":{}},"empty_missing":{},"kept":{"placements":{"local":{"kind":"local","target":%q}}}}}`,
		home,
		target,
	)
	writeStateDocument(t, statePath, document)
	before := snapshotTree(t, root)
	want := corestate.Snapshot{
		Home: home,
		Modules: map[string]corestate.Module{
			"kept": {
				Placements: map[string]corestate.Placement{
					"local": {Kind: corestate.KindLocal, Target: target},
				},
			},
		},
	}

	loaded, err := corestate.Load(statePath, home)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Missing || !loaded.NeedsRewrite || loaded.Warning != "" {
		t.Fatalf("Load() = %#v, want rewrite-only loaded state", loaded)
	}
	if !reflect.DeepEqual(loaded.Snapshot, want) {
		t.Fatalf("Load() snapshot = %#v, want %#v", loaded.Snapshot, want)
	}
	decoded, err := corestate.Decode([]byte(document), home)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("Decode() = %#v, want %#v", decoded, want)
	}
	assertTreeUnchanged(t, root, before)
}

func TestLoadCanonicalEmptyStateDoesNotNeedRewrite(t *testing.T) {
	for _, test := range []struct {
		name    string
		modules string
	}{
		{name: "modules missing"},
		{name: "modules empty", modules: `,"modules":{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatalf("os.Mkdir(home) error = %v", err)
			}
			statePath := filepath.Join(root, "state.json")
			writeStateDocument(t, statePath, fmt.Sprintf(
				`{"version":2,"home":%q%s}`,
				home,
				test.modules,
			))

			loaded, err := corestate.Load(statePath, home)

			if err != nil ||
				loaded.Missing ||
				loaded.NeedsRewrite ||
				loaded.Warning != "" ||
				len(loaded.Snapshot.Modules) != 0 {
				t.Fatalf("Load(canonical empty) = (%#v, %v)", loaded, err)
			}
		})
	}
}

func TestMarshalRejectsEmptyModules(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	for _, placements := range []map[string]corestate.Placement{nil, {}} {
		snapshot := corestate.Snapshot{
			Home: home,
			Modules: map[string]corestate.Module{
				"empty": {Placements: placements},
			},
		}

		data, err := corestate.Marshal(snapshot)

		if !errors.Is(err, corestate.ErrInvalid) {
			t.Fatalf("Marshal(empty module) = (%q, %v), want ErrInvalid", data, err)
		}
	}
}

func writeStateDocument(t *testing.T, path, document string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(state parent) error = %v", err)
	}
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("os.WriteFile(state) error = %v", err)
	}
}

func stateUnixSocket(t *testing.T, path string) string {
	t.Helper()
	listener, err := net.Listen("unix", path)
	if errors.Is(err, os.ErrPermission) {
		t.Skipf("Unix socket creation is unavailable in this sandbox: %v", err)
	}
	if err != nil {
		t.Fatalf("net.Listen(unix, %q) error = %v", path, err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	return path
}

func temporaryStateSocketPath(t *testing.T) string {
	t.Helper()
	file, err := os.CreateTemp("/tmp", "dot-cp4-state-socket-*")
	if err != nil {
		t.Fatalf("os.CreateTemp(socket path) error = %v", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatalf("close temporary socket path error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove temporary socket path error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	return path
}
