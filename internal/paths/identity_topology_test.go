package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestResolveTarget_LeafSymlinkIsNotFollowed(t *testing.T) {
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

	aliasResolution := mustResolveTarget(t, alias)
	realResolution := mustResolveTarget(t, realDirectory)
	aliasChildResolution := mustResolveTarget(t, filepath.Join(alias, "child"))
	realChildResolution := mustResolveTarget(t, child)

	if aliasResolution.Equal(realResolution) {
		t.Error("leaf symlink identity equals its destination")
	}
	if !aliasResolution.IsAncestorOf(aliasChildResolution) {
		t.Error("leaf symlink entry is not an ancestor traversed by its displayed child path")
	}
	if aliasResolution.IsAncestorOf(realChildResolution) {
		t.Error("leaf symlink entry is an ancestor of a child path that does not traverse it")
	}
	if !realResolution.IsAncestorOf(aliasChildResolution) || !aliasChildResolution.Equal(realChildResolution) {
		t.Error("ancestor symlink path does not resolve under the real directory identity")
	}
}

func TestResolveTarget_SymlinkExpansionTopology(t *testing.T) {
	t.Run("chained aliases", func(t *testing.T) {
		root := t.TempDir()
		realDirectory := filepath.Join(root, "real")
		child := filepath.Join(realDirectory, "child")
		if err := os.Mkdir(realDirectory, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", realDirectory, err)
		}
		if err := os.WriteFile(child, []byte("content"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", child, err)
		}

		bridge := filepath.Join(root, "bridge")
		alias := filepath.Join(root, "alias")
		if err := os.Symlink("real", bridge); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", "real", bridge, err)
		}
		if err := os.Symlink("bridge", alias); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", "bridge", alias, err)
		}

		bridgeResolution := mustResolveTarget(t, bridge)
		aliasChildResolution := mustResolveTarget(t, filepath.Join(alias, "child"))
		realChildResolution := mustResolveTarget(t, child)
		if !bridgeResolution.IsAncestorOf(aliasChildResolution) {
			t.Error("intermediate symlink target is absent from traversal ancestors")
		}
		if bridgeResolution.IsAncestorOf(realChildResolution) {
			t.Error("intermediate symlink target is an ancestor of a path that does not traverse it")
		}
		if !aliasChildResolution.Equal(realChildResolution) {
			t.Error("chained symlink path does not resolve to the real leaf identity")
		}
	})

	t.Run("lexically canceled target component", func(t *testing.T) {
		root := t.TempDir()
		detour := filepath.Join(root, "detour")
		realDirectory := filepath.Join(root, "real")
		child := filepath.Join(realDirectory, "child")
		for _, path := range []string{detour, realDirectory} {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatalf("os.Mkdir(%q) error = %v", path, err)
			}
		}
		if err := os.WriteFile(child, []byte("content"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", child, err)
		}

		alias := filepath.Join(root, "alias")
		if err := os.Symlink(filepath.FromSlash("detour/../real"), alias); err != nil {
			t.Fatalf("os.Symlink(detour/../real, %q) error = %v", alias, err)
		}

		detourResolution := mustResolveTarget(t, detour)
		aliasChildResolution := mustResolveTarget(t, filepath.Join(alias, "child"))
		realChildResolution := mustResolveTarget(t, child)
		if !detourResolution.IsAncestorOf(aliasChildResolution) {
			t.Error("symlink target component traversed before .. is absent from traversal ancestors")
		}
		if detourResolution.IsAncestorOf(realChildResolution) {
			t.Error("symlink target detour is an ancestor of a path that does not traverse it")
		}
		if !aliasChildResolution.Equal(realChildResolution) {
			t.Error("symlink target containing .. does not resolve to the real leaf identity")
		}
	})
}

func TestResolveTarget_EqualAliasIsNotItsOwnAncestor(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(".", alias); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", ".", alias, err)
	}

	aliasResolution := mustResolveTarget(t, alias)
	nestedAliasResolution := mustResolveTarget(t, filepath.Join(alias, "alias"))
	if !aliasResolution.Equal(nestedAliasResolution) {
		t.Fatal("nested alias does not resolve to the same leaf target")
	}
	if aliasResolution.IsAncestorOf(nestedAliasResolution) || nestedAliasResolution.IsAncestorOf(aliasResolution) {
		t.Error("equal leaf target is treated as its own strict ancestor")
	}
}

func TestResolveTarget_KernelRejectsDeepSymlinkChain(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", realDirectory, err)
	}
	child := filepath.Join(realDirectory, "child")
	if err := os.WriteFile(child, []byte("content"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", child, err)
	}

	next := "real"
	for index := 63; index >= 0; index-- {
		name := fmt.Sprintf("link-%02d", index)
		path := filepath.Join(root, name)
		if err := os.Symlink(next, path); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", next, path, err)
		}
		next = name
	}
	target := filepath.Join(root, "link-00", "child")
	if _, err := os.Stat(target); !errors.Is(err, syscall.ELOOP) {
		t.Fatalf("deep symlink fixture os.Stat(%q) error = %v, want ELOOP", target, err)
	}

	checkTargetResolvers(t, target, func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, ErrPathBlocked) {
			t.Fatalf("resolver error = %v, want ErrPathBlocked", err)
		}
	})
}

func TestResolveTarget_PreservesOrdinaryIOCause(t *testing.T) {
	overlongComponent := strings.Repeat("x", 4096)
	target := filepath.Join(t.TempDir(), overlongComponent, "child")

	checkTargetResolvers(t, target, func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, syscall.ENAMETOOLONG) {
			t.Fatalf("resolver error = %v, want ENAMETOOLONG cause", err)
		}
		if errors.Is(err, ErrPathBlocked) {
			t.Fatalf("resolver error = %v, must not be ErrPathBlocked", err)
		}
	})
}

func TestResolveTarget_BlockedAncestors(t *testing.T) {
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
			checkTargetResolvers(t, path, func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, ErrPathBlocked) {
					t.Fatalf("resolver error = %v, want ErrPathBlocked", err)
				}
			})
		})
	}

	for _, path := range []string{fifo, dangling} {
		t.Run(filepath.Base(path)+" leaf", func(t *testing.T) {
			checkTargetResolvers(t, path, func(t *testing.T, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("resolver error = %v", err)
				}
			})
		})
	}
}

func TestResolveTarget_MissingTail(t *testing.T) {
	root := t.TempDir()
	missingParent := filepath.Join(root, "missing")
	missingChild := filepath.Join(missingParent, "child")

	parentResolution, err := ResolveTarget(missingParent)
	if errors.Is(err, ErrIdentityUnavailable) {
		assertPathsMissing(t, missingParent, missingChild)
		return
	}
	if err != nil {
		t.Fatalf("ResolveTarget(%q) error = %v", missingParent, err)
	}
	childResolution, err := ResolveTarget(missingChild)
	if err != nil {
		t.Fatalf("ResolveTarget(%q) error = %v", missingChild, err)
	}
	if !parentResolution.IsAncestorOf(childResolution) {
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

func checkTargetResolvers(t *testing.T, path string, check func(*testing.T, error)) {
	t.Helper()

	resolvers := []struct {
		name    string
		resolve func(string) error
	}{
		{name: "identity", resolve: func(path string) error {
			_, err := ResolveTargetIdentity(path)
			return err
		}},
		{name: "resolution", resolve: func(path string) error {
			_, err := ResolveTarget(path)
			return err
		}},
	}
	for _, resolver := range resolvers {
		t.Run(resolver.name, func(t *testing.T) {
			check(t, resolver.resolve(path))
		})
	}
}
