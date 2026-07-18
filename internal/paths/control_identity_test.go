package paths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveControlPathIdentity_FilesystemRoot(t *testing.T) {
	root := string(filepath.Separator)
	control := mustResolveControlPathIdentity(t, root)
	target := mustResolveTarget(t, filepath.Join(t.TempDir(), "target"))

	if !control.OverlapsTarget(target) {
		t.Error("filesystem root control does not overlap a descendant target")
	}
	if _, err := ResolveTarget(root); err == nil {
		t.Fatal("ResolveTarget(filesystem root) error = nil, want target root rejection")
	}
}

func TestResolveControlPathIdentity_LeafSymlinkConsumption(t *testing.T) {
	root := t.TempDir()
	realRepo := filepath.Join(root, "real-repo")
	if err := os.Mkdir(realRepo, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", realRepo, err)
	}
	realConfig := filepath.Join(realRepo, "config.toml")
	if err := os.WriteFile(realConfig, []byte("profile = \"test\"\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", realConfig, err)
	}

	repoAlias := filepath.Join(root, "repo-alias")
	if err := os.Symlink(realRepo, repoAlias); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", realRepo, repoAlias, err)
	}
	configAlias := filepath.Join(root, "config-alias")
	if err := os.Symlink(realConfig, configAlias); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", realConfig, configAlias, err)
	}

	repoControl := mustResolveControlPathIdentity(t, repoAlias)
	configControl := mustResolveControlPathIdentity(t, configAlias)
	if !repoControl.OverlapsTarget(mustResolveTarget(t, realConfig)) {
		t.Error("repo directory symlink control does not overlap a target in the real repo tree")
	}
	if !configControl.OverlapsTarget(mustResolveTarget(t, realConfig)) {
		t.Error("config file symlink control does not overlap its consumed file target")
	}
	if !configControl.OverlapsTarget(mustResolveTarget(t, configAlias)) {
		t.Error("config file symlink control does not overlap its displayed leaf target")
	}
	if !repoControl.Overlaps(configControl) {
		t.Error("repo directory symlink control does not overlap config consumed inside the real repo tree")
	}

	unrelated := mustResolveTarget(t, filepath.Join(root, "unrelated"))
	if repoControl.OverlapsTarget(unrelated) || configControl.OverlapsTarget(unrelated) {
		t.Error("control path overlaps an unrelated target")
	}
}

func TestResolveControlPathIdentity_ChainedLeafSymlink(t *testing.T) {
	root := t.TempDir()
	realFile := filepath.Join(root, "real")
	if err := os.WriteFile(realFile, []byte("content"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", realFile, err)
	}
	bridge := filepath.Join(root, "bridge")
	if err := os.Symlink("real", bridge); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", "real", bridge, err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink("bridge", alias); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", "bridge", alias, err)
	}

	control := mustResolveControlPathIdentity(t, alias)
	for _, target := range []string{alias, bridge, realFile} {
		if !control.OverlapsTarget(mustResolveTarget(t, target)) {
			t.Errorf("chained control symlink does not overlap traversed target %q", target)
		}
	}
}

func TestResolveControlPathIdentity_MissingAndDangling(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	control := mustResolveControlPathIdentity(t, missing)
	if !control.OverlapsTarget(mustResolveTarget(t, missing)) {
		t.Error("missing control path does not overlap the same missing target")
	}
	assertPathsMissing(t, missing)

	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink("absent", dangling); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", "absent", dangling, err)
	}
	if _, err := ResolveControlPathIdentity(dangling); !errors.Is(err, ErrPathBlocked) {
		t.Fatalf("ResolveControlPathIdentity(%q) error = %v, want ErrPathBlocked", dangling, err)
	}
}

func TestControlPathResolution_ZeroValue(t *testing.T) {
	var zero ControlPathResolution
	target := mustResolveTarget(t, filepath.Join(t.TempDir(), "target"))
	if zero.OverlapsTarget(target) || zero.Overlaps(zero) {
		t.Error("zero control path resolution overlaps another path")
	}
}

func mustResolveControlPathIdentity(t *testing.T, path string) ControlPathResolution {
	t.Helper()

	resolution, err := ResolveControlPathIdentity(path)
	if err != nil {
		t.Fatalf("ResolveControlPathIdentity(%q) error = %v", path, err)
	}
	return resolution
}
