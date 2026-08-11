package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/converge"
	"github.com/mianm12/dotfiles/internal/core/state"
)

func TestStatusAndApplyDryRunUseCurrentSelection(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	fixture.writeModule(t, "extra", `
[[links]]
id = "config"
source = "config"
target = "~/.extra"
`, map[string]string{"config": "portable"})
	fixture.writeMachine(t, []string{"base"}, nil)
	before := snapshotTree(t, fixture.root)

	code, stdout, stderr := fixture.runInjected("status")
	if code != exitOK ||
		stdout != "extra  inactive\n" ||
		strings.Contains(stdout, "add-extra") ||
		strings.Contains(stdout, "create-link") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf("status = (%d, %q, %q), want current inventory", code, stdout, stderr)
	}
	assertSnapshotUnchanged(t, before)

	code, stdout, stderr = fixture.runInjected("apply", "--dry-run")
	if code != exitOK ||
		stdout != "converged\n" ||
		strings.Contains(stdout, "create-link") ||
		!strings.Contains(stderr, "state is missing") {
		t.Fatalf(
			"apply --dry-run = (%d, %q, %q), want empty current selection",
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

	analysis, err := converge.Analyze(context.environment())
	if err != nil {
		t.Fatalf("analyzeStatus() error = %v", err)
	}
	if len(analysis.Modules) != 1 ||
		analysis.Modules[0].ID != "app" ||
		analysis.Modules[0].Selection != "profile+extra" ||
		len(analysis.Plan.Steps) != 1 ||
		analysis.Plan.Steps[0].ModuleID != "app" ||
		analysis.Plan.Steps[0].PlacementID != "config" {
		t.Fatalf(
			"analysis = %#v, actions = %#v; want one profile+extra app action",
			analysis.Modules,
			analysis.Plan.Steps,
		)
	}
}

func TestFullMutationAnalysisCompleteness(t *testing.T) {
	t.Run("complete selection", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = ["base"]`)
		fixture.writeModule(t, "base", "", nil)
		fixture.writeModule(t, "extra", "", nil)
		fixture.writeMachine(t, []string{"base"}, []string{"extra"})
		context, err := resolveContext(fixture.env)
		if err != nil {
			t.Fatalf("resolveContext() error = %v", err)
		}
		analysis, err := converge.Analyze(context.environment())
		if err != nil {
			t.Fatalf("analyzeApply() error = %v", err)
		}
		if !analysis.Plan.Complete || len(analysis.Modules) != 2 ||
			analysis.Modules[0].ID != "base" ||
			analysis.Modules[0].Convergence != "converged" ||
			analysis.Modules[1].ID != "extra" ||
			analysis.Modules[1].Convergence != "converged" {
			t.Fatalf(
				"full modules = %#v, want converged base and extra",
				analysis.Modules,
			)
		}
	})

	t.Run("unrelated module blocker", func(t *testing.T) {
		fixture := newCLITestEnv(t, `base = []`)
		fixture.writeModule(t, "extra", "", nil)
		fixture.writeMachine(t, []string{"base"}, []string{"extra", "gone"})
		context, err := resolveContext(fixture.env)
		if err != nil {
			t.Fatalf("resolveContext() error = %v", err)
		}
		analysis, err := converge.Analyze(context.environment())
		if err != nil {
			t.Fatalf("analyzeApply() error = %v", err)
		}
		if analysis.Plan.Complete || len(analysis.Plan.Steps) != 0 ||
			len(analysis.Modules) != 2 ||
			analysis.Modules[0].ID != "extra" ||
			analysis.Modules[0].Summary != "pending" ||
			analysis.Modules[0].Convergence != "unknown" ||
			analysis.Modules[1].ID != "gone" ||
			analysis.Modules[1].Summary != "conflict" ||
			analysis.Modules[1].Convergence != "conflict" {
			t.Fatalf(
				"incomplete full modules = %#v, want unknown peer plus blocker",
				analysis.Modules,
			)
		}
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

			code, stdout, _ := fixture.runInjected("status")

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
		fixture.writeMachine(t, []string{"base"}, []string{"gated"})
		before := snapshotTree(t, fixture.root)

		code, stdout, stderr := fixture.runInjected("apply", "--dry-run")

		if code != exitError ||
			!strings.Contains(stdout, "blocked module=gated") ||
			!strings.Contains(stdout, "not applicable") ||
			!strings.Contains(stderr, "state is missing") ||
			strings.Contains(stderr, "error:") {
			t.Fatalf("not-applicable dry-run = (%d, %q, %q)", code, stdout, stderr)
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

	code, stdout, stderr := fixture.runInjected("status")
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

			code, stdout, _ := fixture.runInjected("status")

			if code != exitOK ||
				!strings.Contains(
					stdout,
					"gone  conflict selection=extra ",
				) ||
				!strings.Contains(
					stdout,
					`reason="selected module \"gone\" does not exist"`,
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
		fixture.writeMachine(t, []string{"base"}, []string{"extra"})
		before := snapshotTree(t, fixture.root)

		code, stdout, _ := fixture.runInjected("status")

		if code != exitOK ||
			!strings.Contains(
				stdout,
				"app  pending selection=profile convergence=unknown",
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
