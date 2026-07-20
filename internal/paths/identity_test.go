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

	var zeroResolution TargetResolution
	if zeroResolution.Equal(zeroResolution) {
		t.Error("zero TargetResolution compares equal to itself")
	}
	if zeroResolution.IsAncestorOf(zeroResolution) {
		t.Error("zero TargetResolution is an ancestor of itself")
	}
}

func TestTargetResolution_Relations(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	child := filepath.Join(alpha, "child")
	alphaPeer := filepath.Join(root, "alpha-peer")
	for _, path := range []string{alpha, child, alphaPeer} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", path, err)
		}
	}

	alphaResolution := mustResolveTarget(t, alpha)
	alphaAgainResolution := mustResolveTarget(t, filepath.Join(root, ".", "alpha"))
	childResolution := mustResolveTarget(t, child)
	peerResolution := mustResolveTarget(t, alphaPeer)
	if !alphaResolution.Identity().Equal(mustResolveTargetIdentity(t, alpha)) {
		t.Error("resolution leaf identity differs from identity-only resolution")
	}

	if !alphaResolution.Equal(alphaAgainResolution) {
		t.Error("same target identities compare different")
	}
	if alphaResolution.Equal(childResolution) || alphaResolution.Equal(peerResolution) {
		t.Error("different target identities compare equal")
	}
	if !alphaResolution.IsAncestorOf(childResolution) {
		t.Error("alpha target is not an ancestor of its child")
	}
	if !childResolution.Traverses(alphaResolution) || alphaResolution.Traverses(childResolution) {
		t.Error("target traversal relation does not preserve direction")
	}
	if alphaResolution.IsAncestorOf(alphaAgainResolution) {
		t.Error("target is a strict ancestor of itself")
	}
	if alphaResolution.IsAncestorOf(peerResolution) {
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

func TestResolveTargetIdentity_BasicRejectsInvalidPath(t *testing.T) {
	for _, path := range []string{"", "relative/path", string(filepath.Separator)} {
		t.Run(path, func(t *testing.T) {
			if _, err := ResolveTargetIdentity(path); err == nil {
				t.Fatalf("ResolveTargetIdentity(%q) error = nil", path)
			}
			if _, err := ResolveTarget(path); err == nil {
				t.Fatalf("ResolveTarget(%q) error = nil", path)
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

func mustResolveTarget(t *testing.T, path string) TargetResolution {
	t.Helper()

	resolution, err := ResolveTarget(path)
	if err != nil {
		t.Fatalf("ResolveTarget(%q) error = %v", path, err)
	}
	return resolution
}
