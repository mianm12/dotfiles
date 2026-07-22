package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
)

func TestParseInitSetValues_PreservesEmptyAndRejectsAmbiguity(t *testing.T) {
	values, err := parseInitSetValues([]string{"email=me@example.com", "empty="})
	if err != nil {
		t.Fatalf("parseInitSetValues() error = %v", err)
	}
	if got := values["email"]; !got.Set || got.Value != "me@example.com" {
		t.Fatalf("email selection = %#v", got)
	}
	if got := values["empty"]; !got.Set || got.Value != "" {
		t.Fatalf("empty selection = %#v, want explicit empty", got)
	}

	for _, test := range []struct {
		name   string
		values []string
		want   string
	}{
		{name: "missing equals", values: []string{"email"}, want: "want key=value"},
		{name: "empty key", values: []string{"=value"}, want: "want key=value"},
		{name: "duplicate", values: []string{"email=first", "email=second"}, want: "duplicate --set key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseInitSetValues(test.values); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseInitSetValues(%q) error = %v, want %q", test.values, err, test.want)
			}
		})
	}
}

func TestResolveInitDecisions_YesUsesUnambiguousInputsWithoutTerminal(t *testing.T) {
	fixture := newInitDecisionFixture(t, `requires = ">=0.0.0"
[profiles]
mac = []
[data.email]
default = "default@example.com"
[data.empty]
default = "fallback"
`, "")
	inputs := fixture.prepare(t, dotruntime.Override{Value: "mac", Set: true})
	before := snapshotCLITree(t, fixture.root)
	opened := false
	decisions, err := resolveInitDecisions(
		inputs,
		map[string]dotruntime.Override{"empty": {Value: "", Set: true}},
		true,
		func() (io.ReadWriteCloser, error) {
			opened = true
			return nil, errors.New("must not open")
		},
	)
	if err != nil {
		t.Fatalf("resolveInitDecisions() error = %v", err)
	}
	if opened {
		t.Fatal("--yes with unambiguous values opened a user terminal")
	}
	if !decisions.apply {
		t.Fatal("--yes did not select immediate apply")
	}
	candidate, err := inputs.BuildCandidate(decisions.selection)
	if err != nil {
		t.Fatalf("BuildCandidate() error = %v", err)
	}
	machine := candidate.Machine()
	if machine.Profile != "mac" || machine.Data["email"] != "default@example.com" || machine.Data["empty"] != "" {
		t.Fatalf("candidate machine = %#v", machine)
	}
	fixture.assertUnchanged(t, before)
}

func TestResolveInitDecisions_NoTerminalLeavesAllMutationPathsMissing(t *testing.T) {
	fixture := newInitDecisionFixture(t, `requires = ">=0.0.0"
[profiles]
linux = []
mac = []
[data.email]
prompt = "Git email"
`, "")
	inputs := fixture.prepare(t, dotruntime.Override{})
	before := snapshotCLITree(t, fixture.root)
	_, err := resolveInitDecisions(inputs, nil, true, func() (io.ReadWriteCloser, error) {
		return nil, os.ErrNotExist
	})
	if err == nil || !strings.Contains(err.Error(), "open user terminal") {
		t.Fatalf("resolveInitDecisions() error = %v, want no terminal", err)
	}
	fixture.assertUnchanged(t, before)
	for _, path := range []string{fixture.config, fixture.state, fixture.lock, fixture.backup} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("mutation path %q exists or cannot be inspected: %v", path, err)
		}
	}
}

func TestResolveInitDecisions_InteractiveUsesTTYAndRepairsStaleProfile(t *testing.T) {
	fixture := newInitDecisionFixture(t, `requires = ">=0.0.0"
[profiles]
linux = []
mac = []
[data.email]
prompt = "Git email"
default = "manifest@example.com"
[data.machine]
`, `profile = "retired"

[data]
email = "old@example.com"
machine = "old-machine"
`)
	inputs := fixture.prepare(t, dotruntime.Override{})
	before := snapshotCLITree(t, fixture.root)
	terminal := newInitTestTerminal("linux\n\nnew-machine\nn\n")
	decisions, err := resolveInitDecisions(inputs, nil, false, func() (io.ReadWriteCloser, error) {
		return terminal, nil
	})
	if err != nil {
		t.Fatalf("resolveInitDecisions() error = %v", err)
	}
	if decisions.apply {
		t.Fatal("interactive no answer selected apply")
	}
	candidate, err := inputs.BuildCandidate(decisions.selection)
	if err != nil {
		t.Fatalf("BuildCandidate() error = %v", err)
	}
	machine := candidate.Machine()
	if machine.Profile != "linux" || machine.Data["email"] != "old@example.com" || machine.Data["machine"] != "new-machine" {
		t.Fatalf("candidate machine = %#v", machine)
	}
	for _, want := range []string{"Profiles:\n  linux\n  mac\n", "Profile: ", "Git email [old@example.com]: ", "machine [old-machine]: ", "Apply now? [Y/n] "} {
		if !strings.Contains(terminal.written.String(), want) {
			t.Fatalf("terminal output = %q, want %q", terminal.written.String(), want)
		}
	}
	if !terminal.closed {
		t.Fatal("init terminal was not closed")
	}
	fixture.assertUnchanged(t, before)
}

