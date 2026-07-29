package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestStatusUsesCurrentSelectionWhileApplyDryRunUsesProspectiveSelection(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "portable"})
	fixture.writeMachine(t, []string{"base"}, nil)
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.runInjected("status", "extra")
	if code != exitOK ||
		stdout != "extra  inactive\n" ||
		strings.Contains(stdout, "add-extra") ||
		strings.Contains(stdout, "create-link") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf("status extra = (%d, %q, %q), want current inventory", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.runInjected("apply", "extra", "--dry-run")
	if code != exitOK ||
		!strings.Contains(stdout, "selection-delta add-extra module=extra") ||
		!strings.Contains(stdout, "create-link") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"apply extra --dry-run = (%d, %q, %q), want prospective analysis",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
	if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
		t.Fatalf("extra_modules = %v, want unchanged", extras)
	}
}

func TestInitializedInitDryRunKeepsGeneralMissingStateWarning(t *testing.T) {
	fixture := newCLITestEnv(t, "")
	fixture.writeMachine(t, nil, nil)
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.runInjected(
		"init",
		fixture.repository,
		"--dry-run",
	)

	if code != exitError ||
		!strings.Contains(stdout, "machine is already initialized") ||
		stderr != "warning: "+state.MissingWarning+"\n" ||
		strings.Contains(stderr, "expected on first init") {
		t.Fatalf(
			"initialized init --dry-run = (%d, %q, %q), want blocker and general missing-state warning",
			code,
			stdout,
			stderr,
		)
	}
	assertSnapshotUnchanged(t, before)
}

func TestOperationAnalysisTreatsMachineSelectionsAsSets(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
	fixture.writeMachine(
		t,
		[]string{"base", "base"},
		[]string{"app", "app"},
	)
	context, err := resolveContext(fixture.env)
	if err != nil {
		t.Fatalf("resolveContext() error = %v", err)
	}

	analysis, err := analyzeStatus(context, fixture.loadMachine(t), nil)
	if err != nil {
		t.Fatalf("analyzeStatus() error = %v", err)
	}
	if len(analysis.Modules) != 1 ||
		analysis.Modules[0].ID != "app" ||
		analysis.Modules[0].Selection != "profile+extra" ||
		len(analysis.Actions) != 1 ||
		analysis.Actions[0].ModuleID != "app" ||
		analysis.Actions[0].PlacementID != "config" {
		t.Fatalf(
			"analysis = %#v, actions = %#v; want one profile+extra app action",
			analysis.Modules,
			analysis.Actions,
		)
	}
}

