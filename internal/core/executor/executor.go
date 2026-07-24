// Package executor applies replacement-core plans through the linear mutation
// pipeline defined by the design baseline.
package executor

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/mianm12/dotfiles/internal/lock"
)

// Request contains validated, in-memory desired inputs and the stable control
// paths used for one mutation. Configuration loading and selection persistence
// are owned by the CLI layer introduced at B6.
type Request struct {
	Home     string
	Controls corepaths.Controls
	Modules  []config.Module
	Scope    []string
}

// Result reports the plan that was applied and whether it changed artifacts or
// ownership state. Advisory-lock bookkeeping is not counted as a mutation.
type Result struct {
	Plan           planner.Plan
	TargetsChanged bool
	StateChanged   bool
	Warnings       []string
}

type stateCommitter func(string, state.Snapshot) error

// Run obtains the stable mutation lock, reloads state, rebuilds the plan, and
// applies it. Dry-run plans are never accepted as executable input.
func Run(request Request) (result Result, err error) {
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}

	lockRoot := filepath.Dir(filepath.Clean(request.Controls.Lock))
	owner, err := lock.Acquire(lockRoot, request.Controls.Lock)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		releaseErr := owner.Release()
		if releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
	}()

	return runLocked(request, commitState)
}

// RunWithLock applies a request while reusing an outer owner bound to the same
// stable lock. B6 uses this after prospective selection preflight and machine
// config publication so the entire mutation pipeline remains under one lock.
func RunWithLock(
	request Request,
	owner *lock.Ownership,
) (result Result, err error) {
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	if owner == nil {
		return Result{}, fmt.Errorf("executor lock owner is nil")
	}
	lockRoot := filepath.Dir(filepath.Clean(request.Controls.Lock))
	guard, err := owner.Reuse(lockRoot, request.Controls.Lock)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		releaseErr := guard.Release()
		if releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
	}()
	return runLocked(request, commitState)
}

func runLocked(request Request, commit stateCommitter) (Result, error) {
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
		return Result{Plan: plan, Warnings: warnings(loaded, plan)}, conflictError(plan)
	}

	next := cloneSnapshot(loaded.Snapshot)
	mutation := mutationRun{home: filepath.Clean(request.Home)}
	if err := mutation.apply(plan, &next); err != nil {
		return Result{
			Plan:           plan,
			TargetsChanged: mutation.changed,
			Warnings:       warnings(loaded, plan),
		}, mutation.wrapError(err)
	}

	stateChanged := loaded.Missing || !reflect.DeepEqual(loaded.Snapshot, next)
	if stateChanged {
		if err := commit(request.Controls.State, next); err != nil {
			return Result{
				Plan:           plan,
				TargetsChanged: mutation.changed,
				Warnings:       warnings(loaded, plan),
			}, mutation.wrapError(fmt.Errorf("commit state: %w", err))
		}
	}
	return Result{
		Plan:           plan,
		TargetsChanged: mutation.changed,
		StateChanged:   stateChanged,
		Warnings:       warnings(loaded, plan),
	}, nil
}

func validateRequest(request Request) error {
	if request.Home == "" || !filepath.IsAbs(request.Home) {
		return fmt.Errorf("executor HOME must be a non-empty absolute path")
	}
	controls := request.Controls
	controlPaths := []struct {
		label string
		path  string
	}{
		{label: "repository", path: controls.Repository},
		{label: "machine config", path: controls.Config},
		{label: "state", path: controls.State},
		{label: "lock", path: controls.Lock},
	}
	for _, control := range controlPaths {
		if control.path == "" || !filepath.IsAbs(control.path) {
			return fmt.Errorf(
				"executor %s path must be a non-empty absolute path",
				control.label,
			)
		}
	}
	if filepath.Dir(filepath.Clean(controls.State)) !=
		filepath.Dir(filepath.Clean(controls.Lock)) {
		return fmt.Errorf("executor state and lock must share one control directory")
	}
	return nil
}

func warnings(loaded state.Loaded, plan planner.Plan) []string {
	size := len(plan.Warnings)
	if loaded.Warning != "" {
		size++
	}
	result := make([]string, 0, size)
	if loaded.Warning != "" {
		result = append(result, loaded.Warning)
	}
	return append(result, plan.Warnings...)
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
