package manifest

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolve_MissingActiveModuleTargetReturnsClassifiedError(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			otherGOOS := "linux"
			if goos == "linux" {
				otherGOOS = "darwin"
			}
			repo := writeRepositoryManifest(t, "[profiles]\nbase = [\"app\"]")
			writeModule(t, repo, "app", "[target]\n"+otherGOOS+" = \"~\"")
			loaded, err := Load(repo)
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}

			_, err = loaded.Resolve("base", goos)
			var targetErr *ModuleTargetMappingError
			if !errors.As(err, &targetErr) {
				t.Fatalf("Resolve() error = %v, want ModuleTargetMappingError", err)
			}
			if targetErr.Module() != "app" || targetErr.GOOS() != goos ||
				!strings.Contains(err.Error(), "target table has no "+goos+" entry") {
				t.Fatalf("ModuleTargetMappingError = module %q, GOOS %q, error %v", targetErr.Module(), targetErr.GOOS(), err)
			}
		})
	}
}

func TestResolve_AppliesBuiltInDefaults(t *testing.T) {
	repo := writeRepositoryManifest(t, "[profiles]\nbase = [\"zsh\"]")
	writeModule(t, repo, "zsh", "")
	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	for _, goos := range []string{"darwin", "linux"} {
		got, err := loaded.Resolve("base", goos)
		if err != nil {
			t.Fatalf("Resolve(%q) error = %v, want nil", goos, err)
		}
		if len(got.modules) != 1 {
			t.Fatalf("Resolve(%q) modules = %v, want one module", goos, got.modules)
		}
		module := got.modules[0]
		if module.Name != "zsh" || module.TargetRoot != "~" || len(module.Ignore) != 0 {
			t.Errorf("Resolve(%q) module = %#v, want zsh targeting ~ with no ignore", goos, module)
		}
		if module.SourceDir != filepath.Join(repo, "modules", "zsh") {
			t.Errorf("Resolve(%q).SourceDir = %q, want module directory", goos, module.SourceDir)
		}
	}
}

