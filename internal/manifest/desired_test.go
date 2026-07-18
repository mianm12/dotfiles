package manifest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/paths"
)

func TestResolvedProfileEnumerate_AppliesPriorityAndReturnsStableDesired(t *testing.T) {
	repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
base = ["zsh", "app"]
`)
	writeModule(t, repo, "app", `
target = "~/.config/app"
[ignore]
patterns = ["README.md", "ignored*"]
[files."README.md"]
kind = "scaffold"
mode = "0600"
target = "~/.config/app/readme"
[files."literal.template"]
kind = "link"
[files.seed]
kind = "scaffold"
mode = "0700"
`)
	appRoot := filepath.Join(repo, "modules", "app")
	writeSourceFile(t, appRoot, "z", "link")
	writeSourceFile(t, appRoot, "README.md", "explicit beats ignore")
	writeSourceFile(t, appRoot, "ignored.txt", "ignored")
	writeSourceFile(t, appRoot, "literal.template", `{{ env "HOME" }}`)
	writeSourceFile(t, appRoot, "scaffold.template", "scaffold")
	writeSourceFile(t, appRoot, "seed", "scaffold without suffix")

	writeModule(t, repo, "zsh", "")
	zshRoot := filepath.Join(repo, "modules", "zsh")
	writeSourceFile(t, zshRoot, ".zshrc", "link")

	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	resolved, err := loaded.Resolve("base", "darwin")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	home := filepath.Join(t.TempDir(), "home-does-not-exist")
	context := testRuntimeContext(home)
	before := snapshotTree(t, repo)

	first, err := resolved.Enumerate(context)
	if err != nil {
		t.Fatalf("Enumerate() error = %v, want nil", err)
	}
	second, err := resolved.Enumerate(context)
	if err != nil {
		t.Fatalf("Enumerate() second error = %v, want nil", err)
	}
	want := []DesiredEntry{
		{
			Module:     "app",
			Source:     "README.md",
			SourcePath: filepath.Join(appRoot, "README.md"),
			Target:     "~/.config/app/readme",
			TargetPath: filepath.Join(home, ".config", "app", "readme"),
			Kind:       FileKindScaffold,
			Mode:       0o600,
			Content:    []byte("explicit beats ignore"),
		},
		{
			Module:     "app",
			Source:     "literal.template",
			SourcePath: filepath.Join(appRoot, "literal.template"),
			Target:     "~/.config/app/literal",
			TargetPath: filepath.Join(home, ".config", "app", "literal"),
			Kind:       FileKindLink,
		},
		{
			Module:     "app",
			Source:     "scaffold.template",
			SourcePath: filepath.Join(appRoot, "scaffold.template"),
			Target:     "~/.config/app/scaffold",
			TargetPath: filepath.Join(home, ".config", "app", "scaffold"),
			Kind:       FileKindScaffold,
			Mode:       0o644,
			Content:    []byte("scaffold"),
		},
		{
			Module:     "app",
			Source:     "seed",
			SourcePath: filepath.Join(appRoot, "seed"),
			Target:     "~/.config/app/seed",
			TargetPath: filepath.Join(home, ".config", "app", "seed"),
			Kind:       FileKindScaffold,
			Mode:       0o700,
			Content:    []byte("scaffold without suffix"),
		},
		{
			Module:     "app",
			Source:     "z",
			SourcePath: filepath.Join(appRoot, "z"),
			Target:     "~/.config/app/z",
			TargetPath: filepath.Join(home, ".config", "app", "z"),
			Kind:       FileKindLink,
		},
		{
			Module:     "zsh",
			Source:     ".zshrc",
			SourcePath: filepath.Join(zshRoot, ".zshrc"),
			Target:     "~/.zshrc",
			TargetPath: filepath.Join(home, ".zshrc"),
			Kind:       FileKindLink,
		},
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("Enumerate() = %#v, want %#v", first, want)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("repeated Enumerate() = %#v, want %#v", second, first)
	}
	if _, err := os.Lstat(home); !os.IsNotExist(err) {
		t.Fatalf("Enumerate() target home Lstat error = %v, want not exist", err)
	}
	if after := snapshotTree(t, repo); !reflect.DeepEqual(after, before) {
		t.Fatalf("Enumerate() changed repository\nbefore: %v\nafter:  %v", before, after)
	}

	first[0].Source = "changed"
	first[0].Content[0] = 'X'
	third, err := resolved.Enumerate(context)
	if err != nil {
		t.Fatalf("Enumerate() third error = %v, want nil", err)
	}
	if !reflect.DeepEqual(third, second) {
		t.Fatalf("mutating result changed profile: got %#v, want %#v", third, second)
	}
}

func TestRepositoryValidateTemplates_DoesNotRequireRuntimeData(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{name: "declared variable", source: `{{ .email }}`},
		{name: "syntax", source: `{{ if }}`, wantErr: "parse template"},
		{name: "function", source: `{{ len .email }}`, wantErr: `function "len" is not allowed`},
		{name: "undeclared variable", source: `{{ .token }}`, wantErr: "not declared by manifest data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
base = ["app"]
[data.email]
`)
			writeModule(t, repo, "app", "")
			writeSourceFile(t, filepath.Join(repo, "modules", "app"), "config.template", tt.source)
			loaded, err := Load(repo)
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			err = loaded.ValidateTemplates()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateTemplates() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateTemplates() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRepositoryValidateTemplates_CoversModuleLocalTemplateCandidates(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		setup   func(t *testing.T, repo string)
		wantErr string
	}{
		{
			name:    "unassigned module",
			profile: `base = ["active"]`,
			setup: func(t *testing.T, repo string) {
				writeModule(t, repo, "active", "")
				writeSourceFile(t, filepath.Join(repo, "modules", "active"), "config", "literal")
				writeModule(t, repo, "unassigned", "")
				writeSourceFile(t, filepath.Join(repo, "modules", "unassigned"), "config.template", `{{ .token }}`)
			},
			wantErr: `module "unassigned" scaffold source "config.template"`,
		},
		{
			name:    "module inactive on darwin",
			profile: `base = ["app"]`,
			setup: func(t *testing.T, repo string) {
				writeModule(t, repo, "app", `os = ["linux"]`)
				writeSourceFile(t, filepath.Join(repo, "modules", "app"), "config.template", `{{ .token }}`)
			},
			wantErr: `user variable ".token" is not declared`,
		},
		{
			name:    "suffixless explicit scaffold",
			profile: `base = ["app"]`,
			setup: func(t *testing.T, repo string) {
				writeModule(t, repo, "app", "[files.config]\nkind = \"scaffold\"")
				writeSourceFile(t, filepath.Join(repo, "modules", "app"), "config", `{{ .token }}`)
			},
			wantErr: `user variable ".token" is not declared`,
		},
		{
			name:    "template suffix explicitly linked",
			profile: `base = ["app"]`,
			setup: func(t *testing.T, repo string) {
				writeModule(t, repo, "app", "[files.\"config.template\"]\nkind = \"link\"")
				writeSourceFile(t, filepath.Join(repo, "modules", "app"), "config.template", `{{ .token }}`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\n"+tt.profile+"\n[data.email]\n")
			tt.setup(t, repo)
			loaded, err := Load(repo)
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}

			err = loaded.ValidateTemplates()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateTemplates() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateTemplates() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolvedProfileEnumerate_ValidatesStructuralInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(module *ResolvedModule)
		want   string
	}{
		{
			name: "relative source root",
			mutate: func(module *ResolvedModule) {
				module.SourceDir = "relative"
			},
			want: "source directory must be a non-empty absolute path",
		},
		{
			name: "invalid target root",
			mutate: func(module *ResolvedModule) {
				module.TargetRoot = "relative"
			},
			want: "target root",
		},
		{
			name: "invalid mode",
			mutate: func(module *ResolvedModule) {
				module.FileRules = []ResolvedFileRule{{Source: "config", Kind: FileKindScaffold, Mode: "644"}}
			},
			want: "invalid mode",
		},
		{
			name: "mode on link",
			mutate: func(module *ResolvedModule) {
				module.FileRules = []ResolvedFileRule{{Source: "config", Kind: FileKindLink, Mode: "0644"}}
			},
			want: "must not declare mode",
		},
		{
			name: "target outside root",
			mutate: func(module *ResolvedModule) {
				module.TargetRoot = "~/.config/app"
				module.FileRules = []ResolvedFileRule{{
					Source:         "config",
					Kind:           FileKindLink,
					TargetOverride: "~/.config/other",
				}}
			},
			want: "true descendant of target root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeSourceFile(t, root, "config", "value")
			module := ResolvedModule{Name: "app", SourceDir: root, TargetRoot: "~"}
			tt.mutate(&module)
			_, err := testResolvedProfile(module).Enumerate(testRuntimeContext(t.TempDir()))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Enumerate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestResolvedProfileEnumerate_RendersScaffoldContentAndMode(t *testing.T) {
	repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
base = ["app"]
[data.email]
`)
	writeModule(t, repo, "app", `
[files."config.template"]
mode = "0600"
`)
	root := filepath.Join(repo, "modules", "app")
	writeSourceFile(t, root, "config.template", "{{ .OS }}/{{ .Arch }}/{{ .Hostname }}/{{ .Profile }}/{{ .Home }}/{{ .email }}")
	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	profile, err := loaded.Resolve("base", "darwin")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	homeRoot := t.TempDir()
	context := testRuntimeContext(filepath.Join(homeRoot, "parent", "..", "home"))
	context.Data = map[string]string{"email": "me@example.com", "stale": "ignored"}

	entries, err := profile.Enumerate(context)
	if err != nil {
		t.Fatalf("Enumerate() error = %v, want nil", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Enumerate() entries = %#v, want one", entries)
	}
	wantContent := "darwin/arm64/test-host/base/" + filepath.Join(homeRoot, "home") + "/me@example.com"
	if string(entries[0].Content) != wantContent || entries[0].Mode != 0o600 {
		t.Fatalf(
			"Enumerate() entry = %#v, want content %q and mode 0600",
			entries[0],
			wantContent,
		)
	}
}

func TestResolvedProfileEnumerate_RejectsEmptySuffixResult(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, ".template", "value")
	profile := testResolvedProfile(ResolvedModule{Name: "app", SourceDir: root, TargetRoot: "~"})

	_, err := profile.Enumerate(testRuntimeContext(t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "empty target basename") {
		t.Fatalf("Enumerate() error = %v, want empty target basename error", err)
	}
}

func TestResolvedProfileEnumerate_ExplicitTargetSkipsSuffixDerivation(t *testing.T) {
	tests := []struct {
		name   string
		source string
		kind   FileKind
		mode   string
	}{
		{name: "link template", source: ".template", kind: FileKindLink},
		{name: "scaffold managed suffix", source: ".tmpl", kind: FileKindScaffold, mode: "0600"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeSourceFile(t, root, tt.source, "value")
			profile := testResolvedProfile(ResolvedModule{
				Name:       "app",
				SourceDir:  root,
				TargetRoot: "~",
				FileRules: []ResolvedFileRule{{
					Source:         tt.source,
					Kind:           tt.kind,
					Mode:           tt.mode,
					TargetOverride: "~/.config",
				}},
			})

			entries, err := profile.Enumerate(testRuntimeContext(t.TempDir()))
			if err != nil {
				t.Fatalf("Enumerate() error = %v, want nil", err)
			}
			if len(entries) != 1 || entries[0].Target != "~/.config" {
				t.Fatalf("Enumerate() entries = %#v, want one entry targeting ~/.config", entries)
			}
		})
	}
}

func TestResolvedProfileEnumerate_RejectsInvalidRuntimeContext(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RuntimeContext)
		want   string
	}{
		{name: "empty home", mutate: func(context *RuntimeContext) { context.Home = "" }, want: "effective HOME"},
		{name: "relative home", mutate: func(context *RuntimeContext) { context.Home = "relative" }, want: "effective HOME"},
		{name: "wrong os", mutate: func(context *RuntimeContext) { context.OS = "linux" }, want: "does not match resolved profile OS"},
		{name: "wrong profile", mutate: func(context *RuntimeContext) { context.Profile = "other" }, want: "does not match resolved profile"},
		{name: "unsupported arch", mutate: func(context *RuntimeContext) { context.Arch = "386" }, want: "architecture"},
	}

	profile := testResolvedProfile()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			context := testRuntimeContext(t.TempDir())
			tt.mutate(&context)
			if _, err := profile.Enumerate(context); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Enumerate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestResolvedProfileValidateTargetStructure_DoesNotRenderScaffolds(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "config.template", `{{ if }}`)
	profile := testResolvedProfile(ResolvedModule{Name: "app", SourceDir: root, TargetRoot: "~"})
	homeRoot := t.TempDir()
	home := filepath.Join(homeRoot, "home")
	target := filepath.Join(home, "config")
	writeBoundaryTarget(t, target)
	beforeSource := snapshotTree(t, root)
	beforeTarget := snapshotTree(t, homeRoot)

	entries, err := profile.validateTargetStructure(home)
	if err != nil {
		t.Fatalf("validateTargetStructure() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Source != "config.template" || entries[0].Content != nil {
		t.Fatalf("validateTargetStructure() entries = %#v, want unrendered scaffold", entries)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, beforeSource) {
		t.Fatalf("validateTargetStructure() changed source tree: before=%v after=%v", beforeSource, after)
	}
	if after := snapshotTree(t, homeRoot); !reflect.DeepEqual(after, beforeTarget) {
		t.Fatalf("validateTargetStructure() changed target tree: before=%v after=%v", beforeTarget, after)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "existing target\n" {
		t.Fatalf("target %q content = %q, error = %v", target, content, err)
	}

	_, err = profile.Enumerate(testRuntimeContext(home))
	if err == nil || !strings.Contains(err.Error(), "parse template") {
		t.Fatalf("Enumerate() error = %v, want proof scaffold would fail rendering path", err)
	}
}

func TestResolvedProfileValidateTargetStructure_RejectsDerivedAndExplicitCollisions(t *testing.T) {
	tests := []struct {
		name    string
		modules func(t *testing.T) []ResolvedModule
		wants   []string
		path    string
	}{
		{
			name: "cross-module target",
			modules: func(t *testing.T) []ResolvedModule {
				alpha := t.TempDir()
				beta := t.TempDir()
				writeSourceFile(t, alpha, "config", "alpha")
				writeSourceFile(t, beta, "config", "beta")
				return []ResolvedModule{
					{Name: "alpha", SourceDir: alpha, TargetRoot: "~/.config"},
					{Name: "beta", SourceDir: beta, TargetRoot: "~/.config"},
				}
			},
			wants: []string{`module "alpha"`, `source "config"`, `module "beta"`, `target "~/.config/config"`},
			path:  filepath.FromSlash(".config/config"),
		},
		{
			name: "template suffix",
			modules: func(t *testing.T) []ResolvedModule {
				root := t.TempDir()
				writeSourceFile(t, root, "config", "link")
				writeSourceFile(t, root, "config.template", "scaffold")
				return []ResolvedModule{{Name: "app", SourceDir: root, TargetRoot: "~"}}
			},
			wants: []string{`source "config"`, `source "config.template"`, `target "~/config"`},
			path:  "config",
		},
		{
			name: "explicit tmpl scaffold suffix",
			modules: func(t *testing.T) []ResolvedModule {
				root := t.TempDir()
				writeSourceFile(t, root, "settings", "link")
				writeSourceFile(t, root, "settings.tmpl", "scaffold")
				return []ResolvedModule{{
					Name:       "app",
					SourceDir:  root,
					TargetRoot: "~",
					FileRules: []ResolvedFileRule{{
						Source: "settings.tmpl",
						Kind:   FileKindScaffold,
						Mode:   "0600",
					}},
				}}
			},
			wants: []string{`source "settings"`, `source "settings.tmpl"`, `target "~/settings"`},
			path:  "settings",
		},
		{
			name: "target override",
			modules: func(t *testing.T) []ResolvedModule {
				root := t.TempDir()
				writeSourceFile(t, root, "first", "first")
				writeSourceFile(t, root, "second", "second")
				return []ResolvedModule{{
					Name:       "app",
					SourceDir:  root,
					TargetRoot: "~/.config/app",
					FileRules: []ResolvedFileRule{
						{Source: "first", Kind: FileKindLink, TargetOverride: "~/.config/app/shared"},
						{Source: "second", Kind: FileKindLink, TargetOverride: "~/.config/app/shared"},
					},
				}}
			},
			wants: []string{`source "first"`, `source "second"`, `target "~/.config/app/shared"`},
			path:  filepath.FromSlash(".config/app/shared"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := testResolvedProfile(test.modules(t)...)
			home := t.TempDir()
			writeBoundaryTarget(t, filepath.Join(home, test.path))
			entries, err := profile.validateTargetStructure(home)
			if !errors.Is(err, paths.ErrTargetOverlap) {
				t.Fatalf("validateTargetStructure() = (%#v, %v), want nil ErrTargetOverlap", entries, err)
			}
			if entries != nil {
				t.Fatalf("validateTargetStructure() entries = %#v, want nil", entries)
			}
			for _, want := range test.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("validateTargetStructure() error = %q, want %q", err, want)
				}
			}
			if want := filepath.Join(home, test.path); !strings.Contains(err.Error(), want) {
				t.Errorf("validateTargetStructure() error = %q, want absolute path %q", err, want)
			}
		})
	}
}

func TestResolvedProfileValidateTargetStructure_RejectsAncestorConflict(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "parent", "parent")
	writeSourceFile(t, root, "child", "child")
	profile := testResolvedProfile(ResolvedModule{
		Name:       "app",
		SourceDir:  root,
		TargetRoot: "~",
		FileRules: []ResolvedFileRule{
			{Source: "parent", Kind: FileKindLink, TargetOverride: "~/.config/app"},
			{Source: "child", Kind: FileKindLink, TargetOverride: "~/.config/app/child"},
		},
	})

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".config", "app"), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(target parent) error = %v", err)
	}
	writeBoundaryTarget(t, filepath.Join(home, ".config", "app", "child"))
	entries, err := profile.validateTargetStructure(home)
	if !errors.Is(err, paths.ErrTargetOverlap) || entries != nil {
		t.Fatalf("validateTargetStructure() = (%#v, %v), want nil ErrTargetOverlap", entries, err)
	}
	for _, want := range []string{
		`source "parent"`,
		`source "child"`,
		"ancestor",
		filepath.Join(home, ".config", "app"),
		filepath.Join(home, ".config", "app", "child"),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("validateTargetStructure() error = %q, want %q", err, want)
		}
	}
}

func TestResolvedProfileValidateTargetStructure_RejectsInvalidHomeAndBlockedTarget(t *testing.T) {
	profile := testResolvedProfile()
	if entries, err := profile.validateTargetStructure("relative"); err == nil || entries != nil {
		t.Fatalf("validateTargetStructure(relative) = (%#v, %v), want nil error", entries, err)
	}

	source := t.TempDir()
	writeSourceFile(t, source, "child", "child")
	profile = testResolvedProfile(ResolvedModule{Name: "app", SourceDir: source, TargetRoot: "~/blocked"})
	home := t.TempDir()
	blocked := filepath.Join(home, "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", blocked, err)
	}

	entries, err := profile.validateTargetStructure(home)
	if !errors.Is(err, paths.ErrPathBlocked) || entries != nil {
		t.Fatalf("validateTargetStructure() = (%#v, %v), want nil ErrPathBlocked", entries, err)
	}
	for _, want := range []string{
		`module "app"`,
		`source "child"`,
		`target "~/blocked/child"`,
		filepath.Join(home, "blocked", "child"),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("validateTargetStructure() error = %q, want %q", err, want)
		}
	}
}

func TestResolvedProfileEnumerate_TemplateErrorsReturnNoPrePlanResult(t *testing.T) {
	tests := []struct {
		name        string
		invalid     string
		contextData map[string]string
		want        string
	}{
		{name: "parse", invalid: `{{ if }}`, contextData: map[string]string{"email": "value"}, want: "parse template"},
		{name: "undeclared variable", invalid: `{{ .missing }}`, contextData: map[string]string{"email": "value"}, want: "not declared by manifest data"},
		{name: "render", invalid: `{{ default "fallback" 1 }}`, contextData: map[string]string{"email": "value"}, want: "render template"},
		{name: "missing declared data", invalid: `literal`, contextData: map[string]string{}, want: "rerun init"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
base = ["app"]
[data.email]
default = "manifest fallback must not render"
`)
			writeModule(t, repo, "app", "")
			root := filepath.Join(repo, "modules", "app")
			writeSourceFile(t, root, "a.template", "valid first")
			writeSourceFile(t, root, "z.template", tt.invalid)
			loaded, err := Load(repo)
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			profile, err := loaded.Resolve("base", "darwin")
			if err != nil {
				t.Fatalf("Resolve() error = %v, want nil", err)
			}
			home := filepath.Join(t.TempDir(), "target-home")
			context := testRuntimeContext(home)
			context.Data = tt.contextData
			before := snapshotTree(t, repo)

			entries, err := profile.Enumerate(context)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Enumerate() error = %v, want containing %q", err, tt.want)
			}
			if entries != nil {
				t.Fatalf("Enumerate() entries = %#v, want nil pre-plan result", entries)
			}
			if after := snapshotTree(t, repo); !reflect.DeepEqual(after, before) {
				t.Fatalf("Enumerate() changed repository\nbefore: %v\nafter:  %v", before, after)
			}
			if _, statErr := os.Lstat(home); !os.IsNotExist(statErr) {
				t.Fatalf("Enumerate() target home Lstat error = %v, want not exist", statErr)
			}
		})
	}
}

func TestParseDesiredMode(t *testing.T) {
	mode, err := parseDesiredMode("app", "config.template", FileKindScaffold, "0777")
	if err != nil {
		t.Fatalf("parseDesiredMode() error = %v, want nil", err)
	}
	if mode != fs.FileMode(0o777) {
		t.Fatalf("parseDesiredMode() = %#o, want 0777", mode)
	}
}

func testResolvedProfile(modules ...ResolvedModule) ResolvedProfile {
	return ResolvedProfile{name: "base", modules: modules, goos: "darwin"}
}

func testRuntimeContext(home string) RuntimeContext {
	return RuntimeContext{
		OS:       "darwin",
		Arch:     "arm64",
		Hostname: "test-host",
		Profile:  "base",
		Home:     home,
	}
}
