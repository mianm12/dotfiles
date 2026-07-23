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
[files.scaffold]
kind = "scaffold"
[files.seed]
kind = "scaffold"
mode = "0700"
`)
	appRoot := filepath.Join(repo, "modules", "app")
	writeSourceFile(t, appRoot, "z", "link")
	writeSourceFile(t, appRoot, "README.md", "explicit beats ignore")
	writeSourceFile(t, appRoot, "ignored.txt", "ignored")
	writeSourceFile(t, appRoot, "literal.template", `{{ env "HOME" }}`)
	writeSourceFile(t, appRoot, "scaffold", "scaffold")
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
			Target:     "~/.config/app/literal.template",
			TargetPath: filepath.Join(home, ".config", "app", "literal.template"),
			Kind:       FileKindLink,
		},
		{
			Module:     "app",
			Source:     "scaffold",
			SourcePath: filepath.Join(appRoot, "scaffold"),
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

func TestResolvedProfileEnumerate_ReadsLiteralScaffoldContentAndMode(t *testing.T) {
	repo := writeRepositoryManifest(t, `
[profiles]
base = ["app"]
`)
	writeModule(t, repo, "app", `
[files.config]
kind = "scaffold"
mode = "0600"
`)
	root := filepath.Join(repo, "modules", "app")
	writeSourceFile(t, root, "config", "{{ .email }} stays literal")
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

	entries, err := profile.Enumerate(context)
	if err != nil {
		t.Fatalf("Enumerate() error = %v, want nil", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Enumerate() entries = %#v, want one", entries)
	}
	wantContent := "{{ .email }} stays literal"
	if string(entries[0].Content) != wantContent || entries[0].Mode != 0o600 {
		t.Fatalf(
			"Enumerate() entry = %#v, want content %q and mode 0600",
			entries[0],
			wantContent,
		)
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

func TestResolvedProfileValidateTargetStructure_DoesNotReadScaffolds(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "config", `{{ if }}`)
	profile := testResolvedProfile(ResolvedModule{
		Name:       "app",
		SourceDir:  root,
		TargetRoot: "~",
		FileRules:  []ResolvedFileRule{{Source: "config", Kind: FileKindScaffold, Mode: "0644"}},
	})
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
	if len(entries) != 1 || entries[0].Source != "config" || entries[0].Content != nil {
		t.Fatalf("validateTargetStructure() entries = %#v, want unread scaffold", entries)
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

	enumerated, err := profile.Enumerate(testRuntimeContext(home))
	if err != nil || len(enumerated) != 1 || string(enumerated[0].Content) != "{{ if }}" {
		t.Fatalf("Enumerate() = (%#v, %v), want literal scaffold content", enumerated, err)
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
		OS:      "darwin",
		Profile: "base",
		Home:    home,
	}
}
