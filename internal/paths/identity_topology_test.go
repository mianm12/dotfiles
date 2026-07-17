package paths

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestResolveTargetIdentity_SymlinkAncestors(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	child := filepath.Join(realDirectory, "child")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", realDirectory, err)
	}
	if err := os.WriteFile(child, []byte("content"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", child, err)
	}

	absoluteAlias := filepath.Join(root, "absolute-alias")
	relativeAlias := filepath.Join(root, "relative-alias")
	if err := os.Symlink(realDirectory, absoluteAlias); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", realDirectory, absoluteAlias, err)
	}
	if err := os.Symlink("real", relativeAlias); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", "real", relativeAlias, err)
	}

	realID := mustResolveTargetIdentity(t, child)
	absoluteID := mustResolveTargetIdentity(t, filepath.Join(absoluteAlias, "child"))
	relativeID := mustResolveTargetIdentity(t, filepath.Join(relativeAlias, "child"))
	if !realID.Equal(absoluteID) || !realID.Equal(relativeID) {
		t.Error("ancestor symlink aliases do not resolve to the real target identity")
	}
}

func TestResolveTargetIdentity_LeafSymlinkIsNotFollowed(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	child := filepath.Join(realDirectory, "child")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", realDirectory, err)
	}
	if err := os.WriteFile(child, []byte("content"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", child, err)
	}

	alias := filepath.Join(root, "alias")
	if err := os.Symlink("real", alias); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", "real", alias, err)
	}

	aliasID := mustResolveTargetIdentity(t, alias)
	realID := mustResolveTargetIdentity(t, realDirectory)
	aliasChildID := mustResolveTargetIdentity(t, filepath.Join(alias, "child"))
	realChildID := mustResolveTargetIdentity(t, child)

	if aliasID.Equal(realID) {
		t.Error("leaf symlink identity equals its destination")
	}
	if aliasID.IsAncestorOf(aliasChildID) || aliasID.IsAncestorOf(realChildID) {
		t.Error("leaf symlink identity is an ancestor of a path reached through its destination")
	}
	if !realID.IsAncestorOf(aliasChildID) || !aliasChildID.Equal(realChildID) {
		t.Error("ancestor symlink path does not resolve under the real directory identity")
	}
}

func TestResolveTargetIdentity_BlockedAncestors(t *testing.T) {
	root := t.TempDir()

	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("content"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", file, err)
	}
	fileAlias := filepath.Join(root, "file-alias")
	if err := os.Symlink("file", fileAlias); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", "file", fileAlias, err)
	}

	fifo := filepath.Join(root, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("syscall.Mkfifo(%q) error = %v", fifo, err)
	}

	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink("missing", dangling); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", "missing", dangling, err)
	}

	loop := filepath.Join(root, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", "loop", loop, err)
	}

	for _, path := range []string{
		filepath.Join(file, "child"),
		filepath.Join(fileAlias, "child"),
		filepath.Join(fifo, "child"),
		filepath.Join(dangling, "child"),
		filepath.Join(loop, "child"),
	} {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			_, err := ResolveTargetIdentity(path)
			if !errors.Is(err, ErrPathBlocked) {
				t.Fatalf("ResolveTargetIdentity(%q) error = %v, want ErrPathBlocked", path, err)
			}
		})
	}

	if _, err := ResolveTargetIdentity(fifo); err != nil {
		t.Fatalf("ResolveTargetIdentity(leaf FIFO) error = %v", err)
	}
	if _, err := ResolveTargetIdentity(dangling); err != nil {
		t.Fatalf("ResolveTargetIdentity(dangling leaf symlink) error = %v", err)
	}
}

func TestResolveTargetIdentity_MissingTail(t *testing.T) {
	root := t.TempDir()
	missingParent := filepath.Join(root, "missing")
	missingChild := filepath.Join(missingParent, "child")

	parentID, err := ResolveTargetIdentity(missingParent)
	if errors.Is(err, ErrIdentityUnavailable) {
		assertPathsMissing(t, missingParent, missingChild)
		return
	}
	if err != nil {
		t.Fatalf("ResolveTargetIdentity(%q) error = %v", missingParent, err)
	}
	childID, err := ResolveTargetIdentity(missingChild)
	if err != nil {
		t.Fatalf("ResolveTargetIdentity(%q) error = %v", missingChild, err)
	}
	if !parentID.IsAncestorOf(childID) {
		t.Error("missing parent identity is not an ancestor of its missing child")
	}
	assertPathsMissing(t, missingParent, missingChild)
}

func assertPathsMissing(t *testing.T, paths ...string) {
	t.Helper()

	for _, path := range paths {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("path %q changed during identity resolution: os.Lstat error = %v", path, err)
		}
	}
}