func TestResolve_MergeMatrix(t *testing.T) {
	tests := []struct {
		name           string
		defaults       string
		globalIgnore   string
		moduleManifest string
		goos           string
		wantModules    int
		wantTarget     string
		wantIgnore     []string
		wantError      string
	}{
		{
			name:         "inherits defaults",
			defaults:     "os = [\"darwin\"]\ntarget = \"~/global\"",
			globalIgnore: "[\"a\"]",
			goos:         "darwin",
			wantModules:  1,
			wantTarget:   "~/global",
			wantIgnore:   []string{"a"},
		},
		{
			name:        "defaults os filters module",
			defaults:    "os = [\"darwin\"]\ntarget = \"~/global\"",
			goos:        "linux",
			wantModules: 0,
		},
		{
			name:           "module os and target replace defaults",
			defaults:       "os = [\"darwin\"]\ntarget = \"~/global\"",
			moduleManifest: "os = [\"linux\"]\ntarget = \"~/module\"",
			goos:           "linux",
			wantModules:    1,
			wantTarget:     "~/module",
		},
		{
			name:           "empty module os disables module",
			defaults:       "os = [\"darwin\", \"linux\"]",
			moduleManifest: "os = []",
			goos:           "darwin",
			wantModules:    0,
		},
		{
			name:        "empty defaults os disables module",
			defaults:    "os = []",
			goos:        "linux",
			wantModules: 0,
		},
		{
			name:        "inherits defaults target table",
			defaults:    "target = { darwin = \"~/default-darwin\" }",
			goos:        "darwin",
			wantModules: 1,
			wantTarget:  "~/default-darwin",
		},
		{
			name:      "active module requires defaults target os",
			defaults:  "target = { darwin = \"~/default-darwin\" }",
			goos:      "linux",
			wantError: "target table has no linux entry",
		},
		{
			name:           "module table replaces common target",
			defaults:       "target = \"~/global\"",
			moduleManifest: "[target]\ndarwin = \"~/darwin\"",
			goos:           "darwin",
			wantModules:    1,
			wantTarget:     "~/darwin",
		},
		{
			name:           "inactive module may omit target os",
			moduleManifest: "os = [\"darwin\"]\n[target]\ndarwin = \"~/darwin\"",
			goos:           "linux",
			wantModules:    0,
		},
		{
			name:           "active module requires target os",
			moduleManifest: "[target]\ndarwin = \"~/darwin\"",
			goos:           "linux",
			wantError:      "target table has no linux entry",
		},
		{
			name:           "module-only ignore",
			moduleManifest: "[ignore]\npatterns = [\"module\"]",
			goos:           "darwin",
			wantModules:    1,
			wantTarget:     "~",
			wantIgnore:     []string{"module"},
		},
		{
			name:           "ignore is stable union",
			defaults:       "target = \"~\"",
			globalIgnore:   "[\"a\", \"b\", \"a\"]",
			moduleManifest: "[ignore]\npatterns = [\"b\", \"c\"]",
			goos:           "darwin",
			wantModules:    1,
			wantTarget:     "~",
			wantIgnore:     []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := ""
			if tt.defaults != "" {
				root += "[defaults]\n" + tt.defaults + "\n"
			}
			if tt.globalIgnore != "" {
				root += "[ignore]\npatterns = " + tt.globalIgnore + "\n"
			}
			root += "[profiles]\nbase = [\"app\"]"
			repo := writeRepositoryManifest(t, root)
			writeModule(t, repo, "app", tt.moduleManifest)
			loaded, err := Load(repo)
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}

			got, err := loaded.Resolve("base", tt.goos)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("Resolve() error = %v, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve() error = %v, want nil", err)
			}
			if len(got.modules) != tt.wantModules {
				t.Fatalf("Resolve() modules = %v, want length %d", got.modules, tt.wantModules)
			}
			if tt.wantModules == 0 {
				return
			}
			if got.modules[0].TargetRoot != tt.wantTarget {
				t.Errorf("TargetRoot = %q, want %q", got.modules[0].TargetRoot, tt.wantTarget)
			}
			if tt.wantIgnore != nil && !reflect.DeepEqual(got.modules[0].Ignore, tt.wantIgnore) {
				t.Errorf("Ignore = %v, want %v", got.modules[0].Ignore, tt.wantIgnore)
			}
		})
	}
}

