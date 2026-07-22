package apply

import (
	"errors"
	"testing"

	"github.com/mianm12/dotfiles/internal/planner"
)

func TestValidateExecutionScope_RejectsUnsupportedExecutablePlan(t *testing.T) {
	tests := []struct {
		name  string
		files []planner.FileAction
		prune []planner.PruneAction
		hooks []planner.HookAction
	}{
		{
			name: "backup replace",
			files: []planner.FileAction{{
				Verb: planner.FileBackupReplace,
			}},
		},
		{
			name: "force scaffold rebuild",
			files: []planner.FileAction{{
				Verb:   planner.FileScaffold,
				Reason: planner.FileReasonScaffoldRebuild,
			}},
		},
		{name: "malformed active prune", prune: []planner.PruneAction{{Mode: planner.PruneStateOnly}}},
		{name: "malformed hook run", hooks: []planner.HookAction{{Verb: planner.HookRun}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateExecutionScope(test.files, test.prune, test.hooks)
			if !errors.Is(err, ErrUnsupportedPlan) {
				t.Fatalf("validateExecutionScope() error = %v, want ErrUnsupportedPlan", err)
			}
		})
	}
}

func TestValidateExecutionScope_AllowsCurrentNonExecutableFacts(t *testing.T) {
	files := []planner.FileAction{
		{Verb: planner.FileSkip},
		{Verb: planner.FileConflict},
		seamLinkAction("~/.create"),
		seamLinkAdoptAction("~/.adopt"),
	}
	prune := []planner.PruneAction{{Deferred: true}}
	fixture := newRunSeamFixture(t)
	hooks := []planner.HookAction{
		seamHookAction(fixture, "app/hooks/run.sh", planner.HookRun),
		seamHookAction(fixture, "app/hooks/skip.sh", planner.HookSkip),
	}
	if err := validateExecutionScope(files, prune, hooks); err != nil {
		t.Fatalf("validateExecutionScope() error = %v", err)
	}
}

func TestValidateExecutionScope_RejectsUnknownVerbs(t *testing.T) {
	if err := validateExecutionScope([]planner.FileAction{{Verb: "future"}}, nil, nil); !errors.Is(err, ErrUnsupportedPlan) {
		t.Fatalf("unknown file verb error = %v, want ErrUnsupportedPlan", err)
	}
	if err := validateExecutionScope(nil, nil, []planner.HookAction{{Verb: "future"}}); !errors.Is(err, ErrUnsupportedPlan) {
		t.Fatalf("unknown hook verb error = %v, want ErrUnsupportedPlan", err)
	}
}
