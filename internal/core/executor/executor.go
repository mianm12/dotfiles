// Package executor applies plans through the linear mutation pipeline defined
// by the product specification.
package executor

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
)

// Request contains the already resolved desired modules for one artifact
// analysis or execution pass.
type Request struct {
	Home     string
	Controls corepaths.Controls
	Modules  []config.Module
	Scope    []string
}

// Result reports the plan that was applied and whether it changed artifacts or
// ownership state. Warnings contain input diagnostics only; action outcomes are
// projected from Plan after the state commit succeeds.
type Result struct {
	Plan           planner.Plan
	TargetsChanged bool
	StateChanged   bool
	Warnings       []string
}

// Analysis is the complete read-only artifact analysis for one request.
type Analysis struct {
	Plan     planner.Plan
	Warnings []string
}

// BlockingError reports the first issue that makes this analysis unsafe to execute.
func (analysis Analysis) BlockingError() error {
	if !analysis.Plan.HasIssues() {
		return nil
	}
	return conflictError(analysis.Plan)
}

type stateCommitter func(string, state.Snapshot) error

type preparedAnalysis struct {
	report Analysis
	loaded state.Loaded
}

// Analyze loads current state and builds a plan without mutating controls,
// targets, or ownership state.
func Analyze(request Request) (Analysis, error) {
	prepared, err := analyze(request)
	if err != nil {
		return Analysis{}, err
	}
	return prepared.report, nil
}

// Execute applies a plan and commits state. The mutation package owns control
// validation and lock lifetime before calling this lower-level engine.
func Execute(request Request) (Result, error) {
	return runLocked(request, commitState)
}

func runLocked(request Request, commit stateCommitter) (Result, error) {
	prepared, err := analyze(request)
	if err != nil {
		return Result{}, err
	}
	if blocked := prepared.report.BlockingError(); blocked != nil {
		return Result{
			Plan:     prepared.report.Plan,
			Warnings: prepared.report.Warnings,
		}, blocked
	}

	next := cloneSnapshot(prepared.loaded.Snapshot)
	mutation := mutationRun{home: filepath.Clean(request.Home)}
	if err := mutation.apply(prepared.report.Plan, &next); err != nil {
		return Result{
			Plan:           prepared.report.Plan,
			TargetsChanged: mutation.changed,
			Warnings:       prepared.report.Warnings,
		}, mutation.wrapError(err)
	}

	stateChanged := prepared.loaded.Missing ||
		prepared.loaded.NeedsRewrite ||
		!reflect.DeepEqual(prepared.loaded.Snapshot, next)
	if stateChanged {
		if err := commit(request.Controls.State, next); err != nil {
			return Result{
					Plan:           prepared.report.Plan,
					TargetsChanged: mutation.changed,
					Warnings:       prepared.report.Warnings,
				}, mutation.wrapError(fmt.Errorf(
					"commit state: %w; state was not committed, rerun to converge",
					err,
				))
		}
	}
	return Result{
		Plan:           prepared.report.Plan,
		TargetsChanged: mutation.changed,
		StateChanged:   stateChanged,
		Warnings:       prepared.report.Warnings,
	}, nil
}

func analyze(request Request) (preparedAnalysis, error) {
	if err := validateRequest(request); err != nil {
		return preparedAnalysis{}, err
	}
	loaded, err := state.Load(request.Controls.State, request.Home)
	if err != nil {
		return preparedAnalysis{}, err
	}
	plan, err := planner.Build(planner.Request{
		Home:     request.Home,
		Controls: request.Controls,
		Modules:  request.Modules,
		Scope:    request.Scope,
		State:    loaded.Snapshot,
	})
	if err != nil {
		return preparedAnalysis{}, err
	}
	return preparedAnalysis{
		report: Analysis{
			Plan:     plan,
			Warnings: warnings(loaded),
		},
		loaded: loaded,
	}, nil
}

func validateRequest(request Request) error {
	if _, err := validateExecutorHome(request.Home); err != nil {
		return err
	}
	return nil
}

func validateExecutorHome(home string) (string, error) {
	if home == "" ||
		!filepath.IsAbs(home) ||
		strings.ContainsRune(home, '\x00') ||
		!utf8.ValidString(home) {
		return "", fmt.Errorf(
			"executor HOME must be a non-empty absolute path without NUL and with valid UTF-8",
		)
	}
	return filepath.Clean(home), nil
}

func warnings(loaded state.Loaded) []string {
	if loaded.Warning == "" {
		return nil
	}
	return []string{loaded.Warning}
}

func conflictError(plan planner.Plan) error {
	if len(plan.Issues) != 0 {
		issue := plan.Issues[0]
		return fmt.Errorf(
			"plan %s for %s/%s: %s",
			issue.Kind,
			issue.ModuleID,
			issue.PlacementID,
			issue.Reason,
		)
	}
	return fmt.Errorf("plan contains an issue")
}

func cloneSnapshot(snapshot state.Snapshot) state.Snapshot {
	cloned := state.Snapshot{
		Home:    snapshot.Home,
		Modules: make(map[string]state.Module, len(snapshot.Modules)),
	}
	for moduleID, module := range snapshot.Modules {
		placements := make(map[string]state.Placement, len(module.Placements))
		for placementID, placement := range module.Placements {
			placements[placementID] = placement
		}
		cloned.Modules[moduleID] = state.Module{Placements: placements}
	}
	return cloned
}
