package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/buildinfo"
)

func TestDoctor_ManifestOnlyIsReadOnlyWithMissingMachineFiles(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	configPath := filepath.Join(home, "custom", "config.toml")
	makeDirectory(t, home)
	writeDoctorRepository(t, root, repo, `requires = ">=0.1.0"
[profiles]
mac = []
`)
	before := snapshotCLITree(t, root)

	stdout, stderr, code := runDoctorForTest(
		t,
		[]string{"doctor", "--manifest-only"},
		home,
		map[string]string{"DOT_REPO": repo, "DOT_CONFIG": configPath},
		buildinfo.Info{Version: "v0.1.0"},
	)
	if code != exitOK || stdout != "Manifest check passed.\n" || stderr != "" {
		t.Fatalf("doctor = stdout %q, stderr %q, exit %d; want clean", stdout, stderr, code)
	}
	if after := snapshotCLITree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("doctor changed isolated tree\nbefore=%v\nafter=%v", before, after)
	}

	for _, path := range []string{
		configPath,
		filepath.Join(home, ".local", "state", "dot"),
		filepath.Join(home, ".local", "state", "dot", "state.json"),
		filepath.Join(home, ".local", "state", "dot", "lock"),
		filepath.Join(home, ".local", "state", "dot", "backup"),
		filepath.Join(home, ".local", "bin", "dot"),
	} {
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("os.Lstat(%q) error = %v, want missing", path, err)
		}
	}
}

func TestDoctor_ManifestOnlyIgnoresInvalidMachineConfigAndState(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(home, ".local", "share", "dot", "repo")
	makeDirectory(t, home)
	writeDoctorRepository(t, root, repo, `requires = ">=0.1.0"
[profiles]
mac = []
`)
	writeCLIFile(t, filepath.Join(home, ".config", "dot", "config.toml"), "invalid = [")
	writeCLIFile(t, filepath.Join(home, ".local", "state", "dot", "state.json"), "not-json")
	writeCLIFile(t, filepath.Join(home, ".local", "state", "dot", "lock"), "not-a-lock-record")
	makeDirectory(t, filepath.Join(home, ".local", "state", "dot", "backup"))
	writeCLIFile(t, filepath.Join(home, ".local", "bin", "dot"), "not-a-binary")
	before := snapshotCLITree(t, root)

	stdout, stderr, code := runDoctorForTest(
		t,
		[]string{"doctor", "--manifest-only"},
		home,
		nil,
		buildinfo.Info{Version: "v0.1.0"},
	)
	if code != exitOK || stdout != "Manifest check passed.\n" || stderr != "" {
		t.Fatalf("doctor = stdout %q, stderr %q, exit %d; want invalid machine files ignored", stdout, stderr, code)
	}
	if after := snapshotCLITree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("doctor changed isolated tree\nbefore=%v\nafter=%v", before, after)
	}
}

func TestDoctor_ManifestOnlyPrintsStableFindings(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	makeDirectory(t, home)
	writeDoctorRepository(t, root, repo, `requires = "^1.0.0"
unknown = true
[profiles]
mac = []
`)
	writeCLIFile(t, filepath.Join(repo, "tracked.local"), "private")
	runGit(t, root, repo, "add", "dot.toml", "tracked.local")

	stdout, stderr, code := runDoctorForTest(
		t,
		[]string{"doctor", "--manifest-only", "--repo", repo},
		home,
		nil,
		buildinfo.Info{Version: "v0.1.0"},
	)
	if code != exitError || stdout != "" {
		t.Fatalf("doctor = stdout %q, stderr %q, exit %d; want findings exit 1", stdout, stderr, code)
	}
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	wantPrefixes := []string{
		"error [git.tracked-local]:",
		"error [manifest.load]:",
		"error [manifest.requires]:",
	}
	if len(lines) != len(wantPrefixes) {
		t.Fatalf("doctor stderr lines = %q, want %d findings", lines, len(wantPrefixes))
	}
	for index, prefix := range wantPrefixes {
		if !strings.HasPrefix(lines[index], prefix) {
			t.Errorf("doctor stderr[%d] = %q, want prefix %q", index, lines[index], prefix)
		}
	}
}

func TestDoctor_DevelopmentNoticeDoesNotChangeExitCode(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	makeDirectory(t, home)
	writeDoctorRepository(t, root, repo, `requires = ">=999.0.0"
[profiles]
mac = []
`)

	stdout, stderr, code := runDoctorForTest(
		t,
		[]string{"doctor", "--manifest-only", "--repo", repo},
		home,
		nil,
		buildinfo.Info{Version: "dev"},
	)
	if code != exitOK || stdout != "Manifest check passed.\n" {
		t.Fatalf("doctor = stdout %q, stderr %q, exit %d; want clean development result", stdout, stderr, code)
	}
	if want := "notice: development build skipped the requires version comparison\n"; stderr != want {
		t.Fatalf("doctor stderr = %q, want %q", stderr, want)
	}
}

