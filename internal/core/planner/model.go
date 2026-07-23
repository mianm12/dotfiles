// Package planner builds read-only convergence plans for the replacement core.
package planner

import (
	"slices"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

// Decision is the result of applying the ordered rules from design baseline
// section 9 to one desired or stale placement.
type Decision string

// Planner decisions cover active convergence, stale cleanup, and conflicts.
const (
	DecisionCreateLink  Decision = "create-link"
	DecisionCreateLocal Decision = "create-local"
	DecisionAdopt       Decision = "adopt"
	DecisionKeep        Decision = "keep"
	DecisionRepairState Decision = "repair-state"
	DecisionUpdate      Decision = "update"
	DecisionPrune       Decision = "prune"
	DecisionForget      Decision = "forget"
	DecisionConflict    Decision = "conflict"
)

// Request contains the complete desired set and ownership snapshot for one
// read-only planning pass.
type Request struct {
	Home     string
	Controls corepaths.Controls
	Modules  []config.Module
	// Scope limits active and stale decisions to the named modules while still
	// comparing their targets with every module in Modules. Nil means a full
	// plan, including state-only stale modules.
	Scope []string
	State state.Snapshot
}

// Action describes one ordered planner decision. LinkDestination is the
// desired raw destination. ExpectedResolvedTarget and
// ExpectedLinkDestination preserve the state facts that B5 must recheck before
// update or prune.
type Action struct {
	ModuleID                string
	PlacementID             string
	Kind                    state.Kind
	Decision                Decision
	Target                  string
	ResolvedTarget          string
	Source                  string
	LinkDestination         string
	ExpectedResolvedTarget  string
	ExpectedLinkDestination string
	Reason                  string
}

// Plan contains active-placement decisions followed by stale cleanup
// decisions. Warnings describe safe ownership abandonment and local
// provenance removal.
type Plan struct {
	Actions  []Action
	Warnings []string
}

// HasConflicts reports whether the plan is unsafe to execute.
func (plan Plan) HasConflicts() bool {
	return slices.ContainsFunc(plan.Actions, func(action Action) bool {
		return action.Decision == DecisionConflict
	})
}
