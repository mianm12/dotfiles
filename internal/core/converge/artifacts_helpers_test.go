package converge

import (
	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

type artifactRequest struct {
	Home     string
	Controls corepaths.Controls
	Modules  []config.Module
}

func Execute(request artifactRequest) (executionResult, error) {
	return runLocked(request, commitState)
}

func runLocked(request artifactRequest, commit stateCommitter) (executionResult, error) {
	prepared, err := analyzeArtifactTest(request)
	if err != nil {
		return executionResult{}, err
	}
	return executePlan(
		request.Home,
		request.Controls.State,
		prepared.Plan,
		prepared.Warnings,
		prepared.Loaded,
		commit,
	)
}

type artifactTestAnalysis struct {
	Plan     Plan
	Warnings []string
	Loaded   state.Loaded
}

func (analysis artifactTestAnalysis) blockingError() error {
	if analysis.Plan.Executable() {
		return nil
	}
	return conflictError(analysis.Plan)
}

func analyzeArtifactTest(request artifactRequest) (artifactTestAnalysis, error) {
	loaded, err := loadState(request.Controls.State, request.Home)
	if err != nil {
		return artifactTestAnalysis{}, err
	}
	plan, err := buildPlan(planRequest{
		Home:     request.Home,
		Controls: request.Controls,
		Modules:  request.Modules,
		State:    loaded.Snapshot,
	})
	if err != nil {
		return artifactTestAnalysis{}, err
	}
	warnings := []string(nil)
	if loaded.Warning != "" {
		warnings = []string{loaded.Warning}
	}
	return artifactTestAnalysis{Plan: plan, Warnings: warnings, Loaded: loaded}, nil
}
