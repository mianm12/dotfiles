package add

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
)

func TestPreflight_LinkPlanIsSelfContainedAndReadOnly(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, ".config/app/config", "content\n", 0o640)
	inputs := fixture.load(t)
	before := snapshotAddTree(t, fixture.root)

	plan, err := Preflight(inputs, Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if plan.Profile() != "base" || plan.Home() != fixture.home || plan.Repository() != fixture.repo ||
		plan.GOOS() != runtime.GOOS || !plan.DevelopmentBuild() || len(plan.Items()) != 1 {
		t.Fatalf("Preflight() plan = %#v", plan)
	}
	item := plan.Items()[0]
	if item.TargetPath() != target || item.Target() != "~/.config/app/config" || item.Module() != "app" ||
		item.Source() != ".config/app/config" || item.SourcePath() != filepath.Join(fixture.repo, "modules", "app", ".config", "app", "config") ||
		item.Kind() != manifest.FileKindLink || item.SourceExists() || item.Snapshot().Mode() != 0o640 || string(item.Snapshot().Content()) != "content\n" {
		t.Fatalf("Preflight() item = %#v", item)
	}
	if after := snapshotAddTree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("Preflight() changed fixture: before=%v after=%v", before, after)
	}
	if _, err := os.Lstat(fixture.control.StateLock()); !os.IsNotExist(err) {
		t.Fatalf("Preflight() lock Lstat error = %v, want missing", err)
	}
}

func TestBatchPlanSeal_ZeroForgedAndReturnedCopies(t *testing.T) {
	var zero BatchPlan
	if zero.Valid() || zero.Items() != nil || zero.DevelopmentBuild() {
		t.Fatalf("zero BatchPlan = %#v, want invalid without items", zero)
	}
	forged := BatchPlan{profile: "base", home: "/home", repository: "/repo"}
	if forged.Valid() {
		t.Fatal("field-populated BatchPlan without successful preflight is valid")
	}

	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o640)
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if !plan.Valid() {
		t.Fatal("successful Preflight() returned invalid plan")
	}
	items := plan.Items()
	if len(items) != 1 || !items[0].Valid() || !items[0].Snapshot().Valid() {
		t.Fatalf("Items() = %#v, want one sealed item and snapshot", items)
	}
	content := items[0].Snapshot().Content()
	content[0] = 'X'
	items[0] = ItemPlan{}

	again := plan.Items()
	if len(again) != 1 || !again[0].Valid() || string(again[0].Snapshot().Content()) != "content" {
		t.Fatalf("mutating accessor results changed plan: %#v", again)
	}
	identity, err := paths.ResolveTargetIdentity(target)
	if err != nil {
		t.Fatal(err)
	}
	if !again[0].Snapshot().MatchesTargetIdentity(identity) {
		t.Fatal("sealed snapshot did not preserve target identity evidence")
	}
}

func TestPreflight_ModuleInferenceUsesUniqueCandidateOrStateEvidence(t *testing.T) {
	t.Run("unique target root", func(t *testing.T) {
		fixture := newAddFixture(t, map[string]string{
			"app":   `target = "~/.config/app"`,
			"broad": `target = "~/.other"`,
		})
		target := fixture.writeTarget(t, ".config/app/config", "x", 0o644)
		plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Mode: ModeLink})
		if err != nil {
			t.Fatalf("Preflight() error = %v", err)
		}
		if got := plan.Items()[0]; got.Module() != "app" || got.Source() != "config" {
			t.Fatalf("inferred item = %#v", got)
		}
	})

	t.Run("state evidence", func(t *testing.T) {
		fixture := newAddFixture(t, map[string]string{
			"app":   `target = "~"`,
			"broad": `target = "~"`,
		})
		fixture.writeState(t, `{"version":1,"entries":{"~/.config/app/existing":{"module":"app","kind":"scaffold","source":"modules/app/existing.template","applied_at":"2026-07-21T00:00:00Z"}},"run_once":{}}`)
		target := fixture.writeTarget(t, ".config/app/config", "x", 0o644)
		plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Mode: ModeLink})
		if err != nil {
			t.Fatalf("Preflight() error = %v", err)
		}
		if plan.Items()[0].Module() != "app" {
			t.Fatalf("inferred module = %q, want app", plan.Items()[0].Module())
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		fixture := newAddFixture(t, map[string]string{"app": `target = "~"`, "other": `target = "~"`})
		target := fixture.writeTarget(t, "config", "x", 0o644)
		_, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Mode: ModeLink})
		if !errors.Is(err, ErrModuleAmbiguous) {
			t.Fatalf("Preflight() error = %v, want ErrModuleAmbiguous", err)
		}
	})
}

