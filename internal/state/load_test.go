package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/paths"
)

func TestLoad_DistinguishesMissingLoadedAndInvalidStates(t *testing.T) {
	root := t.TempDir()
	missingPath := filepath.Join(root, "missing.json")
	before := snapshotFiles(t, root)

	loaded, err := Load(missingPath)
	if _, ok := loaded.Snapshot(); err != nil || loaded.Status() != StatusMissing || !loaded.Missing() || ok {
		t.Fatalf("Load(missing) = (%#v, %v), want missing without Snapshot", loaded, err)
	}
	if after := snapshotFiles(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("Load(missing) changed tree: before=%v after=%v", before, after)
	}

	validPath := writeStateFile(t, root, "valid.json", marshalDocument(t, testDocument()))
	loaded, err = Load(validPath)
	snapshot, ok := loaded.Snapshot()
	if err != nil || loaded.Status() != StatusLoaded || loaded.Missing() || !ok || snapshot.Version() != 1 {
		t.Fatalf("Load(valid) = (%#v, %v), want loaded v1 Snapshot", loaded, err)
	}

	tests := []struct {
		name string
		raw  string
		want error
	}{
		{name: "corrupt", raw: `{"version":1,"entries":{},"entries":{},"run_once":{}}`, want: ErrCorrupt},
		{name: "too-new", raw: `{"version":2}`, want: ErrTooNew},
		{name: "unsupported-rendered", raw: renderedDocument(), want: ErrUnsupportedRendered},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeStateFile(t, root, tt.name+".json", []byte(tt.raw))
			loaded, err := Load(path)
			if snapshot, ok := loaded.Snapshot(); !errors.Is(err, tt.want) || loaded.Status() != StatusInvalid || ok || snapshot.Version() != 0 {
				t.Fatalf("Load() = (%#v, %v), want invalid without Snapshot and errors.Is(%v)", loaded, err, tt.want)
			}
		})
	}
}

func TestLoad_RejectsPathAndReadErrorsWithoutClassifyingStateCorrupt(t *testing.T) {
	if _, err := Load("relative/state.json"); err == nil || errors.Is(err, ErrCorrupt) {
		t.Fatalf("Load(relative) error = %v, want non-corrupt input path error", err)
	}

	root := t.TempDir()
	dangling := filepath.Join(root, "state.json")
	if err := os.Symlink(filepath.Join(root, "missing-target"), dangling); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	if loaded, err := Load(dangling); err == nil || loaded.Status() != StatusInvalid || errors.Is(err, ErrCorrupt) {
		t.Fatalf("Load(dangling) = (%v, %v), want StatusInvalid non-corrupt read error", loaded.Status(), err)
	}
}

func TestValidateTargetIdentities_RejectsAliasedStateKeys(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	realDirectory := filepath.Join(home, "real")
	if err := os.MkdirAll(realDirectory, 0o700); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(realDirectory, "file"), []byte("existing leaf"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	if err := os.Symlink(realDirectory, filepath.Join(home, "alias")); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	document := testDocument()
	document["entries"] = map[string]any{
		"~/real/file":  entryForModule("real"),
		"~/alias/file": entryForModule("alias"),
	}
	snapshot, err := Decode(marshalDocument(t, document))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	err = ValidateTargetIdentities(snapshot, home)
	if !errors.Is(err, ErrCorrupt) || !errors.Is(err, ErrTargetIdentityConflict) || errors.Is(err, ErrPathValidation) {
		t.Fatalf("ValidateTargetIdentities() error = %v, want corrupt identity conflict", err)
	}
}

func TestValidateTargetIdentities_KeepsRuntimePathErrorsSeparateFromCorrupt(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "blocked"), []byte("user data"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	document := testDocument()
	document["entries"] = map[string]any{"~/blocked/child": entryForModule("app")}
	snapshot, err := Decode(marshalDocument(t, document))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	err = ValidateTargetIdentities(snapshot, home)
	if !errors.Is(err, ErrPathValidation) || !errors.Is(err, paths.ErrPathBlocked) || errors.Is(err, ErrCorrupt) {
		t.Fatalf("ValidateTargetIdentities() error = %v, want path validation + ErrPathBlocked, not corrupt", err)
	}

	err = validateTargetIdentities(snapshot, home, func(string) (paths.TargetIdentity, error) {
		return paths.TargetIdentity{}, fmt.Errorf("capability probe: %w", paths.ErrIdentityUnavailable)
	})
	if !errors.Is(err, ErrPathValidation) || !errors.Is(err, paths.ErrIdentityUnavailable) || errors.Is(err, ErrCorrupt) {
		t.Fatalf("validateTargetIdentities(capability) error = %v, want path validation + ErrIdentityUnavailable", err)
	}
}

func TestValidateTargetIdentities_IsReadOnlyAndKeepsDifferentHardLinkLeavesDistinct(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}
	first := filepath.Join(home, "first")
	second := filepath.Join(home, "second")
	if err := os.WriteFile(first, []byte("same inode"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	if err := os.Link(first, second); err != nil {
		t.Fatalf("os.Link() error = %v", err)
	}
	document := testDocument()
	document["entries"] = map[string]any{
		"~/first":  entryForModule("first"),
		"~/second": entryForModule("second"),
	}
	snapshot, err := Decode(marshalDocument(t, document))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	before := snapshotFiles(t, root)
	if err := ValidateTargetIdentities(snapshot, home); err != nil {
		t.Fatalf("ValidateTargetIdentities() error = %v, want nil", err)
	}
	if after := snapshotFiles(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("ValidateTargetIdentities() changed tree: before=%v after=%v", before, after)
	}
}

func entryForModule(module string) map[string]any {
	return map[string]any{
		"module":     module,
		"kind":       "symlink",
		"source":     "modules/" + module + "/file",
		"link_dest":  "/repo/modules/" + module + "/file",
		"applied_at": "2026-07-14T10:00:00Z",
	}
}

func renderedDocument() string {
	return `{"version":1,"entries":{"~/file":{"module":"app","kind":"rendered","source":"modules/app/file.tmpl","hash":"sha256:` + strings.Repeat("a", 64) + `","applied_at":"2026-07-14T10:00:00Z"}},"run_once":{}}`
}

func writeStateFile(t *testing.T, root, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}

func snapshotFiles(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, relative+":"+entry.Type().String())
		return nil
	}); err != nil {
		t.Fatalf("filepath.WalkDir() error = %v", err)
	}
	return entries
}
