package planner

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/manifest"
)

func TestPlanHook_BuildsSelfContainedActionWithoutExecution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mode        os.FileMode
		wantMode    HookExecutionMode
		wantProgram string
		wantArgs    func(string) []string
	}{
		{
			name:        "non executable uses sh",
			mode:        0o644,
			wantMode:    HookExecutionShell,
			wantProgram: "sh",
			wantArgs:    func(script string) []string { return []string{script} },
		},
		{
			name:        "user executable runs directly",
			mode:        0o744,
			wantMode:    HookExecutionDirect,
			wantProgram: "",
			wantArgs:    func(string) []string { return nil },
		},
		{
			name:        "group executable runs directly",
			mode:        0o650,
			wantMode:    HookExecutionDirect,
			wantProgram: "",
			wantArgs:    func(string) []string { return nil },
		},
		{
			name:        "other executable runs directly",
			mode:        0o601,
			wantMode:    HookExecutionDirect,
			wantProgram: "",
			wantArgs:    func(string) []string { return nil },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			home := filepath.Join(root, "home")
			repository := filepath.Join(root, "repo")
			modulePath := filepath.Join(repository, "modules", "alpha")
			scriptPath := filepath.Join(modulePath, "hooks", "setup.sh")
			marker := filepath.Join(root, "must-not-exist")
			if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
				t.Fatalf("MkdirAll(script parent) error = %v", err)
			}
			content := []byte("#!/bin/sh\ntouch " + marker + "\n")
			if err := os.WriteFile(scriptPath, content, test.mode); err != nil {
				t.Fatalf("WriteFile(script) error = %v", err)
			}
			descriptor := manifest.HookDescriptor{
				Module:         "alpha",
				ModulePath:     modulePath,
				Script:         "hooks/setup.sh",
				ScriptPath:     scriptPath,
				TargetRoot:     "~/.config/alpha",
				TargetRootPath: filepath.Join(home, ".config", "alpha"),
			}
			runtime := hookRuntime{
				Profile:    "work",
				GOOS:       "darwin",
				Home:       home,
				Repository: repository,
			}

			action, err := planHook(descriptor, runtime, "", false)
			if err != nil {
				t.Fatalf("planHook() error = %v", err)
			}
			if _, err := os.Lstat(marker); !os.IsNotExist(err) {
				t.Fatalf("planning executed hook or marker check failed: %v", err)
			}
			wantProgram := test.wantProgram
			if wantProgram == "" {
				wantProgram = scriptPath
			}
			want := HookAction{
				Verb:           HookRun,
				StateKey:       "alpha/hooks/setup.sh",
				Module:         "alpha",
				Script:         "hooks/setup.sh",
				ScriptPath:     scriptPath,
				WorkingDir:     modulePath,
				TargetRoot:     "~/.config/alpha",
				TargetRootPath: filepath.Join(home, ".config", "alpha"),
				Profile:        "work",
				GOOS:           "darwin",
				Repository:     repository,
				Invocation: HookInvocation{
					Mode:      test.wantMode,
					Program:   wantProgram,
					Arguments: test.wantArgs(scriptPath),
				},
				Environment: HookEnvironment{
					Home:          home,
					XDGConfigHome: filepath.Join(home, ".config"),
					XDGStateHome:  filepath.Join(home, ".local", "state"),
					XDGDataHome:   filepath.Join(home, ".local", "share"),
					DotModule:     "alpha",
					DotOS:         "darwin",
					DotProfile:    "work",
					DotRepo:       repository,
					DotTarget:     filepath.Join(home, ".config", "alpha"),
				},
				Fingerprint: action.Fingerprint,
				OnSuccess: HookStateEffect{
					Kind:        HookStateUpsert,
					Key:         "alpha/hooks/setup.sh",
					Fingerprint: action.Fingerprint,
				},
				OnFailure: HookStateEffect{Kind: HookStatePreserve},
			}
			if !reflect.DeepEqual(action, want) {
				t.Fatalf("planHook() = %#v, want %#v", action, want)
			}
			if !strings.HasPrefix(action.Fingerprint, "sha256:") || len(action.Fingerprint) != len("sha256:")+64 {
				t.Fatalf("fingerprint = %q, want canonical sha256", action.Fingerprint)
			}
		})
	}
}

func TestHookFingerprint_VersionedGoldenAndInputs(t *testing.T) {
	t.Parallel()

	script := []byte("#!/bin/sh\necho hello\n")
	shell := hookFingerprint(HookExecutionShell, script)
	if want := "sha256:47af394c22dca5efb8c3b5b9971d8442a1363640516202d27523780e00512195"; shell != want {
		t.Fatalf("shell fingerprint = %q, want golden %q", shell, want)
	}
	if direct := hookFingerprint(HookExecutionDirect, script); direct == shell {
		t.Fatal("execution classification did not change fingerprint")
	}
	if changed := hookFingerprint(HookExecutionShell, append(script, '#')); changed == shell {
		t.Fatal("script bytes did not change fingerprint")
	}
	if repeated := hookFingerprint(HookExecutionShell, append([]byte(nil), script...)); repeated != shell {
		t.Fatalf("same inputs produced %q, want %q", repeated, shell)
	}
}

func TestPlanHook_RejectsNonRegularOrMissingScript(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	regular := filepath.Join(root, "regular")
	if err := os.WriteFile(regular, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(regular) error = %v", err)
	}
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatalf("Mkdir(directory) error = %v", err)
	}
	symlink := filepath.Join(root, "symlink")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatalf("Symlink(regular) error = %v", err)
	}

	for _, test := range []struct {
		name string
		path string
	}{
		{name: "missing", path: filepath.Join(root, "missing")},
		{name: "directory", path: directory},
		{name: "symlink", path: symlink},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			descriptor := manifest.HookDescriptor{
				Module:         "alpha",
				ModulePath:     root,
				Script:         "hooks/setup.sh",
				ScriptPath:     test.path,
				TargetRoot:     "~",
				TargetRootPath: root,
			}
			action, err := planHook(descriptor, hookRuntime{
				Profile:    "work",
				GOOS:       "darwin",
				Home:       root,
				Repository: root,
			}, "", false)
			if err == nil {
				t.Fatal("planHook() error = nil, want failure")
			}
			if !reflect.DeepEqual(action, HookAction{}) {
				t.Fatalf("planHook() action = %#v, want zero action", action)
			}
		})
	}
}

func TestHookActionClone_DoesNotShareArguments(t *testing.T) {
	t.Parallel()

	action := HookAction{Invocation: HookInvocation{Arguments: []string{"script"}}}
	cloned := action.Clone()
	cloned.Invocation.Arguments[0] = "changed"
	if action.Invocation.Arguments[0] != "script" {
		t.Fatalf("mutating clone changed source action: %#v", action)
	}
}
