package runtime

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPreflight_ResolvesRepositoryAndProfilePriority(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", home, err)
	}
	configuredRepo := filepath.Join(root, "configured-repo")
	environmentRepo := filepath.Join(root, "environment-repo")
	flagRepo := filepath.Join(root, "flag-repo")
	configPath := writeMachineConfig(t, root, "machine.toml", strings.Join([]string{
		`profile = "configured"`,
		`repo = "` + configuredRepo + `"`,
		`[data]`,
		`email = "me@example.com"`,
	}, "\n"))

	tests := []struct {
		name        string
		repo        string
		repoSet     bool
		profile     string
		profileSet  bool
		environment map[string]string
		wantRepo    string
		wantProfile string
	}{
		{
			name:        "flag overrides environment and config",
			repo:        flagRepo,
			repoSet:     true,
			profile:     "flag-profile",
			profileSet:  true,
			environment: map[string]string{"DOT_REPO": environmentRepo},
			wantRepo:    flagRepo,
			wantProfile: "flag-profile",
		},
		{
			name:        "environment overrides config",
			environment: map[string]string{"DOT_REPO": environmentRepo},
			wantRepo:    environmentRepo,
			wantProfile: "configured",
		},
		{
			name:        "config supplies repository and profile",
			wantRepo:    configuredRepo,
			wantProfile: "configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			environment := map[string]string{"DOT_CONFIG": configPath}
			for key, value := range tt.environment {
				environment[key] = value
			}
			resolver := NewResolver(lookup(environment), fixedHome(home))
			context, err := resolver.Preflight(Overrides{
				Home:       Override{Value: home, Set: true},
				Repository: Override{Value: tt.repo, Set: tt.repoSet},
				Profile:    Override{Value: tt.profile, Set: tt.profileSet},
			})
			if err != nil {
				t.Fatalf("Preflight() error = %v", err)
			}
			if context.Control().RepositoryPath() != tt.wantRepo {
				t.Errorf("Preflight().RepositoryPath() = %q, want %q", context.Control().RepositoryPath(), tt.wantRepo)
			}
			if context.Profile() != tt.wantProfile {
				t.Errorf("Preflight().Profile() = %q, want %q", context.Profile(), tt.wantProfile)
			}
			data := context.Data()
			if data["email"] != "me@example.com" {
				t.Errorf("Preflight().Data()[email] = %q, want machine value", data["email"])
			}
			data["email"] = "changed"
			second, err := resolver.Preflight(Overrides{
				Home:       Override{Value: home, Set: true},
				Repository: Override{Value: tt.repo, Set: tt.repoSet},
				Profile:    Override{Value: tt.profile, Set: tt.profileSet},
			})
			if err != nil {
				t.Fatalf("second Preflight() error = %v", err)
			}
			if second.Data()["email"] != "me@example.com" || context.Data()["email"] != "me@example.com" {
				t.Errorf("Preflight() retained caller data mutation: second=%q original=%q", second.Data()["email"], context.Data()["email"])
			}
		})
	}
}

