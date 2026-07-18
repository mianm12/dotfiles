package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/paths"
)

func TestGlobalPathValidation_ValidProfileIsReadOnlyAndUnrendered(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repository")
	controlPaths := writeGlobalControlFixture(t, home, repository)
	sourceRoot := filepath.Join(repository, "modules", "app")
	writeSourceFile(t, sourceRoot, "config.template", `{{ if }}`)
	profile := testResolvedProfile(ResolvedModule{
		Name:       "app",
		SourceDir:  sourceRoot,
		TargetRoot: "~/.config/app",
	})
	before := snapshotTree(t, root)

	validated, err := profile.ValidatePathBoundaries(controlPaths)
	if err != nil {
		t.Fatalf("ValidatePathBoundaries() error = %v", err)
	}
	entries := validated.Entries()
	if len(entries) != 1 || entries[0].Source != "config.template" || entries[0].Content != nil {
		t.Fatalf("Entries() = %#v, want one unrendered scaffold", entries)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("ValidatePathBoundaries() changed fixture: before=%v after=%v", before, after)
	}
	if _, err := os.Lstat(entries[0].TargetPath); !os.IsNotExist(err) {
		t.Fatalf("target path %q Lstat error = %v, want missing", entries[0].TargetPath, err)
	}

	entries[0].Source = "changed"
	entries[0].Content = []byte("changed")
	again := validated.Entries()
	if again[0].Source == entries[0].Source || again[0].Content != nil {
		t.Fatalf("mutating Entries() result changed validated profile: %#v", again)
	}
}

func TestGlobalPathValidation_SharedEntryContracts(t *testing.T) {
	requireProfileBoundaryEntry(t, ResolvedProfile.ValidatePathBoundaries)
	requireLabeledBoundaryEntry(t, paths.ValidatePathBoundaries)
}

func requireProfileBoundaryEntry(
	t *testing.T,
	entry func(ResolvedProfile, paths.ControlPlanePaths) (ValidatedProfile, error),
) {
	t.Helper()
	if entry == nil {
		t.Fatal("profile path boundary entry is unavailable")
	}
}

func requireLabeledBoundaryEntry(
	t *testing.T,
	entry func(paths.ControlPlanePaths, []paths.LabeledTarget) (paths.PathBoundaries, error),
) {
	t.Helper()
	if entry == nil {
		t.Fatal("labeled path boundary entry is unavailable")
	}
}

func TestGlobalPathValidation_EnumeratesStructureBeforeControlPlane(t *testing.T) {
	root := t.TempDir()
	controlPaths := writeGlobalControlFixture(
		t,
		filepath.Join(root, "home"),
		filepath.Join(root, "repository"),
	)
	profile := testResolvedProfile(ResolvedModule{
		Name:       "app",
		SourceDir:  "relative",
		TargetRoot: "~",
	})

	validated, err := profile.ValidatePathBoundaries(controlPaths)
	if err == nil || !strings.Contains(err.Error(), "source directory") {
		t.Fatalf("ValidatePathBoundaries() error = %v, want structure error", err)
	}
	if validated.Entries() != nil {
		t.Fatalf("ValidatePathBoundaries() result = %#v, want zero", validated)
	}
}

func TestGlobalPathValidation_RejectsZeroResolvedProfile(t *testing.T) {
	root := t.TempDir()
	controlPaths := writeGlobalControlFixture(
		t,
		filepath.Join(root, "home"),
		filepath.Join(root, "repository"),
	)

	validated, err := (ResolvedProfile{}).ValidatePathBoundaries(controlPaths)
	if err == nil || !strings.Contains(err.Error(), "profile name") {
		t.Fatalf("ValidatePathBoundaries() error = %v, want invalid resolved profile", err)
	}
	if validated.Entries() != nil {
		t.Fatalf("ValidatePathBoundaries() result = %#v, want zero", validated)
	}
}

func writeGlobalControlFixture(t *testing.T, home, repository string) paths.ControlPlanePaths {
	t.Helper()

	config := filepath.Join(filepath.Dir(repository), "machine-config.toml")
	controlPaths, err := paths.ResolveControlPlanePaths(home, repository, config)
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}
	for _, directory := range []string{
		controlPaths.Repository(),
		controlPaths.StateRoot(),
		controlPaths.BackupRoot(),
		filepath.Dir(controlPaths.InstalledBinary()),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", directory, err)
		}
	}
	for _, file := range []string{
		controlPaths.Config(),
		controlPaths.StateFile(),
		controlPaths.StateLock(),
		controlPaths.InstalledBinary(),
	} {
		if err := os.WriteFile(file, []byte("fixture\n"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", file, err)
		}
	}
	return controlPaths
}