func TestScopedMutationAnalysisProjectsOnlyScopeAndModuleBlockers(t *testing.T) {
	t.Run("successful scope", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["base"]`)
		fixture.writeModule(t, "base", "", nil)
		fixture.writeModule(t, "extra", "", nil)
		fixture.writeMachine(t, []string{"base"}, nil)
		context, err := resolveContext(fixture.env)
		if err != nil {
			t.Fatalf("resolveContext() error = %v", err)
		}
		moduleID := "extra"

		analysis, err := analyzeApply(
			context,
			fixture.loadMachine(t),
			&moduleID,
		)
		if err != nil {
			t.Fatalf("analyzeApply() error = %v", err)
		}
		if len(analysis.Modules) != 1 ||
			analysis.Modules[0].ID != "extra" ||
			analysis.Modules[0].Convergence != "converged" {
			t.Fatalf(
				"scoped modules = %#v, want only converged extra",
				analysis.Modules,
			)
		}
	})

	t.Run("unrelated module blocker", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "extra", "", nil)
		fixture.writeMachine(t, []string{"base"}, []string{"gone"})
		context, err := resolveContext(fixture.env)
		if err != nil {
			t.Fatalf("resolveContext() error = %v", err)
		}
		moduleID := "extra"

		analysis, err := analyzeApply(
			context,
			fixture.loadMachine(t),
			&moduleID,
		)
		if err != nil {
			t.Fatalf("analyzeApply() error = %v", err)
		}
		if len(analysis.Modules) != 2 ||
			analysis.Modules[0].ID != "extra" ||
			analysis.Modules[0].Summary != "pending" ||
			analysis.Modules[0].Convergence != "-" ||
			analysis.Modules[1].ID != "gone" ||
			analysis.Modules[1].Summary != "conflict" ||
			analysis.Modules[1].Convergence != "-" {
			t.Fatalf(
				"blocked scoped modules = %#v, want unknown scope plus blocker",
				analysis.Modules,
			)
		}
	})
}

func TestSelectionOnlyDryRunsRenderDelta(t *testing.T) {
	t.Run("create selection", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.runInjected(
			"init",
			fixture.repository,
			"--profile",
			"base",
			"--dry-run",
		)

		assertSelectionOnlyAnalysis(
			t,
			code,
			stdout,
			stderr,
			"selection-delta create",
		)
		assertSnapshotUnchanged(t, before)
		assertCLIMissing(t, fixture.config)
	})

	t.Run("add extra", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "empty", "", nil)
		fixture.writeMachine(t, []string{"base"}, nil)
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.runInjected(
			"apply",
			"empty",
			"--dry-run",
		)

		assertSelectionOnlyAnalysis(
			t,
			code,
			stdout,
			stderr,
			"selection-delta add-extra module=empty",
		)
		assertSnapshotUnchanged(t, before)
	})

	t.Run("remove extra", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "empty", "", nil)
		fixture.writeMachine(t, []string{"base"}, []string{"empty"})
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.runInjected(
			"remove",
			"empty",
			"--dry-run",
		)

		assertSelectionOnlyAnalysis(
			t,
			code,
			stdout,
			stderr,
			"selection-delta remove-extra module=empty",
		)
		assertSnapshotUnchanged(t, before)
	})
}

func TestStatusRendersSelectionSources(t *testing.T) {
	tests := []struct {
		name     string
		profiles string
		extras   []string
		want     string
	}{
		{
			name:     "profile",
			profiles: `base = ["app"]`,
			want:     "selection=profile",
		},
		{
			name:     "extra",
			profiles: `base = []`,
			extras:   []string{"app"},
			want:     "selection=extra",
		},
		{
			name:     "profile and extra",
			profiles: `base = ["app"]`,
			extras:   []string{"app"},
			want:     "selection=profile+extra",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLITestEnv(t, test.profiles)
			fixture.writeModule(t, "app", "", nil)
			fixture.writeMachine(t, []string{"base"}, test.extras)

			code, stdout, _ := fixture.runInjected("status", "app")

			if code != exitOK ||
				stdout != "app  converged "+test.want+"\n" {
				t.Fatalf("status source = (%d, %q), want %s", code, stdout, test.want)
			}
		})
	}
}

func TestDryRunRendersCompleteBlockersWithFailureExit(t *testing.T) {
	t.Run("placement conflict", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
		fixture.writeMachine(t, []string{"base"}, nil)
		writeCLIFile(t, filepath.Join(fixture.home, ".app"), "personal")
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.runInjected("apply", "--dry-run")

		if code != exitError ||
			!strings.Contains(stdout, "conflict") ||
			!strings.Contains(stdout, `reason="actual target is regular file"`) ||
			!strings.Contains(stderr, "state is missing") ||
			strings.Contains(stderr, "error:") {
			t.Fatalf("conflict dry-run = (%d, %q, %q)", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
	})

	t.Run("not applicable explicit module", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "gated", `
[match]
os = ["macos"]
`, nil)
		fixture.writeMachine(t, []string{"base"}, nil)
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.runInjected(
			"apply",
			"gated",
			"--dry-run",
		)

		if code != exitError ||
			!strings.Contains(stdout, "selection-delta add-extra module=gated") ||
			!strings.Contains(stdout, "blocked module=gated") ||
			!strings.Contains(stdout, "not applicable") ||
			!strings.Contains(stderr, "state is missing") ||
			strings.Contains(stderr, "error:") {
			t.Fatalf("not-applicable dry-run = (%d, %q, %q)", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
	})

	t.Run("profile selected remove", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["app"]`)
		fixture.writeModule(t, "app", "", nil)
		fixture.writeMachine(t, []string{"base"}, nil)
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.runInjected(
			"remove",
			"app",
			"--dry-run",
		)

		if code != exitError ||
			!strings.Contains(stdout, "blocked module=app") ||
			!strings.Contains(stdout, "selected by an active profile") ||
			!strings.Contains(stderr, "state is missing") ||
			strings.Contains(stderr, "error:") {
			t.Fatalf("profile remove dry-run = (%d, %q, %q)", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
	})

	t.Run("unknown explicit module", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeMachine(t, []string{"base"}, nil)
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.runInjected(
			"apply",
			"missing",
			"--dry-run",
		)

		if code != exitError ||
			!strings.Contains(stdout, "selection-delta add-extra module=missing") ||
			!strings.Contains(stdout, "blocked module=missing") ||
			!strings.Contains(stdout, "does not exist") ||
			!strings.Contains(stderr, "state is missing") ||
			strings.Contains(stderr, "error:") {
			t.Fatalf("unknown dry-run = (%d, %q, %q)", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)

		code, stdout, stderr = fixture.runInjected("status", "missing")
		if code != exitOK ||
			!strings.Contains(
				stdout,
				`missing  inactive reason="unknown module \"missing\""`,
			) ||
			!strings.Contains(stdout, "blocked module=missing") ||
			!strings.Contains(stdout, "unknown module") ||
			!strings.Contains(stderr, "state is missing") {
			t.Fatalf("unknown status = (%d, %q, %q)", code, stdout, stderr)
		}
		assertSnapshotUnchanged(t, before)
	})
}