func TestResolveInitDecisions_RejectsUndeclaredSetBeforeOpeningTerminal(t *testing.T) {
	fixture := newInitDecisionFixture(t, `requires = ">=0.0.0"
[profiles]
mac = []
`, "")
	inputs := fixture.prepare(t, dotruntime.Override{Value: "mac", Set: true})
	opened := false
	_, err := resolveInitDecisions(
		inputs,
		map[string]dotruntime.Override{"unknown": {Value: "value", Set: true}},
		false,
		func() (io.ReadWriteCloser, error) {
			opened = true
			return nil, errors.New("must not open")
		},
	)
	if err == nil || !strings.Contains(err.Error(), "unknown init data key") {
		t.Fatalf("resolveInitDecisions() error = %v", err)
	}
	if opened {
		t.Fatal("unknown --set opened a user terminal")
	}
}

type initDecisionFixture struct {
	root     string
	home     string
	realHome string
	repo     string
	config   string
	state    string
	lock     string
	backup   string
}

func newInitDecisionFixture(t *testing.T, manifest, machine string) initDecisionFixture {
	t.Helper()
	root := t.TempDir()
	fixture := initDecisionFixture{
		root:     root,
		home:     filepath.Join(root, "synthetic-home"),
		realHome: filepath.Join(root, "real-home-sentinel"),
		repo:     filepath.Join(root, "repo"),
	}
	fixture.config = filepath.Join(fixture.home, ".config", "dot", "config.toml")
	fixture.state = filepath.Join(fixture.home, ".local", "state", "dot", "state.json")
	fixture.lock = filepath.Join(fixture.home, ".local", "state", "dot", "lock")
	fixture.backup = filepath.Join(fixture.home, ".local", "state", "dot", "backup")
	writeCLIFile(t, filepath.Join(fixture.repo, "dot.toml"), manifest)
	writeCLIFile(t, filepath.Join(fixture.realHome, "sentinel"), "unchanged\n")
	if machine != "" {
		writeCLIFile(t, fixture.config, machine)
		if err := os.Chmod(fixture.config, 0o600); err != nil {
			t.Fatalf("os.Chmod(config) error = %v", err)
		}
	}
	t.Setenv("HOME", fixture.realHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fixture.home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(fixture.home, ".local", "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(fixture.home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(fixture.home, ".cache"))
	t.Setenv("DOT_CONFIG", fixture.config)
	t.Setenv("DOT_REPO", fixture.repo)
	return fixture
}

func (fixture initDecisionFixture) prepare(t *testing.T, profile dotruntime.Override) dotruntime.InitInputs {
	t.Helper()
	inputs, err := dotruntime.PrepareInit(dotruntime.Overrides{
		Home:       dotruntime.Override{Value: fixture.home, Set: true},
		Repository: dotruntime.Override{Value: fixture.repo, Set: true},
		Profile:    profile,
	}, "v0.0.0")
	if err != nil {
		t.Fatalf("PrepareInit() error = %v", err)
	}
	return inputs
}

func (fixture initDecisionFixture) assertUnchanged(t *testing.T, before map[string]cliTreeEntry) {
	t.Helper()
	if after := snapshotCLITree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("init decision phase changed synthetic tree\nbefore=%v\nafter=%v", before, after)
	}
	content, err := os.ReadFile(filepath.Join(fixture.realHome, "sentinel"))
	if err != nil || string(content) != "unchanged\n" {
		t.Fatalf("real HOME sentinel = %q, %v", content, err)
	}
}

type initTestTerminal struct {
	input   *strings.Reader
	written bytes.Buffer
	closed  bool
}

func newInitTestTerminal(input string) *initTestTerminal {
	return &initTestTerminal{input: strings.NewReader(input)}
}

func (terminal *initTestTerminal) Read(buffer []byte) (int, error) {
	return terminal.input.Read(buffer)
}

func (terminal *initTestTerminal) Write(buffer []byte) (int, error) {
	return terminal.written.Write(buffer)
}

func (terminal *initTestTerminal) Close() error {
	terminal.closed = true
	return nil
}
