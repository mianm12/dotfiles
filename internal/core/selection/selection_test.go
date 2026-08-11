package selection

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
)

func TestResolveDistinguishesProfileAndDirectNotApplicability(t *testing.T) {
	root := t.TempDir()
	writeSelectionFile(t, filepath.Join(root, "dot.toml"), `
version = 1
[profiles]
base = ["gated"]
`)
	writeSelectionFile(t, filepath.Join(root, "modules", "gated", "module.toml"), `
[match]
os = ["macos"]
`)
	repository, err := config.OpenRepository(root)
	if err != nil {
		t.Fatalf("OpenRepository() error = %v", err)
	}
	platform := config.Platform{
		OS:     config.KnownPlatformField("linux"),
		Distro: config.KnownPlatformField("ubuntu"),
		Arch:   config.KnownPlatformField("x86_64"),
	}

	profile, err := Resolve(repository, config.Machine{
		Version:    1,
		Repository: root,
		Profiles:   []string{"base"},
	}, platform)
	if err != nil || len(profile.Issues) != 0 || len(profile.Modules) != 0 {
		t.Fatalf("Resolve(profile not-applicable) = (%#v, %v)", profile, err)
	}
	if observation := profile.Observations["gated"]; !observation.Loaded ||
		observation.Applicability.State != config.ApplicabilityNotApplicable {
		t.Fatalf("profile observation = %#v", observation)
	}

	direct, err := Resolve(repository, config.Machine{
		Version:      1,
		Repository:   root,
		ExtraModules: []string{"gated"},
	}, platform)
	if err != nil || len(direct.Issues) != 1 || direct.Issues[0].ModuleID != "gated" {
		t.Fatalf("Resolve(direct not-applicable) = (%#v, %v)", direct, err)
	}

}

func TestResolveReportsMissingDirectModule(t *testing.T) {
	root := t.TempDir()
	writeSelectionFile(t, filepath.Join(root, "dot.toml"), "version = 1\n[profiles]\n")
	repository, err := config.OpenRepository(root)
	if err != nil {
		t.Fatalf("OpenRepository() error = %v", err)
	}
	result, err := Resolve(repository, config.Machine{
		Version:      1,
		Repository:   root,
		ExtraModules: []string{"gone"},
	}, config.Platform{})
	if err != nil || len(result.Issues) != 1 || result.Issues[0].ModuleID != "gone" {
		t.Fatalf("Resolve(missing extra) = (%#v, %v)", result, err)
	}
}

func writeSelectionFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
