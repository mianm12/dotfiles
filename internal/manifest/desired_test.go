package manifest

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
	writeSourceFile(t, appRoot, "literal.template", "link with stripped suffix")
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
	before := snapshotTree(t, repo)

	first, err := resolved.Enumerate(home)
	if err != nil {
		t.Fatalf("Enumerate() error = %v, want nil", err)
	}
	second, err := resolved.Enumerate(home)
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
		},
		{
			Module:     "app",
			Source:     "seed",
			SourcePath: filepath.Join(appRoot, "seed"),
			Target:     "~/.config/app/seed",
			TargetPath: filepath.Join(home, ".config", "app", "seed"),
			Kind:       FileKindScaffold,
			Mode:       0o700,
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
	third, err := resolved.Enumerate(home)
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
			_, err := (ResolvedProfile{Name: "base", Modules: []ResolvedModule{module}}).Enumerate(t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Enumerate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestResolvedProfileEnumerate_RejectsEmptySuffixResult(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, ".template", "value")
	profile := ResolvedProfile{Modules: []ResolvedModule{{Name: "app", SourceDir: root, TargetRoot: "~"}}}

	_, err := profile.Enumerate(t.TempDir())
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
			profile := ResolvedProfile{Modules: []ResolvedModule{{
				Name:       "app",
				SourceDir:  root,
				TargetRoot: "~",
				FileRules: []ResolvedFileRule{{
					Source:         tt.source,
					Kind:           tt.kind,
					Mode:           tt.mode,
					TargetOverride: "~/.config",
				}},
			}}}

			entries, err := profile.Enumerate(t.TempDir())
			if err != nil {
				t.Fatalf("Enumerate() error = %v, want nil", err)
			}
			if len(entries) != 1 || entries[0].Target != "~/.config" {
				t.Fatalf("Enumerate() entries = %#v, want one entry targeting ~/.config", entries)
			}
		})
	}
}

func TestResolvedProfileEnumerate_RejectsInvalidHome(t *testing.T) {
	profile := ResolvedProfile{}
	for _, home := range []string{"", "relative", "~/home"} {
		if _, err := profile.Enumerate(home); err == nil {
			t.Errorf("Enumerate(%q) error = nil, want invalid HOME error", home)
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