func TestPreflight_ValidatesExplicitModule(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`, "other": `target = "~"`})
	writeAddFile(t, filepath.Join(fixture.repo, "dot.toml"), `requires = ">=0.3.0"
[profiles]
base = ["app"]
`, 0o644)
	target := fixture.writeTarget(t, "config", "x", 0o644)
	inputs := fixture.load(t)
	for _, test := range []struct {
		name   string
		module string
		want   string
	}{
		{name: "invalid", module: "../app", want: "invalid add module"},
		{name: "missing", module: "missing", want: "does not exist"},
		{name: "outside profile", module: "other", want: "not in the effective profile"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Preflight(inputs, Request{Paths: []string{target}, Module: test.module, Mode: ModeLink})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Preflight() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestPreflight_RejectsManifestIgnoreAndDuplicateInputs(t *testing.T) {
	t.Run("manifest ignore", func(t *testing.T) {
		fixture := newAddFixture(t, map[string]string{"app": `target = "~"
[ignore]
patterns = ["config"]`})
		target := fixture.writeTarget(t, "config", "x", 0o644)
		_, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
		if err == nil || !strings.Contains(err.Error(), "produced 0 desired entries") {
			t.Fatalf("Preflight() error = %v, want manifest ignore rejection", err)
		}
	})

	t.Run("duplicate identity before Git", func(t *testing.T) {
		fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
		target := fixture.writeTarget(t, "config", "x", 0o644)
		operations := defaultOperations()
		calls := 0
		operations.git = func(string, []string, []string) (gitResult, error) {
			calls++
			return gitResult{exitCode: 1}, nil
		}
		_, err := preflight(fixture.load(t), Request{Paths: []string{target, target}, Module: "app", Mode: ModeLink}, operations)
		if err == nil || !errors.Is(err, paths.ErrTargetOverlap) {
			t.Fatalf("preflight() error = %v, want target overlap", err)
		}
		if calls != 0 {
			t.Fatalf("Git calls = %d, want zero", calls)
		}
	})
}

func TestPreflight_RejectsInputBeforeReturningPartialPlan(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, fixture *addFixture) string
		want    string
	}{
		{name: "local", prepare: func(t *testing.T, fixture *addFixture) string {
			return fixture.writeTarget(t, "secret.local", "secret", 0o600)
		}, want: "*.local"},
		{name: "directory", prepare: func(t *testing.T, fixture *addFixture) string {
			path := filepath.Join(fixture.home, "directory")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			return path
		}, want: "ordinary file"},
		{name: "symlink", prepare: func(t *testing.T, fixture *addFixture) string {
			target := fixture.writeTarget(t, "real", "x", 0o644)
			link := filepath.Join(fixture.home, "link")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			return link
		}, want: "ordinary file"},
		{name: "existing state", prepare: func(t *testing.T, fixture *addFixture) string {
			fixture.writeState(t, `{"version":1,"entries":{"~/managed":{"module":"app","kind":"scaffold","source":"modules/app/managed.template","applied_at":"2026-07-21T00:00:00Z"}},"run_once":{}}`)
			return fixture.writeTarget(t, "managed", "x", 0o644)
		}, want: "already has state"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
			first := fixture.writeTarget(t, "valid", "valid", 0o644)
			second := tt.prepare(t, fixture)
			before := snapshotAddTree(t, fixture.root)
			plan, err := Preflight(fixture.load(t), Request{Paths: []string{first, second}, Module: "app", Mode: ModeLink})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Preflight() error = %v, want containing %q", err, tt.want)
			}
			if len(plan.Items()) != 0 {
				t.Fatalf("Preflight() returned partial plan %#v", plan)
			}
			if after := snapshotAddTree(t, fixture.root); !reflect.DeepEqual(after, before) {
				t.Fatalf("failed Preflight() changed fixture: before=%v after=%v", before, after)
			}
		})
	}
}

func TestPreflight_SourceVariantsAndEquivalentRetry(t *testing.T) {
	t.Run("equivalent source", func(t *testing.T) {
		fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
		target := fixture.writeTarget(t, "config", "same", 0o640)
		source := filepath.Join(fixture.repo, "modules", "app", "config")
		writeAddFile(t, source, "same", 0o640)
		plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
		if err != nil {
			t.Fatalf("Preflight() error = %v", err)
		}
		if !plan.Items()[0].SourceExists() {
			t.Fatal("equivalent source was not marked reusable")
		}
	})

	for _, test := range []struct {
		name    string
		path    string
		content string
		mode    fs.FileMode
	}{
		{name: "other suffix variant", path: "config.template", content: "same", mode: 0o640},
		{name: "different bytes", path: "config", content: "different", mode: 0o640},
		{name: "different mode", path: "config", content: "same", mode: 0o600},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
			target := fixture.writeTarget(t, "config", "same", 0o640)
			writeAddFile(t, filepath.Join(fixture.repo, "modules", "app", test.path), test.content, test.mode)
			_, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
			if err == nil || !strings.Contains(err.Error(), "source variant") {
				t.Fatalf("Preflight() error = %v, want source variant rejection", err)
			}
		})
	}
}

func TestPreflight_RejectsBatchSourceVariantFamilyBeforeGit(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"
[files."a.template"]
kind = "link"
target = "~/b"
[files.b]
kind = "scaffold"`})
	first := fixture.writeTarget(t, "a", "first", 0o644)
	second := fixture.writeTarget(t, "b", "second", 0o644)
	operations := defaultOperations()
	calls := 0
	operations.git = func(string, []string, []string) (gitResult, error) {
		calls++
		return gitResult{exitCode: 1}, nil
	}

	_, err := preflight(fixture.load(t), Request{
		Paths: []string{first, second}, Module: "app", Mode: ModeLink,
	}, operations)
	if err == nil || !strings.Contains(err.Error(), "source variant families") {
		t.Fatalf("preflight() error = %v, want batch source family rejection", err)
	}
	if calls != 0 {
		t.Fatalf("Git calls = %d, want zero before source families pass", calls)
	}
}

