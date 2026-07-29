package config_test

import (
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
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	if !filepath.IsAbs(root) {
		t.Fatalf("tracked repository root %q is not absolute", root)
	}

	repository, err := coreconfig.OpenRepository(root)
	if err != nil {
		t.Fatalf("OpenRepository(tracked root) error = %v", err)
	}
	moduleIDs := repository.ModuleIDs()
	if len(moduleIDs) == 0 {
		t.Fatal("tracked repository contains no recognized modules")
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
