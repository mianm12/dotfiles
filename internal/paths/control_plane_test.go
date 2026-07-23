package paths

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveControlPlanePaths(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home", "..", "effective-home")
	repo := filepath.Join(root, "repo", "..", "effective-repo")
	config := filepath.Join(root, "config", "..", "machine.toml")

	paths, err := ResolveControlPlanePaths(home, repo, config)
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}
	cleanHome := filepath.Clean(home)
	want := []string{
		cleanHome,
		filepath.Clean(repo),
		filepath.Clean(config),
		filepath.Join(cleanHome, ".local", "state", "dot"),
		filepath.Join(cleanHome, ".local", "state", "dot", "state.json"),
		filepath.Join(cleanHome, ".local", "state", "dot", "lock"),
		filepath.Join(cleanHome, ".local", "bin", "dot"),
	}
	got := []string{
		paths.EffectiveHome(),
		paths.Repository(),
		paths.Config(),
		paths.StateRoot(),
		paths.StateFile(),
		paths.StateLock(),
		paths.InstalledBinary(),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("control paths = %#v, want %#v", got, want)
	}
}

func TestResolveControlPlanePaths_FilesystemRootHome(t *testing.T) {
	root := string(filepath.Separator)
	paths, err := ResolveControlPlanePaths(root, "/repo", "/config.toml")
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}
	if paths.StateRoot() != "/.local/state/dot" {
		t.Errorf("StateRoot() = %q, want %q", paths.StateRoot(), "/.local/state/dot")
	}
	if paths.InstalledBinary() != "/.local/bin/dot" {
		t.Errorf("InstalledBinary() = %q, want %q", paths.InstalledBinary(), "/.local/bin/dot")
	}
}

func TestResolveControlPlanePaths_RejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name    string
		home    string
		repo    string
		config  string
		wantErr string
	}{
		{name: "empty home", repo: "/repo", config: "/config", wantErr: "effective HOME"},
		{name: "relative home", home: "home", repo: "/repo", config: "/config", wantErr: "effective HOME"},
		{name: "empty repo", home: "/home", config: "/config", wantErr: "repository"},
		{name: "relative repo", home: "/home", repo: "repo", config: "/config", wantErr: "repository"},
		{name: "empty config", home: "/home", repo: "/repo", wantErr: "machine config"},
		{name: "relative config", home: "/home", repo: "/repo", config: "config", wantErr: "machine config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveControlPlanePaths(tt.home, tt.repo, tt.config)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ResolveControlPlanePaths() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolveControlPlanePaths_StateFamilyIsSinglePlannedHierarchy(t *testing.T) {
	paths, err := ResolveControlPlanePaths("/home", "/repo", "/config")
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}
	if len(paths.members) != int(controlMemberCount) {
		t.Fatalf("member count = %d, want %d", len(paths.members), controlMemberCount)
	}

	for role, member := range paths.members {
		if member.role != controlMemberRole(role) {
			t.Errorf("member %d role = %d", role, member.role)
		}
		if role < int(controlMemberStateRoot) || role > int(controlMemberStateLock) {
			if member.family == controlFamilyState || member.hasParent {
				t.Errorf("non-state member %d has state family or planned parent: %#v", role, member)
			}
			continue
		}
		if member.family != controlFamilyState {
			t.Errorf("state member %d family = %d, want state", role, member.family)
		}
		if member.role == controlMemberStateRoot {
			if member.hasParent {
				t.Error("state root has a planned parent")
			}
			continue
		}
		if !member.hasParent || member.parent != controlMemberStateRoot {
			t.Errorf("state child %d planned parent = (%d, %t), want state root", role, member.parent, member.hasParent)
		}
	}
}

func TestResolveControlPlanePaths_IsReadOnlyAndCWDIndependent(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "missing-home")
	repo := filepath.Join(root, "missing-repo")
	config := filepath.Join(root, "missing-config")
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("os.ReadDir(%q) error = %v", root, err)
	}

	t.Chdir(t.TempDir())
	paths, err := ResolveControlPlanePaths(home, repo, config)
	if err != nil {
		t.Fatalf("ResolveControlPlanePaths() error = %v", err)
	}
	if paths.Repository() != repo || paths.Config() != config {
		t.Errorf("control paths depend on cwd: repo=%q config=%q", paths.Repository(), paths.Config())
	}
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("os.ReadDir(%q) after resolution error = %v", root, err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("control path resolution changed filesystem: before=%v after=%v", before, after)
	}
}