func TestValidateSourceFamilies_UsesSharedFilesystemNameIdentity(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name  string
		left  string
		right string
	}{
		{name: "case alias", left: "Config", right: "config"},
		{name: "unicode alias", left: "caf\u00e9", right: "cafe\u0301"},
	} {
		t.Run(test.name, func(t *testing.T) {
			leftPath := filepath.Join(root, test.left)
			rightPath := filepath.Join(root, test.right)
			leftIdentity, leftErr := paths.ResolveTargetIdentity(leftPath)
			rightIdentity, rightErr := paths.ResolveTargetIdentity(rightPath)

			err := validateSourceFamilies([]sourceFamily{
				{input: "left", paths: []string{leftPath}},
				{input: "right", paths: []string{rightPath}},
			})
			if leftErr != nil || rightErr != nil {
				if err == nil {
					t.Fatalf("validateSourceFamilies() accepted identities paths could not establish: (%v, %v)", leftErr, rightErr)
				}
				return
			}
			sharedSaysAlias := leftIdentity.Equal(rightIdentity)
			if sharedSaysAlias && err == nil {
				t.Fatal("validateSourceFamilies() accepted alias identified by paths")
			}
			if !sharedSaysAlias && err != nil {
				t.Fatalf("validateSourceFamilies() error = %v for distinct shared identities", err)
			}
		})
	}
}

