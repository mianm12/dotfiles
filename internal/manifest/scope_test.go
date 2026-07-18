package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/paths"
)

func TestPartialScope_CannotBypassUnrequestedIdentityConflict(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repository")
	controlPaths := writeGlobalControlFixture(t, home, repository)
	alphaRoot := filepath.Join(repository, "modules", "alpha")
	betaRoot := filepath.Join(repository, "modules", "beta")
	writeSourceFile(t, alphaRoot, "config", "alpha")
	writeSourceFile(t, betaRoot, "config", "beta")
	writeBoundaryTarget(t, filepath.Join(home, ".config", "config"))
	alpha := ResolvedModule{Name: "alpha", SourceDir: alphaRoot, TargetRoot: "~/.config"}
	beta := ResolvedModule{Name: "beta", SourceDir: betaRoot, TargetRoot: "~/.config"}

	requestedOnly, err := testResolvedProfile(alpha).ValidatePathBoundaries(controlPaths)
	if err != nil || len(requestedOnly.Entries()) != 1 {
		t.Fatalf("requested-only proof = (%#v, %v), want valid single module", requestedOnly, err)
	}
	validated, err := testResolvedProfile(alpha, beta).ValidatePathBoundaries(controlPaths)
	if !errors.Is(err, paths.ErrTargetOverlap) || validated.Entries() != nil {
		t.Fatalf("full profile validation = (%#v, %v), want zero ErrTargetOverlap", validated, err)
	}
	for _, want := range []string{`module "alpha"`, `module "beta"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidatePathBoundaries() error = %q, want %q", err, want)
		}
	}
}

func TestPartialScope_CannotBypassUnrequestedControlOverlap(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(home, "control-repository")
	controlPaths := writeGlobalControlFixture(t, home, repository)
	requestedRoot := filepath.Join(repository, "modules", "requested")
	unrequestedRoot := filepath.Join(repository, "modules", "unrequested")
	writeSourceFile(t, requestedRoot, "good", "good")
	writeSourceFile(t, unrequestedRoot, "bad", "bad")
	writeBoundaryTarget(t, filepath.Join(home, "safe", "good"))
	writeBoundaryTarget(t, filepath.Join(repository, "bad"))
	requested := ResolvedModule{Name: "requested", SourceDir: requestedRoot, TargetRoot: "~/safe"}
	unrequested := ResolvedModule{Name: "unrequested", SourceDir: unrequestedRoot, TargetRoot: "~/control-repository"}

	requestedOnly, err := testResolvedProfile(requested).ValidatePathBoundaries(controlPaths)
	if err != nil || len(requestedOnly.Entries()) != 1 {
		t.Fatalf("requested-only proof = (%#v, %v), want valid single module", requestedOnly, err)
	}
	validated, err := testResolvedProfile(requested, unrequested).ValidatePathBoundaries(controlPaths)
	if !errors.Is(err, paths.ErrTargetControlOverlap) || validated.Entries() != nil {
		t.Fatalf("full profile validation = (%#v, %v), want zero ErrTargetControlOverlap", validated, err)
	}
	for _, want := range []string{`module "unrequested"`, repository} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidatePathBoundaries() error = %q, want %q", err, want)
		}
	}
}

func TestFullProfileValidation_ValidatesProfilesSeparately(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repository")
	controlPaths := writeGlobalControlFixture(t, home, repository)
	alphaRoot := filepath.Join(repository, "modules", "alpha")
	betaRoot := filepath.Join(repository, "modules", "beta")
	writeSourceFile(t, alphaRoot, "config", "alpha")
	writeSourceFile(t, betaRoot, "config", "beta")
	writeBoundaryTarget(t, filepath.Join(home, "shared", "config"))
	alpha := ResolvedModule{Name: "alpha", SourceDir: alphaRoot, TargetRoot: "~/shared"}
	beta := ResolvedModule{Name: "beta", SourceDir: betaRoot, TargetRoot: "~/shared"}
	alphaProfile := ResolvedProfile{name: "alpha-profile", modules: []ResolvedModule{alpha}, goos: "darwin"}
	betaProfile := ResolvedProfile{name: "beta-profile", modules: []ResolvedModule{beta}, goos: "darwin"}

	for name, profile := range map[string]ResolvedProfile{
		"alpha": alphaProfile,
		"beta":  betaProfile,
	} {
		t.Run(name, func(t *testing.T) {
			validated, err := profile.ValidatePathBoundaries(controlPaths)
			if err != nil || len(validated.Entries()) != 1 {
				t.Fatalf("ValidatePathBoundaries() = (%#v, %v), want one valid profile entry", validated, err)
			}
		})
	}

	combined := ResolvedProfile{
		name:    "invalid-combined-proof",
		modules: []ResolvedModule{alpha, beta},
		goos:    "darwin",
	}
	if validated, err := combined.ValidatePathBoundaries(controlPaths); !errors.Is(err, paths.ErrTargetOverlap) || validated.Entries() != nil {
		t.Fatalf("combined proof = (%#v, %v), want zero ErrTargetOverlap", validated, err)
	}
}

func writeBoundaryTarget(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("existing target\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