func TestStatusShowsPendingCleanupForNotApplicableProfileModule(t *testing.T) {
	fixture := newCLITestEnv(t, `base = ["app"]`)
	fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
	fixture.writeMachine(t, []string{"base"}, nil)
	code, _, stderr := fixture.runInjected("apply")
	if code != exitOK {
		t.Fatalf("initial apply = (%d, %q)", code, stderr)
	}

	writeCLIFile(
		t,
		filepath.Join(fixture.repository, "modules", "app", "module.toml"),
		`
[match]
os = ["macos"]

[[links]]
id = "config"
source = "config"
target = "~/.app"
`,
	)
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.runInjected("status", "app")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(
			stdout,
			"app  not-applicable selection=profile "+
				`convergence=pending-cleanup reason="prune"`,
		) {
		t.Fatalf("status pending cleanup = (%d, %q, %q)", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.runInjected("apply", "--dry-run")
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "prune") {
		t.Fatalf("cleanup dry-run = (%d, %q, %q)", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.runInjected("apply")
	if code != exitOK ||
		stderr != "" ||
		!strings.Contains(stdout, "prune") ||
		!strings.Contains(stdout, "targets_changed=true state_changed=true") {
		t.Fatalf("cleanup apply = (%d, %q, %q)", code, stdout, stderr)
	}
	assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
	if modules := loadTestState(t, fixture).Modules; len(modules) != 0 {
		t.Fatalf("state modules = %#v, want cleanup complete", modules)
	}
	assertApplyNoMutation(t, fixture, fixture.runInjected)
}

func TestStatusDoesNotClaimConvergenceWhenPlanningIsBlocked(t *testing.T) {
	for _, withState := range []bool{false, true} {
		name := "without state"
		if withState {
			name = "with state"
		}
		t.Run("selected missing module "+name, func(t *testing.T) {
			fixture := newCLITestEnv(t, `base = []`)
			fixture.writeMachine(t, []string{"base"}, []string{"gone"})
			if withState {
				fixture.writeState(t, state.Snapshot{
					Home: fixture.home,
					Modules: map[string]state.Module{
						"gone": {Placements: map[string]state.Placement{
							"local": {
								Kind:   state.KindLocal,
								Target: filepath.Join(fixture.home, ".gone"),
							},
						}},
					},
				})
			}
			before := snapshotTree(t, fixture.root)

			code, stdout, _ := fixture.runInjected("status", "gone")

			if code != exitOK ||
				!strings.Contains(
					stdout,
					"gone  conflict selection=extra ",
				) ||
				!strings.Contains(
					stdout,
					`reason="required module \"gone\" does not exist"`,
				) ||
				!strings.Contains(stdout, "blocked module=gone") ||
				strings.Contains(stdout, "gone  converged") {
				t.Fatalf(
					"status missing selected module = (%d, %q), want blocked unknown convergence",
					code,
					stdout,
				)
			}
			assertSnapshotUnchanged(t, before)
		})
	}

	t.Run("global control topology blocker", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.repository = filepath.Join(
			filepath.Dir(fixture.config),
			"repository",
		)
		writeCLIFile(
			t,
			filepath.Join(fixture.repository, "dot.toml"),
			"version = 1\n[profiles]\nbase = [\"app\"]\n",
		)
		fixture.writeModule(t, "app", `
[[links]]
id = "config"
source = "config"
target = "~/.app"
`, map[string]string{"config": "portable"})
		fixture.writeMachine(t, []string{"base"}, nil)
		before := snapshotTree(t, fixture.root)

		code, stdout, _ := fixture.runInjected("status", "app")

		if code != exitOK ||
			!strings.Contains(
				stdout,
				"app  conflict selection=profile reason=",
			) ||
			!strings.Contains(stdout, `reason="control paths conflict:`) ||
			!strings.Contains(stdout, "blocked") ||
			strings.Contains(stdout, "app  converged") {
			t.Fatalf(
				"status topology blocker = (%d, %q), want blocked unknown convergence",
				code,
				stdout,
			)
		}
		assertSnapshotUnchanged(t, before)
		assertCLIMissing(t, filepath.Join(fixture.home, ".app"))
	})
}

func TestMutationReanalyzesInputsInsideLock(t *testing.T) {
	t.Run("init", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.env.afterPreflight = func() {
			corruptRootManifest(t, fixture)
		}

		code, stdout, stderr := fixture.runInjected(
			"init",
			fixture.repository,
			"--profile",
			"base",
		)

		assertLockedReanalysisFailure(t, code, stdout, stderr)
		assertCLIMissing(t, fixture.config)
		assertCLIMissing(t, fixture.state)
	})

	t.Run("apply", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "portable"})
		fixture.writeMachine(t, []string{"base"}, nil)
		fixture.env.afterPreflight = func() {
			corruptRootManifest(t, fixture)
		}

		code, stdout, stderr := fixture.runInjected("apply", "extra")

		assertLockedReanalysisFailure(t, code, stdout, stderr)
		if extras := fixture.loadMachine(t).ExtraModules; len(extras) != 0 {
			t.Fatalf("extra_modules = %v, want unchanged", extras)
		}
		assertCLIMissing(t, fixture.state)
		assertCLIMissing(t, filepath.Join(fixture.home, ".extra"))
	})

	t.Run("remove", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "extra", "", nil)
		fixture.writeMachine(t, []string{"base"}, []string{"extra"})
		fixture.env.afterPreflight = func() {
			corruptRootManifest(t, fixture)
		}

		code, stdout, stderr := fixture.runInjected("remove", "extra")

		assertLockedReanalysisFailure(t, code, stdout, stderr)
		extras := fixture.loadMachine(t).ExtraModules
		if len(extras) != 1 || extras[0] != "extra" {
			t.Fatalf("extra_modules = %v, want unchanged [extra]", extras)
		}
		assertCLIMissing(t, fixture.state)
	})

	t.Run("repository drift", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "old"})
		fixture.writeMachine(t, []string{"base"}, nil)

		otherRepository := filepath.Join(fixture.root, "other-repository")
		writeCLIFile(
			t,
			filepath.Join(otherRepository, "dot.toml"),
			"version = 1\n[profiles]\nbase = []\n",
		)
		writeCLIFile(
			t,
			filepath.Join(otherRepository, "modules", "extra", "module.toml"),
			"[[links]]\nid = \"config\"\nsource = \"config\"\ntarget = \"~/.extra\"\n",
		)
		writeCLIFile(
			t,
			filepath.Join(otherRepository, "modules", "extra", "config"),
			"new",
		)
		fixture.env.afterPreflight = func() {
			if _, err := config.PublishMachine(fixture.config, config.Machine{
				Version:      1,
				Repository:   otherRepository,
				Profiles:     []string{"base"},
				ExtraModules: []string{},
			}); err != nil {
				t.Fatalf("PublishMachine(repository drift) error = %v", err)
			}
		}

		code, stdout, stderr := fixture.runInjected("apply", "extra")

		if code != exitError ||
			stdout != "" ||
			!strings.Contains(stderr, "does not match mutation session repository") {
			t.Fatalf(
				"apply after repository drift = (%d, %q, %q), want fixed-session failure",
				code,
				stdout,
				stderr,
			)
		}
		machine := fixture.loadMachine(t)
		if machine.Repository != otherRepository || len(machine.ExtraModules) != 0 {
			t.Fatalf("machine after repository drift = %#v, want external edit only", machine)
		}
		assertCLIMissing(t, fixture.state)
		assertCLIMissing(t, filepath.Join(fixture.home, ".extra"))
	})
}

func assertSelectionOnlyAnalysis(
	t *testing.T,
	code int,
	stdout, stderr, delta string,
) {
	t.Helper()
	if code != exitOK ||
		!strings.Contains(stdout, delta) ||
		strings.Contains(stdout, "converged") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"selection-only analysis = (%d, %q, %q), want %q",
			code,
			stdout,
			stderr,
			delta,
		)
	}
}

func corruptRootManifest(t *testing.T, fixture *cliTestEnv) {
	t.Helper()
	path := filepath.Join(fixture.repository, "dot.toml")
	if err := os.WriteFile(path, []byte("version = [\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(corrupt root manifest) error = %v", err)
	}
}

func assertLockedReanalysisFailure(
	t *testing.T,
	code int,
	stdout, stderr string,
) {
	t.Helper()
	if code != exitError ||
		stdout != "" ||
		!strings.Contains(stderr, "root manifest") {
		t.Fatalf(
			"locked reanalysis = (%d, %q, %q), want fatal refreshed input",
			code,
			stdout,
			stderr,
		)
	}
}