func TestPreflight_ScaffoldRequiresRenderedBytesAndMode(t *testing.T) {
	t.Run("matching", func(t *testing.T) {
		fixture := newAddFixture(t, map[string]string{"app": `target = "~"
[files."config.template"]
mode = "0600"`})
		target := fixture.writeTarget(t, "config", "literal\n", 0o600)
		plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeScaffold})
		if err != nil {
			t.Fatalf("Preflight() error = %v", err)
		}
		if item := plan.Items()[0]; item.Kind() != manifest.FileKindScaffold || item.Source() != "config.template" {
			t.Fatalf("scaffold item = %#v", item)
		}
	})

	for _, test := range []struct {
		name     string
		content  string
		mode     fs.FileMode
		manifest string
	}{
		{name: "render changes bytes", content: "{{ .Profile }}\n", mode: 0o600, manifest: "0600"},
		{name: "mode mismatch", content: "literal\n", mode: 0o644, manifest: "0600"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAddFixture(t, map[string]string{"app": `target = "~"
[files."config.template"]
mode = "` + test.manifest + `"`})
			target := fixture.writeTarget(t, "config", test.content, test.mode)
			_, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeScaffold})
			if err == nil || !strings.Contains(err.Error(), "rendered") {
				t.Fatalf("Preflight() error = %v, want rendered consistency rejection", err)
			}
		})
	}
}

