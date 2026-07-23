package planner

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestObserveTarget_ClassifiesLeafWithoutFollowingSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	realTarget := filepath.Join(root, "real")
	if err := os.WriteFile(realTarget, []byte("private bytes\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", realTarget, err)
	}
	rawDestination := "real"
	if err := os.Symlink(rawDestination, target); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", rawDestination, target, err)
	}
	before := snapshotObservationTree(t, root)

	observed, err := ObserveTarget(target)
	if err != nil {
		t.Fatalf("ObserveTarget() error = %v", err)
	}
	if observed.Kind != ObjectSymlink || observed.LinkDest != rawDestination {
		t.Fatalf("ObserveTarget() = %#v, want raw symlink %q", observed, rawDestination)
	}
	if observed.Hash != "" {
		t.Fatalf("symlink observation contains followed content evidence: %#v", observed)
	}
	if after := snapshotObservationTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("ObserveTarget() changed fixture: before=%v after=%v", before, after)
	}
}

func TestObserveTarget_RegularFileCarriesMetadataWithoutReadingDigest(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	content := []byte("regular bytes\n")
	if err := os.WriteFile(target, content, 0o640); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", target, err)
	}

	observed, err := ObserveTarget(target)
	if err != nil {
		t.Fatalf("ObserveTarget() error = %v", err)
	}
	if observed.Kind != ObjectRegular {
		t.Fatalf("ObserveTarget() = %#v, want regular metadata", observed)
	}
	if observed.Hash != "" {
		t.Fatalf("ObserveTarget() hash = %q, want no unrequested digest", observed.Hash)
	}
	if observed.Mode.Perm() != 0o640 {
		t.Fatalf("ObserveTarget() mode = %v, want 0640", observed.Mode)
	}
}

func TestObserveTarget_ClassifiesMissingDirectoryAndSpecial(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o750); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", directory, err)
	}

	tests := []struct {
		name string
		path string
		want ObjectKind
	}{
		{name: "missing", path: filepath.Join(root, "missing", "target"), want: ObjectMissing},
		{name: "directory", path: directory, want: ObjectDirectory},
	}
	if runtime.GOOS != "windows" {
		fifo := filepath.Join(root, "fifo")
		if err := makeFIFO(fifo); err != nil {
			t.Fatalf("makeFIFO(%q) error = %v", fifo, err)
		}
		tests = append(tests, struct {
			name string
			path string
			want ObjectKind
		}{name: "special", path: fifo, want: ObjectSpecial})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observed, err := ObserveTarget(tt.path)
			if err != nil {
				t.Fatalf("ObserveTarget(%q) error = %v", tt.path, err)
			}
			if observed.Kind != tt.want {
				t.Fatalf("ObserveTarget(%q) kind = %v, want %v", tt.path, observed.Kind, tt.want)
			}
		})
	}
}

func TestObserveTarget_RejectsInvalidAndUnsafeMissingPaths(t *testing.T) {
	if _, err := ObserveTarget("relative"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("ObserveTarget(relative) error = %v, want absolute-path error", err)
	}

	root := t.TempDir()
	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink("missing-directory", dangling); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	if _, err := ObserveTarget(filepath.Join(dangling, "target")); err == nil {
		t.Fatal("ObserveTarget() error = nil, want dangling ancestor error")
	}
}

func snapshotObservationTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		value := info.Mode().String()
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			destination, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value += ":" + destination
		case info.Mode().IsRegular():
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += ":" + string(content)
		}
		snapshot[filepath.ToSlash(relative)] = value
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot tree %q: %v", root, err)
	}
	return snapshot
}
