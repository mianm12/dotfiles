package manifest

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoad_ExpandsProfilesAndFindsUnassignedModules(t *testing.T) {
	repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
base = ["zsh", "git"]
dev = ["@base", "nvim", "zsh"]
mac = ["@dev", "karabiner"]
empty = []
`)
	for _, name := range []string{"tmux", "zsh", "karabiner", "git", "nvim"} {
		writeModule(t, repo, name, "")
	}

	got, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	wantProfiles := []string{"base", "dev", "empty", "mac"}
	if !reflect.DeepEqual(got.ProfileNames(), wantProfiles) {
		t.Errorf("ProfileNames() = %v, want %v", got.ProfileNames(), wantProfiles)
	}
	wantMac := []string{"git", "karabiner", "nvim", "zsh"}
	if !reflect.DeepEqual(got.profiles["mac"], wantMac) {
		t.Errorf("expanded mac = %v, want %v", got.profiles["mac"], wantMac)
	}
	wantUnassigned := []string{"tmux"}
	if !reflect.DeepEqual(got.UnassignedModules(), wantUnassigned) {
		t.Errorf("UnassignedModules() = %v, want %v", got.UnassignedModules(), wantUnassigned)
	}
}

func TestLoad_ExpandsDiamondProfileOnce(t *testing.T) {
	repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
base = ["git"]
left = ["@base", "zsh"]
right = ["@base", "nvim"]
top = ["@left", "@right"]
`)
	for _, name := range []string{"zsh", "git", "nvim"} {
		writeModule(t, repo, name, "")
	}

	got, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	want := []string{"git", "nvim", "zsh"}
	if !reflect.DeepEqual(got.profiles["top"], want) {
		t.Errorf("expanded top = %v, want diamond de-duplicated %v", got.profiles["top"], want)
	}
}

func TestLoad_RejectsInvalidProfiles(t *testing.T) {
	tests := []struct {
		name     string
		profiles string
		modules  []string
		want     string
	}{
		{name: "invalid profile name", profiles: `_bad = []`, want: "invalid profile name"},
		{name: "invalid module name", profiles: `base = ["bad/name"]`, want: "invalid module name"},
		{name: "invalid profile reference", profiles: `base = ["@bad/name"]`, want: "invalid profile reference"},
		{name: "self cycle", profiles: `base = ["@base"]`, want: "base -> base"},
		{name: "multiple profile cycle", profiles: "a = [\"@b\"]\nb = [\"@c\"]\nc = [\"@a\"]", want: "a -> b -> c -> a"},
		{name: "missing profile", profiles: `base = ["@missing"]`, want: "unknown profile"},
		{name: "missing module", profiles: `base = ["zsh"]`, want: "missing module"},
		{name: "case mismatch", profiles: `base = ["Zsh"]`, modules: []string{"zsh"}, want: "does not exactly match"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\n"+tt.profiles)
			for _, module := range tt.modules {
				writeModule(t, repo, module, "")
			}
			_, err := Load(repo)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestLoad_ProfileResultsAreStable(t *testing.T) {
	contents := []string{
		"requires = \">=0.3.0\"\n[profiles]\nwork = [\"@base\", \"nvim\"]\nbase = [\"zsh\", \"git\"]",
		"requires = \">=0.3.0\"\n[profiles]\nbase = [\"zsh\", \"git\"]\nwork = [\"@base\", \"nvim\"]",
	}
	orders := [][]string{{"nvim", "git", "unused", "zsh"}, {"zsh", "unused", "git", "nvim"}}

	var first Repository
	for index := range contents {
		repo := writeRepositoryManifest(t, contents[index])
		for _, module := range orders[index] {
			writeModule(t, repo, module, "")
		}
		loaded, err := Load(repo)
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if index == 0 {
			first = loaded
			continue
		}
		if !reflect.DeepEqual(loaded.ProfileNames(), first.ProfileNames()) {
			t.Errorf("ProfileNames() = %v, want %v", loaded.ProfileNames(), first.ProfileNames())
		}
		if !reflect.DeepEqual(loaded.profiles, first.profiles) {
			t.Errorf("profiles = %v, want %v", loaded.profiles, first.profiles)
		}
		if !reflect.DeepEqual(loaded.UnassignedModules(), first.UnassignedModules()) {
			t.Errorf("UnassignedModules() = %v, want %v", loaded.UnassignedModules(), first.UnassignedModules())
		}
	}
}