func TestPreflight_GitTrackabilityUsesRepositoryLocalAndGlobalRules(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(t *testing.T, fixture *addFixture, sourceRelative string)
		want  string
	}{
		{name: "repository ignore", setup: func(t *testing.T, fixture *addFixture, _ string) {
			writeAddFile(t, filepath.Join(fixture.repo, ".gitignore"), "modules/app/config\n", 0o644)
		}, want: "ignored by Git"},
		{name: "local exclude", setup: func(t *testing.T, fixture *addFixture, _ string) {
			writeAddFile(t, filepath.Join(fixture.repo, ".git", "info", "exclude"), "modules/app/config\n", 0o644)
		}, want: "ignored by Git"},
		{name: "global exclude", setup: func(t *testing.T, fixture *addFixture, _ string) {
			excludes := filepath.Join(fixture.home, "global-excludes")
			writeAddFile(t, excludes, "modules/app/config\n", 0o644)
			runGit(t, fixture, "config", "--global", "core.excludesFile", excludes)
		}, want: "ignored by Git"},
		{name: "tracked source remains eligible", setup: func(t *testing.T, fixture *addFixture, sourceRelative string) {
			writeAddFile(t, filepath.Join(fixture.repo, filepath.FromSlash(sourceRelative)), "same", 0o644)
			runGit(t, fixture, "add", "--", sourceRelative)
			writeAddFile(t, filepath.Join(fixture.repo, ".gitignore"), "modules/app/config\n", 0o644)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
			target := fixture.writeTarget(t, "config", "same", 0o644)
			sourceRelative := "modules/app/config"
			test.setup(t, fixture, sourceRelative)
			_, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
			if test.want == "" {
				if err != nil {
					t.Fatalf("Preflight() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Preflight() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestGitEnvironment_StripsInheritedGitAndHomeOverrides(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HOME=/wrong",
		"XDG_CONFIG_HOME=/wrong/config",
		"XDG_DATA_HOME=/wrong/data",
		"XDG_STATE_HOME=/wrong/state",
		"XDG_CACHE_HOME=/wrong/cache",
		"GIT_DIR=/alternate/repo",
		"GIT_INDEX_FILE=/alternate/index",
		"GIT_CONFIG_SYSTEM=/alternate/system",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.excludesFile",
		"GIT_CONFIG_VALUE_0=/alternate/excludes",
		"UNRELATED=value",
	}
	home := filepath.Join(t.TempDir(), "home")
	environment := gitEnvironment(base, home)
	values := environmentMap(environment)

	for _, name := range []string{
		"GIT_DIR", "GIT_INDEX_FILE", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_COUNT",
		"GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0",
	} {
		if _, exists := values[name]; exists {
			t.Fatalf("gitEnvironment() retained inherited %s", name)
		}
	}
	want := map[string]string{
		"HOME":                home,
		"XDG_CONFIG_HOME":     filepath.Join(home, ".config"),
		"XDG_DATA_HOME":       filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME":      filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME":      filepath.Join(home, ".cache"),
		"GIT_CONFIG_NOSYSTEM": "1",
		"PATH":                "/usr/bin",
		"UNRELATED":           "value",
	}
	for name, value := range want {
		if values[name] != value {
			t.Fatalf("gitEnvironment()[%q] = %q, want %q", name, values[name], value)
		}
	}
}

func TestPreflight_GitEnvironmentCannotRedirectEffectiveRepositoryOrConfig(t *testing.T) {
	t.Run("GIT_DIR", func(t *testing.T) {
		fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
		target := fixture.writeTarget(t, "config", "same", 0o644)
		writeAddFile(t, filepath.Join(fixture.repo, ".gitignore"), "modules/app/config\n", 0o644)
		alternate := filepath.Join(fixture.root, "alternate")
		if err := os.MkdirAll(alternate, 0o700); err != nil {
			t.Fatal(err)
		}
		alternateFixture := &addFixture{repo: alternate, home: fixture.home}
		runGit(t, alternateFixture, "init", "-q")
		writeAddFile(t, filepath.Join(alternate, "modules", "app", "config"), "same", 0o644)
		runGit(t, alternateFixture, "add", "--", "modules/app/config")
		t.Setenv("GIT_DIR", filepath.Join(alternate, ".git"))

		_, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
		if err == nil || !strings.Contains(err.Error(), "ignored by Git") {
			t.Fatalf("Preflight() error = %v, want effective repository ignore rejection", err)
		}
	})

	t.Run("GIT_INDEX_FILE", func(t *testing.T) {
		fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
		target := fixture.writeTarget(t, "config", "same", 0o644)
		sourceRelative := "modules/app/config"
		writeAddFile(t, filepath.Join(fixture.repo, filepath.FromSlash(sourceRelative)), "same", 0o644)
		runGit(t, fixture, "add", "--", sourceRelative)
		writeAddFile(t, filepath.Join(fixture.repo, ".gitignore"), sourceRelative+"\n", 0o644)
		alternateIndex := filepath.Join(fixture.root, "alternate.index")
		command := exec.Command("git", "-C", fixture.repo, "read-tree", "--empty")
		command.Env = append(os.Environ(), "GIT_INDEX_FILE="+alternateIndex)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("create alternate index: %v: %s", err, output)
		}
		t.Setenv("GIT_INDEX_FILE", alternateIndex)

		if _, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink}); err != nil {
			t.Fatalf("Preflight() error = %v, want effective index tracked source", err)
		}
	})

	t.Run("config count", func(t *testing.T) {
		fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
		target := fixture.writeTarget(t, "config", "same", 0o644)
		excludes := filepath.Join(fixture.home, "global-excludes")
		writeAddFile(t, excludes, "modules/app/config\n", 0o644)
		runGit(t, fixture, "config", "--global", "core.excludesFile", excludes)
		empty := filepath.Join(fixture.home, "empty-excludes")
		writeAddFile(t, empty, "", 0o644)
		t.Setenv("GIT_CONFIG_COUNT", "1")
		t.Setenv("GIT_CONFIG_KEY_0", "core.excludesFile")
		t.Setenv("GIT_CONFIG_VALUE_0", empty)

		_, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
		if err == nil || !strings.Contains(err.Error(), "ignored by Git") {
			t.Fatalf("Preflight() error = %v, want effective HOME global exclude rejection", err)
		}
	})
}

func TestPreflight_RejectsTemplateAndGitRunnerFailure(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "x", 0o644)
	inputs := fixture.load(t)
	if _, err := Preflight(inputs, Request{Paths: []string{target}, Module: "app", Mode: ModeTemplate}); !errors.Is(err, ErrTemplateUnsupported) {
		t.Fatalf("Preflight(template) error = %v, want ErrTemplateUnsupported", err)
	}

	operations := defaultOperations()
	operations.git = func(string, []string, []string) (gitResult, error) {
		return gitResult{}, errors.New("git unavailable")
	}
	if _, err := preflight(inputs, Request{Paths: []string{target}, Module: "app", Mode: ModeLink}, operations); err == nil || !strings.Contains(err.Error(), "git unavailable") {
		t.Fatalf("preflight(git failure) error = %v", err)
	}
}

