package manifest

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestManagedInputIsExplicitlyUnsupported(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{name: "declared managed", manifest: "[files.config]\nkind = \"managed\""},
		{name: "declared source infers managed", manifest: "[files.\"config.tmpl\"]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = [\"app\"]")
			writeModule(t, repo, "app", tt.manifest)

			_, err := Load(repo)
			if !errors.Is(err, ErrManagedUnsupported) {
				t.Fatalf("Load() error = %v, want ErrManagedUnsupported", err)
			}
		})
	}
}

func TestResolvedProfileEnumerate_RejectsImplicitManaged(t *testing.T) {
	repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = [\"app\"]")
	writeModule(t, repo, "app", "")
	writeSourceFile(t, filepath.Join(repo, "modules", "app"), "config.tmpl", "template")
	module := resolveOnlyModule(t, repo)

	entries, err := (ResolvedProfile{Name: "base", Modules: []ResolvedModule{module}}).Enumerate(t.TempDir())
	if !errors.Is(err, ErrManagedUnsupported) {
		t.Fatalf("Enumerate() error = %v, want ErrManagedUnsupported", err)
	}
	if entries != nil {
		t.Fatalf("Enumerate() entries = %#v, want nil on managed input", entries)
	}
}

func TestResolvedProfileEnumerate_DoesNotInferManagedBeforeHigherPriorities(t *testing.T) {
	tests := []struct {
		name       string
		manifest   string
		wantKind   FileKind
		wantMode   uint32
		wantTarget string
		wantCount  int
	}{
		{
			name:      "ignore precedes suffix inference",
			manifest:  "[ignore]\npatterns = [\"*.tmpl\"]",
			wantCount: 0,
		},
		{
			name:       "explicit link precedes suffix inference",
			manifest:   "[files.\"config.tmpl\"]\nkind = \"link\"",
			wantKind:   FileKindLink,
			wantTarget: "~/config",
			wantCount:  1,
		},
		{
			name:       "explicit scaffold precedes suffix inference",
			manifest:   "[files.\"config.tmpl\"]\nkind = \"scaffold\"\nmode = \"0600\"",
			wantKind:   FileKindScaffold,
			wantMode:   0o600,
			wantTarget: "~/config",
			wantCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := writeRepositoryManifest(t, "requires = \">=0.3.0\"\n[profiles]\nbase = [\"app\"]")
			writeModule(t, repo, "app", tt.manifest)
			writeSourceFile(t, filepath.Join(repo, "modules", "app"), "config.tmpl", "template")
			module := resolveOnlyModule(t, repo)

			entries, err := (ResolvedProfile{Name: "base", Modules: []ResolvedModule{module}}).Enumerate(t.TempDir())
			if err != nil {
				t.Fatalf("Enumerate() error = %v, want nil", err)
			}
			if len(entries) != tt.wantCount {
				t.Fatalf("Enumerate() entries = %#v, want count %d", entries, tt.wantCount)
			}
			if tt.wantCount == 0 {
				return
			}
			if entries[0].Kind != tt.wantKind || uint32(entries[0].Mode) != tt.wantMode || entries[0].Target != tt.wantTarget {
				t.Fatalf(
					"Enumerate() entry = %#v, want kind=%q mode=%#o target=%q",
					entries[0],
					tt.wantKind,
					tt.wantMode,
					tt.wantTarget,
				)
			}
		})
	}
}

func TestResolvedProfileEnumerate_RejectsInjectedManagedRule(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "config", "template")
	profile := ResolvedProfile{Modules: []ResolvedModule{{
		Name:       "app",
		SourceDir:  root,
		TargetRoot: "~",
		FileRules: []ResolvedFileRule{{
			Source: "config",
			Kind:   FileKind(managedFileKindName),
		}},
	}}}

	entries, err := profile.Enumerate(t.TempDir())
	if !errors.Is(err, ErrManagedUnsupported) {
		t.Fatalf("Enumerate() error = %v, want ErrManagedUnsupported", err)
	}
	if !reflect.DeepEqual(entries, []DesiredEntry(nil)) {
		t.Fatalf("Enumerate() entries = %#v, want nil", entries)
	}
}
