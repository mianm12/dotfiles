package manifest

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateProspectivePathBoundaries_UsesNormalRulesAndRendering(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := writeRepositoryManifest(t, `
requires = ">=0.3.0"
[profiles]
base = ["app"]
[data.name]
`)
	writeModule(t, repo, "app", `
target = "~/.config/app"
[files."config.template"]
kind = "scaffold"
mode = "0600"
target = "~/.config/app/config"
`)
	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	resolved, err := loaded.Resolve("base", "darwin")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	control := writeGlobalControlFixture(t, home, repo)
	before := snapshotTree(t, root)

	validated, err := resolved.ValidateProspectivePathBoundaries(control, []ProspectiveSource{{
		Module:  "app",
		Source:  "config.template",
		Content: []byte("hello {{ .name }}\n"),
		Mode:    0o600,
	}})
	if err != nil {
		t.Fatalf("ValidateProspectivePathBoundaries() error = %v", err)
	}
	entries := validated.Entries()
	if len(entries) != 1 || entries[0].Source != "config.template" || entries[0].Content != nil {
		t.Fatalf("Entries() = %#v, want one unrendered prospective scaffold", entries)
	}
	scoped, err := validated.RenderScope(nil, RuntimeContext{
		OS: "darwin", Arch: "arm64", Hostname: "host", Profile: "base", Home: home,
		Data: map[string]string{"name": "world"},
	})
	if err != nil {
		t.Fatalf("RenderScope() error = %v", err)
	}
	want := []byte("hello world\n")
	if got := scoped.Entries(); len(got) != 1 || !reflect.DeepEqual(got[0].Content, want) || got[0].Mode != 0o600 {
		t.Fatalf("RenderScope().Entries() = %#v, want content %q mode 0600", got, want)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("prospective validation changed fixture: before=%v after=%v", before, after)
	}
	if _, err := os.Lstat(filepath.Join(repo, "modules", "app", "config.template")); !os.IsNotExist(err) {
		t.Fatalf("prospective source exists after validation: %v", err)
	}
}

func TestValidateProspectivePathBoundaries_OnlyExemptsExactCandidates(t *testing.T) {
	tests := []struct {
		name      string
		manifest  string
		candidate ProspectiveSource
		setup     func(t *testing.T, moduleRoot string)
		wantErr   string
	}{
		{
			name: "unrelated missing file rule",
			manifest: `[files.candidate]
kind = "link"
[files.unrelated]
kind = "link"`,
			candidate: ProspectiveSource{Module: "app", Source: "candidate", Content: []byte("x"), Mode: 0o644},
			wantErr:   `references missing source "unrelated"`,
		},
		{
			name:      "candidate already exists",
			candidate: ProspectiveSource{Module: "app", Source: "candidate", Content: []byte("x"), Mode: 0o644},
			setup: func(t *testing.T, moduleRoot string) {
				writeSourceFile(t, moduleRoot, "candidate", "existing")
			},
			wantErr: "already exists",
		},
		{
			name:      "candidate is built in ignored hook path",
			candidate: ProspectiveSource{Module: "app", Source: "hooks/setup.sh", Content: []byte("x"), Mode: 0o755},
			wantErr:   "built-in ignore",
		},
		{
			name:      "candidate source is not canonical",
			candidate: ProspectiveSource{Module: "app", Source: "dir/../candidate", Content: []byte("x"), Mode: 0o644},
			wantErr:   "prospective source",
		},
		{
			name:      "candidate mode has special bits",
			candidate: ProspectiveSource{Module: "app", Source: "candidate", Content: []byte("x"), Mode: fs.ModeSymlink | 0o777},
			wantErr:   "regular-file mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			repo := writeRepositoryManifest(t, `requires = ">=0.3.0"
[profiles]
base = ["app"]`)
			writeModule(t, repo, "app", tt.manifest)
			moduleRoot := filepath.Join(repo, "modules", "app")
			if tt.setup != nil {
				tt.setup(t, moduleRoot)
			}
			loaded, err := Load(repo)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			resolved, err := loaded.Resolve("base", "darwin")
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			control := writeGlobalControlFixture(t, home, repo)

			_, err = resolved.ValidateProspectivePathBoundaries(control, []ProspectiveSource{tt.candidate})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateProspectivePathBoundaries() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateProspectivePathBoundaries_RejectsUnknownOrDuplicateCandidate(t *testing.T) {
	root := t.TempDir()
	repo := writeRepositoryManifest(t, `requires = ">=0.3.0"
[profiles]
base = ["app"]`)
	writeModule(t, repo, "app", "")
	loaded, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	resolved, err := loaded.Resolve("base", "darwin")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	control := writeGlobalControlFixture(t, filepath.Join(root, "home"), repo)

	for _, test := range []struct {
		name       string
		candidates []ProspectiveSource
		want       string
	}{
		{name: "unknown module", candidates: []ProspectiveSource{{Module: "other", Source: "x", Mode: 0o644}}, want: "not effective"},
		{name: "duplicate", candidates: []ProspectiveSource{{Module: "app", Source: "x", Mode: 0o644}, {Module: "app", Source: "x", Mode: 0o644}}, want: "duplicate prospective source"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolved.ValidateProspectivePathBoundaries(control, test.candidates)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}