func TestDoctor_ProfileFlagNarrowsOnlyProfileBoundaries(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	makeDirectory(t, home)
	writeDoctorRepository(t, root, repo, `requires = ">=0.1.0"
[profiles]
good = []
bad = ["left", "right"]
`)
	for _, module := range []string{"left", "right"} {
		writeCLIFile(t, filepath.Join(repo, "modules", module, "dot.toml"), `target = "~/collision"`)
		writeCLIFile(t, filepath.Join(repo, "modules", module, "file"), module)
	}

	_, allStderr, allCode := runDoctorForTest(
		t,
		[]string{"doctor", "--manifest-only", "--repo", repo},
		home,
		nil,
		buildinfo.Info{Version: "v0.1.0"},
	)
	if allCode != exitError || !strings.Contains(allStderr, "error [manifest.profile]") {
		t.Fatalf("all profiles = stderr %q, exit %d; want bad-profile error", allStderr, allCode)
	}

	stdout, stderr, code := runDoctorForTest(
		t,
		[]string{"doctor", "--manifest-only", "--repo", repo, "--profile", "good"},
		home,
		nil,
		buildinfo.Info{Version: "v0.1.0"},
	)
	if code != exitOK || stdout != "Manifest check passed.\n" || stderr != "" {
		t.Fatalf("selected profile = stdout %q, stderr %q, exit %d; want clean", stdout, stderr, code)
	}
}

func TestDoctor_NakedCommandExplainsM2Boundary(t *testing.T) {
	stdout, stderr, code := runForTest(t, []string{"doctor"}, nil, buildinfo.Info{})
	if code != exitError || stdout != "" {
		t.Fatalf("doctor = stdout %q, stderr %q, exit %d; want error", stdout, stderr, code)
	}
	for _, want := range []string{"full doctor requires M2", "dot doctor --manifest-only"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("doctor stderr = %q, want containing %q", stderr, want)
		}
	}
}

func TestDoctor_OutputFailuresOverrideDiagnosticResult(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	makeDirectory(t, home)
	writeDoctorRepository(t, root, repo, `requires = ">=999.0.0"
[profiles]
mac = []
`)

	t.Run("stdout", func(t *testing.T) {
		var stderr bytes.Buffer
		code := run([]string{"doctor", "--manifest-only", "--repo", repo}, environment{
			stdout: failingWriter{err: errors.New("broken stdout")},
			stderr: &stderr,
			lookupEnv: func(string) (string, bool) {
				return "", false
			},
			userHomeDir: func() (string, error) { return home, nil },
			build:       buildinfo.Info{Version: "v999.0.0"},
			goos:        runtime.GOOS,
		})
		if code != exitError || !strings.Contains(stderr.String(), "write stdout: broken stdout") {
			t.Fatalf("doctor stdout failure = stderr %q, exit %d; want write error", stderr.String(), code)
		}
	})

	t.Run("stderr", func(t *testing.T) {
		var stdout bytes.Buffer
		code := run([]string{"doctor", "--manifest-only", "--repo", repo}, environment{
			stdout: &stdout,
			stderr: failingWriter{err: errors.New("broken stderr")},
			lookupEnv: func(string) (string, bool) {
				return "", false
			},
			userHomeDir: func() (string, error) { return home, nil },
			build:       buildinfo.Info{Version: "dev"},
			goos:        runtime.GOOS,
		})
		if code != exitError || stdout.String() != "Manifest check passed.\n" {
			t.Fatalf("doctor stderr failure = stdout %q, exit %d; want output error", stdout.String(), code)
		}
	})
}

type cliTreeEntry struct {
	mode fs.FileMode
	data string
}

func snapshotCLITree(t *testing.T, root string) map[string]cliTreeEntry {
	t.Helper()
	snapshot := make(map[string]cliTreeEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		data := ""
		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			data = string(content)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snapshot[relative] = cliTreeEntry{mode: info.Mode(), data: data}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot tree %q: %v", root, err)
	}
	return snapshot
}

func runDoctorForTest(
	t *testing.T,
	args []string,
	home string,
	envVars map[string]string,
	build buildinfo.Info,
) (string, string, int) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(args, environment{
		stdout: &stdout,
		stderr: &stderr,
		lookupEnv: func(name string) (string, bool) {
			value, exists := envVars[name]
			return value, exists
		},
		userHomeDir: func() (string, error) { return home, nil },
		build:       build,
		goos:        runtime.GOOS,
	})
	return stdout.String(), stderr.String(), code
}

func writeDoctorRepository(t *testing.T, root, repo, manifest string) {
	t.Helper()
	makeDirectory(t, repo)
	runGit(t, root, repo, "init", "--quiet")
	writeCLIFile(t, filepath.Join(repo, "dot.toml"), manifest)
}

func runGit(t *testing.T, root, repo string, args ...string) {
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

func makeDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", path, err)
	}
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	makeDirectory(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
