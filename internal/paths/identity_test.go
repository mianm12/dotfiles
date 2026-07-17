package paths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTargetIdentity_ZeroValue(t *testing.T) {
	var zero TargetIdentity
	if zero.Equal(zero) {
		t.Error("zero TargetIdentity compares equal to itself")
	}
	if zero.IsAncestorOf(zero) {
		t.Error("zero TargetIdentity is an ancestor of itself")
	}
}

func TestTargetIdentity_Relations(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	child := filepath.Join(alpha, "child")
	alphaPeer := filepath.Join(root, "alpha-peer")
	for _, path := range []string{alpha, child, alphaPeer} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", path, err)
		}
	}

	alphaID := mustResolveTargetIdentity(t, alpha)
	alphaAgainID := mustResolveTargetIdentity(t, filepath.Join(root, ".", "alpha"))
	childID := mustResolveTargetIdentity(t, child)
	peerID := mustResolveTargetIdentity(t, alphaPeer)

	if !alphaID.Equal(alphaAgainID) {
		t.Error("same target identities compare different")
	}
	if alphaID.Equal(childID) || alphaID.Equal(peerID) {
		t.Error("different target identities compare equal")
	}
	if !alphaID.IsAncestorOf(childID) {
		t.Error("alpha target is not an ancestor of its child")
	}
	if alphaID.IsAncestorOf(alphaAgainID) {
		t.Error("target is a strict ancestor of itself")
	}
	if alphaID.IsAncestorOf(peerID) {
		t.Error("component string prefix is treated as an ancestor")
	}
}

func TestResolveTargetIdentity_BasicMissingLeaf(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")

	first, err := ResolveTargetIdentity(path)
	if errors.Is(err, ErrIdentityUnavailable) {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("ResolveTargetIdentity(%q) changed target: os.Lstat error = %v", path, statErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("ResolveTargetIdentity(%q) error = %v", path, err)
	}
	second := mustResolveTargetIdentity(t, path)
	if !first.Equal(second) {
		t.Error("stable missing target has different identities")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("ResolveTargetIdentity(%q) changed target: os.Lstat error = %v", path, err)
	}
}

func TestResolveTargetIdentity_BasicRejectsNonAbsolutePath(t *testing.T) {
	for _, path := range []string{"", "relative/path"} {
		t.Run(path, func(t *testing.T) {
			if _, err := ResolveTargetIdentity(path); err == nil {
				t.Fatalf("ResolveTargetIdentity(%q) error = nil", path)
			}
		})
	}
}

func TestResolveTargetIdentity_BasicHardLinksRemainDistinct(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	if err := os.WriteFile(first, []byte("content"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", first, err)
	}
	if err := os.Link(first, second); err != nil {
		t.Fatalf("os.Link(%q, %q) error = %v", first, second, err)
	}

	firstInfo, err := os.Lstat(first)
	if err != nil {
		t.Fatalf("os.Lstat(%q) error = %v", first, err)
	}
	secondInfo, err := os.Lstat(second)
	if err != nil {
		t.Fatalf("os.Lstat(%q) error = %v", second, err)
	}
	if !os.SameFile(firstInfo, secondInfo) {
		t.Fatal("hard-link fixture does not share a file object")
	}

	firstID := mustResolveTargetIdentity(t, first)
	secondID := mustResolveTargetIdentity(t, second)
	if firstID.Equal(secondID) {
		t.Error("different hard-link directory entries have the same target identity")
	}
}

func mustResolveTargetIdentity(t *testing.T, path string) TargetIdentity {
	t.Helper()

	identity, err := ResolveTargetIdentity(path)
	if err != nil {
		t.Fatalf("ResolveTargetIdentity(%q) error = %v", path, err)
	}
	return identity
}
