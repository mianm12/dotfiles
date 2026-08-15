package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	coreconfig "github.com/mianm12/dotfiles/internal/core/config"
)

func TestTrackedRepositoryConfiguration(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not return the test filename")
	}
	checkout := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	if !filepath.IsAbs(checkout) {
		t.Fatalf("tracked repository root %q is not absolute", checkout)
	}

	root := t.TempDir()
	for _, relative := range []string{
		"dot.toml",
		filepath.Join("modules", "starship", "module.toml"),
		filepath.Join("modules", "starship", "starship.toml"),
	} {
		copyTrackedRepositoryFile(t, checkout, root, relative)
	}

	repository, err := coreconfig.OpenRepository(root)
	if err != nil {
		t.Fatalf("OpenRepository(tracked root) error = %v", err)
	}
	moduleIDs := repository.ModuleIDs()
	if len(moduleIDs) != 1 || moduleIDs[0] != "starship" {
		t.Fatalf("tracked repository modules = %q, want [starship]", moduleIDs)
	}

	platforms := []struct {
		name     string
		platform coreconfig.Platform
	}{
		{
			name:     "macos-aarch64",
			platform: testPlatform("macos", "", "aarch64"),
		},
		{
			name:     "linux-ubuntu-x86_64",
			platform: testPlatform("linux", "ubuntu", "x86_64"),
		},
		{
			name:     "linux-arch-x86_64",
			platform: testPlatform("linux", "arch", "x86_64"),
		},
	}
	applicable := make(map[string]bool, len(moduleIDs))
	for _, platform := range platforms {
		t.Run(platform.name, func(t *testing.T) {
			for _, moduleID := range moduleIDs {
				module, exists, applicability, err := repository.InspectModule(
					moduleID,
					platform.platform,
				)
				if err != nil {
					t.Fatalf("InspectModule(%q) error = %v", moduleID, err)
				}
				if !exists {
					t.Fatalf("InspectModule(%q) reports a missing recognized module", moduleID)
				}
				switch applicability.State {
				case coreconfig.ApplicabilityApplicable:
					if module.ID != moduleID {
						t.Fatalf("InspectModule(%q).ID = %q", moduleID, module.ID)
					}
					applicable[moduleID] = true
				case coreconfig.ApplicabilityNotApplicable:
				// Platform-specific tracked modules may be intentionally skipped.
				case coreconfig.ApplicabilityIndeterminate:
					t.Fatalf(
						"InspectModule(%q) applicability is indeterminate: %s",
						moduleID,
						applicability.Diagnostic,
					)
				default:
					t.Fatalf(
						"InspectModule(%q) returned invalid applicability %q",
						moduleID,
						applicability.State,
					)
				}
			}
		})
	}
	for _, moduleID := range moduleIDs {
		if !applicable[moduleID] {
			t.Fatalf("tracked module %q is not applicable on any supported smoke platform", moduleID)
		}
	}
}

func copyTrackedRepositoryFile(t *testing.T, checkout, root, relative string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(checkout, relative))
	if err != nil {
		t.Fatalf("read tracked fixture %q: %v", relative, err)
	}
	target := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("create fixture parent for %q: %v", relative, err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatalf("write tracked fixture %q: %v", relative, err)
	}
}
