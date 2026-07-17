package paths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTargetIdentity_CaseSemantics(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "CaseProbe")
	alias := filepath.Join(root, "caseprobe")
	if err := os.WriteFile(actual, []byte("content"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", actual, err)
	}

	actualID := mustResolveTargetIdentity(t, actual)
	_, lookupErr := os.Lstat(alias)
	aliasID, identityErr := ResolveTargetIdentity(alias)
	assertAliasMatchesFilesystem(t, "case", lookupErr, actualID, aliasID, identityErr)

	upperMissing := filepath.Join(root, "MissingCase")
	lowerMissing := filepath.Join(root, "missingcase")
	assertMissingPairMatchesFilesystem(t, lookupErr == nil, upperMissing, lowerMissing)
}

func TestTargetIdentity_UnicodeSemantics(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "caf\u00e9")
	alias := filepath.Join(root, "cafe\u0301")
	if err := os.WriteFile(actual, []byte("content"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", actual, err)
	}

	actualID := mustResolveTargetIdentity(t, actual)
	_, lookupErr := os.Lstat(alias)
	aliasID, identityErr := ResolveTargetIdentity(alias)
	assertAliasMatchesFilesystem(t, "Unicode", lookupErr, actualID, aliasID, identityErr)

	composedMissing := filepath.Join(root, "missing-\u00e9")
	decomposedMissing := filepath.Join(root, "missing-e\u0301")
	assertMissingPairMatchesFilesystem(t, lookupErr == nil, composedMissing, decomposedMissing)
}

func TestTargetIdentity_HardLinkSemantics(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first-link")
	second := filepath.Join(root, "second-link")
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
	if mustResolveTargetIdentity(t, first).Equal(mustResolveTargetIdentity(t, second)) {
		t.Error("hard-link directory entries have the same target identity")
	}
}

func TestTargetIdentity_HardLinkAliasDoesNotMergeSibling(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "FirstLink")
	firstAlias := filepath.Join(root, "firstlink")
	second := filepath.Join(root, "second-link")
	if err := os.WriteFile(first, []byte("content"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", first, err)
	}
	if err := os.Link(first, second); err != nil {
		t.Fatalf("os.Link(%q, %q) error = %v", first, second, err)
	}

	_, lookupErr := os.Lstat(firstAlias)
	aliasID, identityErr := ResolveTargetIdentity(firstAlias)
	firstID := mustResolveTargetIdentity(t, first)
	assertAliasMatchesFilesystem(t, "hard-link case", lookupErr, firstID, aliasID, identityErr)
	if lookupErr == nil && aliasID.Equal(mustResolveTargetIdentity(t, second)) {
		t.Error("case alias of one hard link merged with a different directory entry")
	}
}

func TestTargetIdentity_SymlinkAliasWithCaseSemantics(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "RealDirectory")
	actualChild := filepath.Join(realDirectory, "CaseChild")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", realDirectory, err)
	}
	if err := os.WriteFile(actualChild, []byte("content"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", actualChild, err)
	}

	aliasDirectory := filepath.Join(root, "alias")
	if err := os.Symlink(realDirectory, aliasDirectory); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", realDirectory, aliasDirectory, err)
	}
	aliasChild := filepath.Join(aliasDirectory, "casechild")
	_, lookupErr := os.Lstat(aliasChild)
	aliasID, identityErr := ResolveTargetIdentity(aliasChild)
	assertAliasMatchesFilesystem(
		t,
		"case through ancestor symlink",
		lookupErr,
		mustResolveTargetIdentity(t, actualChild),
		aliasID,
		identityErr,
	)
}

func TestTargetIdentity_CaseAliasInAncestorDirectory(t *testing.T) {
	root := t.TempDir()
	actualDirectory := filepath.Join(root, "CaseDirectory")
	actualChild := filepath.Join(actualDirectory, "child")
	if err := os.Mkdir(actualDirectory, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", actualDirectory, err)
	}
	if err := os.WriteFile(actualChild, []byte("content"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", actualChild, err)
	}

	aliasChild := filepath.Join(root, "casedirectory", "child")
	_, lookupErr := os.Lstat(aliasChild)
	aliasID, identityErr := ResolveTargetIdentity(aliasChild)
	assertAliasMatchesFilesystem(
		t,
		"case in ancestor directory",
		lookupErr,
		mustResolveTargetIdentity(t, actualChild),
		aliasID,
		identityErr,
	)
}

func assertAliasMatchesFilesystem(
	t *testing.T,
	name string,
	lookupErr error,
	actualID TargetIdentity,
	aliasID TargetIdentity,
	identityErr error,
) {
	t.Helper()

	switch {
	case lookupErr == nil:
		if identityErr != nil {
			t.Fatalf("%s alias is accepted by filesystem but identity resolution failed: %v", name, identityErr)
		}
		if !actualID.Equal(aliasID) {
			t.Fatalf("%s alias is accepted by filesystem but identities differ", name)
		}
	case os.IsNotExist(lookupErr):
		if errors.Is(identityErr, ErrIdentityUnavailable) {
			return
		}
		if identityErr != nil {
			t.Fatalf("%s alias identity error = %v", name, identityErr)
		}
		if actualID.Equal(aliasID) {
			t.Fatalf("%s names are distinct on filesystem but identities are equal", name)
		}
	default:
		t.Fatalf("observe %s filesystem alias: %v", name, lookupErr)
	}
}

func assertMissingPairMatchesFilesystem(t *testing.T, equivalent bool, first, second string) {
	t.Helper()

	firstID, firstErr := ResolveTargetIdentity(first)
	secondID, secondErr := ResolveTargetIdentity(second)
	if errors.Is(firstErr, ErrIdentityUnavailable) || errors.Is(secondErr, ErrIdentityUnavailable) {
		if !errors.Is(firstErr, ErrIdentityUnavailable) || !errors.Is(secondErr, ErrIdentityUnavailable) {
			t.Fatalf("missing-name pair has asymmetric availability: first=%v second=%v", firstErr, secondErr)
		}
		assertPathsMissing(t, first, second)
		return
	}
	if firstErr != nil || secondErr != nil {
		t.Fatalf("resolve missing-name pair: first=%v second=%v", firstErr, secondErr)
	}
	if firstID.Equal(secondID) != equivalent {
		t.Fatalf("missing-name equality = %v, filesystem equivalence = %v", firstID.Equal(secondID), equivalent)
	}
	assertPathsMissing(t, first, second)
}
