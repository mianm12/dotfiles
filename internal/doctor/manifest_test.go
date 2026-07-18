package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCheckManifest_ContinuesIndependentDiagnostics(t *testing.T) {
	root, repo := newGitRepository(t)
	writeFile(t, filepath.Join(repo, "dot.toml"), `
requires = "^1.0.0"
unknown = true
[profiles]
base = []
`)
	writeFile(t, filepath.Join(repo, "machine.local"), "private")
	writeFile(t, filepath.Join(repo, "modules", "app", "nested.local"), "private")
	writeFile(t, filepath.Join(repo, "untracked.local"), "private")
	git(t, root, repo, "add", "dot.toml", "machine.local", "modules/app/nested.local")

	result := CheckManifest(context.Background(), manifestOptions(t, root, repo, "v0.1.0"))
	if result.ExitCode() != 1 {
		t.Fatalf("CheckManifest().ExitCode() = %d, want 1", result.ExitCode())
	}

	gotChecks := make([]string, 0, len(result.Findings()))
	for _, finding := range result.Findings() {
		gotChecks = append(gotChecks, finding.Check)
	}
	wantChecks := []string{
		"git.tracked-local",
		"git.tracked-local",
		"manifest.load",
		"manifest.requires",
	}
	if !reflect.DeepEqual(gotChecks, wantChecks) {
		t.Fatalf("CheckManifest() checks = %v, want %v; findings = %#v", gotChecks, wantChecks, result.Findings())
	}

	messages := findingsText(result)
	for _, want := range []string{"machine.local", "modules/app/nested.local", "strict mode", "invalid requires"} {
		if !strings.Contains(messages, want) {
			t.Errorf("CheckManifest() findings = %q, want containing %q", messages, want)
		}
	}
	if strings.Contains(messages, "untracked.local") {
		t.Errorf("CheckManifest() findings = %q, want untracked file omitted", messages)
	}
}

func TestCheckManifest_RequiresOutcomesDoNotStopGit(t *testing.T) {
	tests := []struct {
		name     string
		requires string
		version  string
		wantText string
	}{
		{name: "missing", version: "v1.0.0", wantText: "requires is missing"},
		{name: "invalid", requires: `requires = "^1.0.0"`, version: "v1.0.0", wantText: "invalid requires"},
		{name: "unsatisfied", requires: `requires = ">=2.0.0"`, version: "v1.0.0", wantText: "does not satisfy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, repo := newGitRepository(t)
			manifest := tt.requires + "\n[profiles]\nbase = []\n"
			writeFile(t, filepath.Join(repo, "dot.toml"), manifest)
			writeFile(t, filepath.Join(repo, "tracked.local"), "private")
			git(t, root, repo, "add", "dot.toml", "tracked.local")

			result := CheckManifest(context.Background(), manifestOptions(t, root, repo, tt.version))
			text := findingsText(result)
			for _, want := range []string{tt.wantText, "tracked.local"} {
				if !strings.Contains(text, want) {
					t.Errorf("CheckManifest() findings = %q, want containing %q", text, want)
				}
			}
		})
	}
}

func TestCheckManifest_UnsatisfiedRequirementStillRunsStrictLoad(t *testing.T) {
	root, repo := newGitRepository(t)
	writeFile(t, filepath.Join(repo, "dot.toml"), `
requires = ">=2.0.0"
unknown = true
[profiles]
base = []
`)
	writeFile(t, filepath.Join(repo, "tracked.local"), "private")
	git(t, root, repo, "add", "dot.toml", "tracked.local")

	result := CheckManifest(context.Background(), manifestOptions(t, root, repo, "v1.0.0"))
	checks := make([]string, 0, len(result.Findings()))
	for _, finding := range result.Findings() {
		checks = append(checks, finding.Check)
	}
	want := []string{"git.tracked-local", "manifest.load", "manifest.requires"}
	if !reflect.DeepEqual(checks, want) {
		t.Fatalf("CheckManifest() checks = %v, want %v; findings = %#v", checks, want, result.Findings())
	}
}

func TestCheckManifest_InvalidTOMLStillQueriesGit(t *testing.T) {
	root, repo := newGitRepository(t)
	writeFile(t, filepath.Join(repo, "dot.toml"), "requires =")
	writeFile(t, filepath.Join(repo, "tracked.local"), "private")
	git(t, root, repo, "add", "dot.toml", "tracked.local")

	result := CheckManifest(context.Background(), manifestOptions(t, root, repo, "v1.0.0"))
	checks := make([]string, 0, len(result.Findings()))
	for _, finding := range result.Findings() {
		checks = append(checks, finding.Check)
	}
	if want := []string{"git.tracked-local", "manifest.load"}; !reflect.DeepEqual(checks, want) {
		t.Fatalf("CheckManifest() checks = %v, want %v; findings = %#v", checks, want, result.Findings())
	}
}