func TestResolve_ReturnsStableFileRulesAndHooks(t *testing.T) {
	repo := writeRepositoryManifest(t, `
[profiles]
base = ["app"]
`)
	writeModule(t, repo, "app", `
[ignore]
patterns = ["*.bak"]
[files."z.template"]
kind = "scaffold"
[files.a]
kind = "link"
target = "~/.config/app/a"
[files."b.template"]
kind = "scaffold"
mode = "0600"
[hooks]
run_once = ["hooks/z", "hooks/a"]
`)
	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	first, err := loaded.Resolve("base", "darwin")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	second, err := loaded.Resolve("base", "darwin")
	if err != nil {
		t.Fatalf("Resolve() second error = %v, want nil", err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("repeated Resolve() = %#v, want %#v", second, first)
	}
	wantFileRules := []ResolvedFileRule{
		{Source: "a", Kind: FileKindLink, TargetOverride: "~/.config/app/a"},
		{Source: "b.template", Kind: FileKindScaffold, Mode: "0600"},
		{Source: "z.template", Kind: FileKindScaffold, Mode: "0644"},
	}
	if !reflect.DeepEqual(first.modules[0].FileRules, wantFileRules) {
		t.Errorf("FileRules = %#v, want %#v", first.modules[0].FileRules, wantFileRules)
	}
	if !reflect.DeepEqual(first.modules[0].RunOnce, []string{"hooks/z", "hooks/a"}) {
		t.Errorf("RunOnce = %v, want declaration order", first.modules[0].RunOnce)
	}
	if !reflect.DeepEqual(first.modules[0].Ignore, []string{"*.bak"}) {
		t.Errorf("Ignore = %v, want [*.bak]", first.modules[0].Ignore)
	}
	if first.goos != "darwin" {
		t.Errorf("resolved GOOS = %q, want darwin", first.goos)
	}

	first.modules[0].FileRules[0].Source = "changed"
	first.modules[0].Ignore[0] = "changed"
	first.modules[0].RunOnce[0] = "changed"
	third, err := loaded.Resolve("base", "darwin")
	if err != nil {
		t.Fatalf("Resolve() third error = %v, want nil", err)
	}
	if !reflect.DeepEqual(third, second) {
		t.Fatalf("mutating resolved result changed repository: got %#v, want %#v", third, second)
	}
}

func TestResolve_ValidatesFileTargetWithinEffectiveRoot(t *testing.T) {
	tests := []struct {
		name         string
		defaultsRoot string
		moduleRoot   string
		fileTarget   string
		wantError    bool
	}{
		{name: "home root", defaultsRoot: "~", fileTarget: "~/.config/app"},
		{name: "inherited root", defaultsRoot: "~/.config", fileTarget: "~/.config/app"},
		{name: "module root override", defaultsRoot: "~/.config", moduleRoot: "~/Library/App", fileTarget: "~/Library/App/settings"},
		{name: "outside inherited root", defaultsRoot: "~/.config", fileTarget: "~/Library/App/settings", wantError: true},
		{name: "same as root", defaultsRoot: "~/.config", fileTarget: "~/.config", wantError: true},
		{name: "shared prefix is not descendant", defaultsRoot: "~/.config", fileTarget: "~/.configuration/app", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := "[defaults]\ntarget = \"" + tt.defaultsRoot + "\"\n[profiles]\nbase = [\"app\"]"
			repo := writeRepositoryManifest(t, root)
			module := ""
			if tt.moduleRoot != "" {
				module += "target = \"" + tt.moduleRoot + "\"\n"
			}
			module += "[files.settings]\ntarget = \"" + tt.fileTarget + "\""
			writeModule(t, repo, "app", module)
			loaded, err := Load(repo)
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}

			resolved, err := loaded.Resolve("base", "darwin")
			if tt.wantError {
				if err == nil || !strings.Contains(err.Error(), "true descendant of target root") {
					t.Fatalf("Resolve() error = %v, want target root boundary error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve() error = %v, want nil", err)
			}
			if got := resolved.modules[0].FileRules[0].TargetOverride; got != tt.fileTarget {
				t.Errorf("TargetOverride = %q, want %q", got, tt.fileTarget)
			}
		})
	}
}

func TestResolve_RejectsUnknownProfileAndGOOS(t *testing.T) {
	repo := writeRepositoryManifest(t, "[profiles]\nbase = []")
	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if _, err := loaded.Resolve("missing", "darwin"); err == nil || !strings.Contains(err.Error(), "unknown profile") {
		t.Fatalf("Resolve(missing) error = %v, want unknown profile", err)
	}
	if _, err := loaded.Resolve("base", "freebsd"); err == nil || !strings.Contains(err.Error(), "unsupported GOOS") {
		t.Fatalf("Resolve(freebsd) error = %v, want unsupported GOOS", err)
	}
}

func TestResolve_DoesNotWriteOnSuccessOrFailure(t *testing.T) {
	repo := writeRepositoryManifest(t, `
[profiles]
base = ["app"]
`)
	writeModule(t, repo, "app", "[target]\ndarwin = \"~\"")
	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	before := snapshotTree(t, repo)

	if _, err := loaded.Resolve("base", "darwin"); err != nil {
		t.Fatalf("Resolve(darwin) error = %v, want nil", err)
	}
	if _, err := loaded.Resolve("base", "linux"); err == nil {
		t.Fatal("Resolve(linux) error = nil, want missing target error")
	}
	after := snapshotTree(t, repo)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("Resolve() changed repository\nbefore: %v\nafter:  %v", before, after)
	}
}