func TestPreflight_ValidatesWholeInputBoundaryBeforeGit(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	valid := fixture.writeTarget(t, "valid", "x", 0o644)
	config := filepath.Join(fixture.home, ".config", "dot", "config.toml")
	inputs := fixture.load(t)
	calls := 0
	operations := defaultOperations()
	operations.git = func(string, []string, []string) (gitResult, error) {
		calls++
		return gitResult{exitCode: 1}, nil
	}

	if _, err := preflight(inputs, Request{Paths: []string{valid, config}, Module: "app", Mode: ModeLink}, operations); err == nil || !strings.Contains(err.Error(), "control") {
		t.Fatalf("preflight(control overlap) error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("Git calls = %d, want zero before whole input boundary passes", calls)
	}
}

type addFixture struct {
	root    string
	home    string
	repo    string
	control paths.ControlPlanePaths
}

func newAddFixture(t *testing.T, modules map[string]string) *addFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	for _, directory := range []string{home, repo} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, `"`+name+`"`)
	}
	slices.Sort(names)
	writeAddFile(t, filepath.Join(repo, "dot.toml"), "requires = \">=0.3.0\"\n[profiles]\nbase = ["+strings.Join(names, ", ")+"]\n", 0o644)
	for name, moduleManifest := range modules {
		moduleRoot := filepath.Join(repo, "modules", name)
		if err := os.MkdirAll(moduleRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if moduleManifest != "" {
			writeAddFile(t, filepath.Join(moduleRoot, "dot.toml"), moduleManifest+"\n", 0o644)
		}
	}
	config := filepath.Join(home, ".config", "dot", "config.toml")
	writeAddFile(t, config, "profile = \"base\"\n", 0o600)

	fixture := &addFixture{root: root, home: home, repo: repo}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	runGit(t, fixture, "init", "-q")
	fixture.control = fixture.load(t).Context().Control().Paths()
	return fixture
}

func (fixture *addFixture) load(t *testing.T) dotruntime.LoadedInputs {
	t.Helper()
	inputs, err := dotruntime.LoadReadOnly(dotruntime.Overrides{
		Home:       dotruntime.Override{Value: fixture.home, Set: true},
		Repository: dotruntime.Override{Value: fixture.repo, Set: true},
		Profile:    dotruntime.Override{Value: "base", Set: true},
	}, "dev")
	if err != nil {
		t.Fatalf("LoadReadOnly() error = %v", err)
	}
	return inputs
}

func (fixture *addFixture) writeTarget(t *testing.T, relative, content string, mode fs.FileMode) string {
	t.Helper()
	path := filepath.Join(fixture.home, filepath.FromSlash(relative))
	writeAddFile(t, path, content, mode)
	return path
}

func (fixture *addFixture) writeState(t *testing.T, content string) {
	t.Helper()
	writeAddFile(t, filepath.Join(fixture.home, ".local", "state", "dot", "state.json"), content, 0o600)
}

func writeAddFile(t *testing.T, path, content string, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, fixture *addFixture, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", fixture.repo}, args...)...)
	command.Env = append(os.Environ(), "HOME="+fixture.home, "XDG_CONFIG_HOME="+filepath.Join(fixture.home, ".config"), "GIT_CONFIG_NOSYSTEM=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v error = %v, output = %s", args, err, output)
	}
}

func environmentMap(environment []string) map[string]string {
	result := make(map[string]string, len(environment))
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		result[name] = value
	}
	return result
}

type addTreeEntry struct {
	mode fs.FileMode
	data string
}

func snapshotAddTree(t *testing.T, root string) map[string]addTreeEntry {
	t.Helper()
	result := make(map[string]addTreeEntry)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		item := addTreeEntry{mode: info.Mode()}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			item.data, err = os.Readlink(path)
		case info.Mode().IsRegular():
			var data []byte
			data, err = os.ReadFile(path)
			item.data = string(data)
		}
		if err != nil {
			return err
		}
		result[relative] = item
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot tree: %v", err)
	}
	return result
}