func TestCheckManifest_DevelopmentNoticeIsNotWarning(t *testing.T) {
	root, repo := newGitRepository(t)
	writeFile(t, filepath.Join(repo, "dot.toml"), "requires = \">=999.0.0\"\n[profiles]\nbase = []\n")

	result := CheckManifest(context.Background(), manifestOptions(t, root, repo, "dev"))
	if result.ExitCode() != 0 || len(result.Findings()) != 0 {
		t.Fatalf("CheckManifest() = %#v, exit %d; want no findings and exit 0", result.Findings(), result.ExitCode())
	}
	if got := result.Notices(); !reflect.DeepEqual(got, []string{developmentNotice}) {
		t.Fatalf("CheckManifest().Notices() = %v, want development notice", got)
	}
}

func TestCheckManifest_GitQueryFailureIsError(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", repo, err)
	}
	writeFile(t, filepath.Join(repo, "dot.toml"), "requires = \">=0.1.0\"\n[profiles]\nbase = []\n")

	result := CheckManifest(context.Background(), manifestOptions(t, root, repo, "v0.1.0"))
	if result.ExitCode() != 1 || len(result.Findings()) != 1 {
		t.Fatalf("CheckManifest() findings = %#v, exit %d; want one error", result.Findings(), result.ExitCode())
	}
	finding := result.Findings()[0]
	if finding.Check != "git.index" || !strings.Contains(finding.Message, "query Git index") {
		t.Fatalf("CheckManifest() finding = %#v, want Git query error", finding)
	}
}

func TestCheckManifest_GitEnvironmentCannotRedirectIndex(t *testing.T) {
	root, repo := newGitRepository(t)
	writeFile(t, filepath.Join(repo, "dot.toml"), "requires = \">=0.1.0\"\n[profiles]\nbase = []\n")
	writeFile(t, filepath.Join(repo, "tracked.local"), "private")
	git(t, root, repo, "add", "dot.toml", "tracked.local")

	decoyRoot, decoy := newGitRepository(t)
	writeFile(t, filepath.Join(decoy, "dot.toml"), "decoy")
	git(t, decoyRoot, decoy, "add", "dot.toml")
	tests := []struct {
		name        string
		environment map[string]string
	}{
		{name: "literal pathspec", environment: map[string]string{"GIT_LITERAL_PATHSPECS": "1"}},
		{name: "alternate index", environment: map[string]string{
			"GIT_INDEX_FILE": filepath.Join(decoy, ".git", "index"),
		}},
		{name: "alternate repository", environment: map[string]string{
			"GIT_DIR":       filepath.Join(decoy, ".git"),
			"GIT_WORK_TREE": decoy,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for name, value := range tt.environment {
				t.Setenv(name, value)
			}
			result := CheckManifest(context.Background(), manifestOptions(t, root, repo, "v0.1.0"))
			if checks := findingChecks(result); !reflect.DeepEqual(checks, []string{"git.tracked-local"}) {
				t.Fatalf("CheckManifest() checks = %v, want tracked local despite Git environment; findings = %#v", checks, result.Findings())
			}
		})
	}
}

func TestIsolatedGitEnvironment(t *testing.T) {
	environment := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"GIT_DIR=/tmp/other.git",
		"GIT_INDEX_FILE=/tmp/index",
		"GIT_LITERAL_PATHSPECS=1",
	}
	want := []string{"PATH=/usr/bin", "HOME=/tmp/home"}
	if got := isolatedGitEnvironment(environment); !reflect.DeepEqual(got, want) {
		t.Fatalf("isolatedGitEnvironment() = %v, want %v", got, want)
	}
}

func findingsText(result Result) string {
	var text strings.Builder
	for _, finding := range result.Findings() {
		text.WriteString(finding.Check)
		text.WriteByte(' ')
		text.WriteString(finding.Message)
		text.WriteByte('\n')
	}
	return text.String()
}

func newGitRepository(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", repo, err)
	}
	git(t, root, repo, "init", "--quiet")
	return root, repo
}

func git(t *testing.T, root, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	command.Env = append(os.Environ(),
		"HOME="+root,
		"XDG_CONFIG_HOME="+filepath.Join(root, "xdg-config"),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v error = %v\n%s", args, err, output)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}

func manifestOptions(t *testing.T, root, repo, version string) ManifestOptions {
	t.Helper()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", home, err)
	}
	return ManifestOptions{
		Repository: repo,
		Version:    version,
		Home:       home,
		Config:     filepath.Join(home, ".config", "dot", "config.toml"),
		GOOS:       runtime.GOOS,
	}
}
