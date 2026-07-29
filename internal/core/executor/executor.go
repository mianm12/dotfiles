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
	"github.com/mianm12/dotfiles/internal/lock"
	"github.com/mianm12/dotfiles/internal/storage"
)

type convergenceRequest struct {
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

type stateCommitter func(string, state.Snapshot) error

// ValidateMutationControls performs every read-only control check required
// before lock acquisition.
func ValidateMutationControls(controls corepaths.Controls) error {
	if err := corepaths.ValidateControlTopology(controls); err != nil {
		return err
	}
	configRoot := filepath.Dir(filepath.Clean(controls.Config))
	if err := storage.ValidateRoot(configRoot); err != nil {
		return fmt.Errorf(
			"validate machine config root %q before mutation: %w; "+
				"run `dot paths` to inspect the active control paths",
			configRoot,
			err,
		)
	}
	if err := storage.ValidatePrivateFile(controls.Config); err != nil {
		return fmt.Errorf(
			"validate machine config %q before mutation: %w; "+
				"run `dot paths` to inspect the active control paths",
			controls.Config,
			err,
		)
	}
	stateRoot := filepath.Dir(filepath.Clean(controls.State))
	if err := lock.Validate(stateRoot, controls.Lock); err != nil {
		return fmt.Errorf(
			"validate state root and lock %q before mutation: %w; "+
				"run `dot paths` to inspect the active control paths",
			controls.Lock,
			err,
		)
	}
	if err := storage.ValidatePrivateFile(controls.State); err != nil {
		return fmt.Errorf(
			"validate state %q before mutation: %w; "+
				"run `dot paths` to inspect the active control paths",
			controls.State,
			err,
		)
	}
	return nil
}

func runLocked(request convergenceRequest, commit stateCommitter) (Result, error) {
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	loaded, err := state.Load(request.Controls.State, request.Home)
	if err != nil {
		return Result{}, err
	}
	plan, err := planner.Build(planner.Request{
		Home:     request.Home,
		Controls: request.Controls,
		Modules:  request.Modules,
		Scope:    request.Scope,
		State:    loaded.Snapshot,
	})
	if err != nil {
		return Result{}, err
	}
	if plan.HasConflicts() {
		return Result{Plan: plan, Warnings: warnings(loaded)}, conflictError(plan)
	}

	next := cloneSnapshot(loaded.Snapshot)
	mutation := mutationRun{home: filepath.Clean(request.Home)}
	if err := mutation.apply(plan, &next); err != nil {
		return Result{
			Plan:           plan,
			TargetsChanged: mutation.changed,
			Warnings:       warnings(loaded),
		}, mutation.wrapError(err)
	}

	stateChanged := loaded.Missing ||
		loaded.NeedsRewrite ||
		!reflect.DeepEqual(loaded.Snapshot, next)
	if stateChanged {
		if err := commit(request.Controls.State, next); err != nil {
			return Result{
					Plan:           plan,
					TargetsChanged: mutation.changed,
					Warnings:       warnings(loaded),
				}, mutation.wrapError(fmt.Errorf(
					"commit state: %w; state was not committed, rerun to converge",
					err,
				))
		}
	}
	return Result{
		Plan:           plan,
		TargetsChanged: mutation.changed,
		StateChanged:   stateChanged,
		Warnings:       warnings(loaded),
	}, nil
}

func validateRequest(request convergenceRequest) error {
	if _, err := validateExecutorHome(request.Home); err != nil {
		return err
	}
	if err := ValidateMutationControls(request.Controls); err != nil {
		return fmt.Errorf("validate executor mutation controls: %w", err)
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
	for _, action := range plan.Actions {
		if action.Decision == planner.DecisionConflict {
			return fmt.Errorf(
				"plan conflict for %s/%s: %s",
				action.ModuleID,
				action.PlacementID,
				action.Reason,
			)
		}
	}
	return fmt.Errorf("plan contains a conflict")
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
