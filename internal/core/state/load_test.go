package state_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	corestate "github.com/mianm12/dotfiles/internal/core/state"
)

func TestLoadStateMissingWarnsAndContinues(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(home) error = %v", err)
	}
	statePath := filepath.Join(root, "state", "state.json")
	before := snapshotTree(t, root)

	loaded, err := corestate.Load(statePath, home)
	if err != nil {
		t.Fatalf("Load(missing) error = %v", err)
	}
	if !loaded.Missing || loaded.Warning == "" {
		t.Fatalf("Load(missing) = %#v, want missing with warning", loaded)
	}
	if loaded.Snapshot.Home != home || len(loaded.Snapshot.Links) != 0 {
		t.Fatalf("missing snapshot = %#v, want empty state bound to %q", loaded.Snapshot, home)
	}
	assertTreeUnchanged(t, root, before)
}

func TestLoadInvalidLegacyAndTooNewStateRejectReadOnly(t *testing.T) {
	tests := []struct {
		name     string
		document func(string) string
		want     error
	}{
		{
			name: "corrupt JSON",
			document: func(string) string {
				return "{"
			},
			want: corestate.ErrInvalid,
		},
		{
			name: "unknown field",
			document: func(home string) string {
				return fmt.Sprintf(
					`{"version":5,"home":%q,"links":[],"unknown":true}`,
					home,
				)
			},
			want: corestate.ErrInvalid,
		},
		{
			name: "legacy version",
			document: func(string) string {
				return `{"version":1,"entries":{},"run_once":{}}`
			},
			want: corestate.ErrLegacyVersion,
		},
		{
			name: "legacy version 2",
			document: func(home string) string {
				return fmt.Sprintf(`{"version":2,"home":%q,"modules":{}}`, home)
			},
			want: corestate.ErrLegacyVersion,
		},
		{
			name: "legacy version 3",
			document: func(home string) string {
				return fmt.Sprintf(`{"version":3,"home":%q,"records":[]}`, home)
			},
			want: corestate.ErrLegacyVersion,
		},
		{
			name: "legacy version 4",
			document: func(home string) string {
				return fmt.Sprintf(`{"version":4,"home":%q,"links":[]}`, home)
			},
			want: corestate.ErrLegacyVersion,
		},
		{
			name: "too new",
			document: func(string) string {
				return `{"version":6}`
			},
			want: corestate.ErrTooNew,
		},
		{
			name: "home mismatch",
			document: func(home string) string {
				return fmt.Sprintf(
					`{"version":5,"home":%q,"links":[]}`,
					filepath.Join(home, "other"),
				)
			},
			want: corestate.ErrHomeMismatch,
		},
		{
			name: "missing dest",
			document: func(home string) string {
				return fmt.Sprintf(
					`{"version":5,"home":%q,"links":[{"module":"app","placement":"config","target":%q}]}`,
					home,
					filepath.Join(".config", "app"),
				)
			},
			want: corestate.ErrInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatalf("os.Mkdir(home) error = %v", err)
			}
			statePath := filepath.Join(root, "state.json")
			if err := os.WriteFile(statePath, []byte(test.document(home)), 0o600); err != nil {
				t.Fatalf("os.WriteFile(state) error = %v", err)
			}
			before := snapshotTree(t, root)

			loaded, err := corestate.Load(statePath, home)
			if !errors.Is(err, test.want) {
				t.Fatalf("Load() = (%#v, %v), want %v", loaded, err, test.want)
			}
			if loaded.Missing || loaded.Warning != "" ||
				loaded.Snapshot.Home != "" || loaded.Snapshot.Links != nil {
				t.Fatalf("Load(error) returned partial result %#v", loaded)
			}
			assertTreeUnchanged(t, root, before)
		})
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

func assertTreeUnchanged(t *testing.T, root string, before map[string]treeEntry) {
	t.Helper()
	after := snapshotTree(t, root)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("state loading mutated fixture\nbefore=%v\nafter=%v", before, after)
	}
}