func TestPreflight_UsesDefaultRepository(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", home, err)
	}
	configPath := writeMachineConfig(t, root, "machine.toml", `profile = "mac"`)

	resolver := NewResolver(lookup(map[string]string{"DOT_CONFIG": configPath}), fixedHome(home))
	context, err := resolver.Preflight(Overrides{
		Home: Override{Value: home, Set: true},
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	want := filepath.Join(home, ".local", "share", "dot", "repo")
	if context.Control().RepositoryPath() != want {
		t.Errorf("Preflight().RepositoryPath() = %q, want %q", context.Control().RepositoryPath(), want)
	}
}

func TestPreflight_DefaultSourcesDoNotRequireCallerInjection(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", home, err)
	}
	configPath := writeMachineConfig(t, root, "machine.toml", `profile = "mac"`)
	t.Setenv("DOT_CONFIG", configPath)

	context, err := Preflight(Overrides{
		Home:       Override{Value: home, Set: true},
		Repository: Override{Value: filepath.Join(root, "repo"), Set: true},
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if context.Control().ConfigPath() != configPath {
		t.Fatalf("Preflight().ConfigPath() = %q, want %q", context.Control().ConfigPath(), configPath)
	}
}

func TestPreflight_RejectsInvalidConfigAndExplicitProfile(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", home, err)
	}
	validRepo := filepath.Join(root, "repo")

	tests := []struct {
		name       string
		content    string
		profile    string
		profileSet bool
		want       string
	}{
		{name: "unknown field", content: "profile = \"mac\"\nunknown = true", want: "decode machine config"},
		{name: "wrong data type", content: "profile = \"mac\"\n[data]\nemail = 1", want: "decode machine config"},
		{name: "empty explicit profile", content: `profile = "mac"`, profileSet: true, want: "--profile must not be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := writeMachineConfig(t, root, strings.ReplaceAll(tt.name, " ", "-")+".toml", tt.content)
			resolver := NewResolver(lookup(map[string]string{"DOT_CONFIG": configPath}), fixedHome(home))
			_, err := resolver.Preflight(Overrides{
				Home:       Override{Value: home, Set: true},
				Repository: Override{Value: validRepo, Set: true},
				Profile:    Override{Value: tt.profile, Set: tt.profileSet},
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Preflight() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestPreflight_RejectsInvalidConfiguredRepositoryDespiteOverride(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", home, err)
	}
	configPath := writeMachineConfig(t, root, "machine.toml", "profile = \"mac\"\nrepo = \"relative/repo\"")

	resolver := NewResolver(lookup(map[string]string{"DOT_CONFIG": configPath}), fixedHome(home))
	_, err := resolver.Preflight(Overrides{
		Home:       Override{Value: home, Set: true},
		Repository: Override{Value: filepath.Join(root, "override-repo"), Set: true},
	})
	if err == nil || !strings.Contains(err.Error(), "machine config repo") {
		t.Fatalf("Preflight() error = %v, want invalid machine config repo", err)
	}
}

func TestPreflight_ConfigMissingPolicies(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", home, err)
	}
	overrides := Overrides{
		Home: Override{Value: home, Set: true},
	}
	resolver := NewResolver(lookup(nil), fixedHome(home))

	if _, err := resolver.Preflight(overrides); err == nil || !strings.Contains(err.Error(), "run dot init") {
		t.Fatalf("Preflight() error = %v, want missing config guidance", err)
	}
	initContext, err := resolver.PreflightInit(overrides)
	if err != nil {
		t.Fatalf("PreflightInit() error = %v", err)
	}
	if !initContext.ConfigMissing() {
		t.Error("PreflightInit().ConfigMissing() = false, want true")
	}
	if _, ok := initContext.ExistingMachine(); ok {
		t.Error("PreflightInit().ExistingMachine() ok = true, want false")
	}
	repository, err := resolver.PreflightRepository(overrides)
	if err != nil {
		t.Fatalf("PreflightRepository() error = %v", err)
	}
	wantRepo := filepath.Join(home, ".local", "share", "dot", "repo")
	if repository.RepositoryPath() != wantRepo {
		t.Errorf("PreflightRepository().RepositoryPath() = %q, want %q", repository.RepositoryPath(), wantRepo)
	}
}

func TestPreflightInit_PreservesExistingMachineAndExplicitProfileSeparately(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", home, err)
	}
	configPath := writeMachineConfig(t, root, "machine.toml", strings.Join([]string{
		`profile = "configured"`,
		`[data]`,
		`email = "me@example.com"`,
	}, "\n"))
	resolver := NewResolver(lookup(map[string]string{"DOT_CONFIG": configPath}), fixedHome(home))

	context, err := resolver.PreflightInit(Overrides{
		Home:    Override{Value: home, Set: true},
		Profile: Override{Value: "requested", Set: true},
	})
	if err != nil {
		t.Fatalf("PreflightInit() error = %v", err)
	}
	if context.ConfigMissing() {
		t.Fatal("ConfigMissing() = true, want false")
	}
	machine, ok := context.ExistingMachine()
	if !ok || machine.Profile() != "configured" || machine.Data()["email"] != "me@example.com" {
		t.Fatalf("ExistingMachine() = (%#v, %t), want configured machine", machine, ok)
	}
	profile, ok := context.ProfileOverride()
	if !ok || profile != "requested" {
		t.Fatalf("ProfileOverride() = (%q, %t), want requested, true", profile, ok)
	}
	data := machine.Data()
	data["email"] = "changed"
	if machine.Data()["email"] != "me@example.com" {
		t.Fatal("MachineContext.Data() exposed mutable internal state")
	}
}

func TestResolver_RejectsMissingSourcesWithoutPanic(t *testing.T) {
	_, err := NewResolver(nil, nil).PreflightRepository(Overrides{})
	if err == nil || !strings.Contains(err.Error(), "source is nil") {
		t.Fatalf("PreflightRepository() error = %v, want missing source", err)
	}
}

func TestPreflight_IsCWDIndependent(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(filepath.Join(home, "machine"), 0o700); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	configPath := filepath.Join(home, "machine", "config.toml")
	if err := os.WriteFile(configPath, []byte("profile = \"mac\"\nrepo = \"~/repo\"\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", configPath, err)
	}
	cwd := filepath.Join(root, "cwd")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", cwd, err)
	}
	t.Chdir(cwd)

	resolver := NewResolver(lookup(map[string]string{"DOT_CONFIG": "~/machine/config.toml"}), fixedHome(home))
	context, err := resolver.Preflight(Overrides{
		Home: Override{Value: home, Set: true},
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	control := context.Control()
	if control.ConfigPath() != configPath || control.RepositoryPath() != filepath.Join(home, "repo") {
		t.Errorf("Preflight() paths = (%q, %q), want HOME-relative absolute paths", control.ConfigPath(), control.RepositoryPath())
	}
}

func TestPreflight_RejectsControlPlaneOverlap(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", repo, err)
	}
	configPath := writeMachineConfig(t, repo, "config.toml", `profile = "mac"`)

	resolver := NewResolver(lookup(map[string]string{"DOT_CONFIG": configPath}), fixedHome(home))
	_, err := resolver.Preflight(Overrides{
		Home:       Override{Value: home, Set: true},
		Repository: Override{Value: repo, Set: true},
	})
	if err == nil || !strings.Contains(err.Error(), "control-plane paths overlap") {
		t.Fatalf("Preflight() error = %v, want control-plane overlap", err)
	}
}

func TestPreflight_IsReadOnlyAndDoesNotCreateStateOrLock(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", home, err)
	}
	configPath := writeMachineConfig(t, root, "machine.toml", `profile = "mac"`)
	before := snapshotTree(t, root)

	resolver := NewResolver(lookup(map[string]string{"DOT_CONFIG": configPath}), fixedHome(home))
	if _, err := resolver.Preflight(Overrides{
		Home:       Override{Value: home, Set: true},
		Repository: Override{Value: filepath.Join(root, "repo"), Set: true},
	}); err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	after := snapshotTree(t, root)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("Preflight() changed fixture tree:\nbefore=%v\nafter=%v", before, after)
	}
	for _, path := range []string{
		filepath.Join(home, ".local", "state", "dot"),
		filepath.Join(home, ".local", "state", "dot", "state.json"),
		filepath.Join(home, ".local", "state", "dot", "lock"),
	} {
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("os.Lstat(%q) error = %v, want fs.ErrNotExist", path, err)
		}
	}
}

func writeMachineConfig(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content+"\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}

func lookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func fixedHome(home string) func() (string, error) {
	return func() (string, error) {
		return home, nil
	}
}

func snapshotTree(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, relative+":"+entry.Type().String())
		return nil
	}); err != nil {
		t.Fatalf("filepath.WalkDir(%q) error = %v", root, err)
	}
	return entries
}
