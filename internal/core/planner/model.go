// Package planner builds read-only convergence plans for dot.
package planner

import (
	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

// Decision is the result of applying the ordered planning rules to one desired
// or stale placement.
type Decision string

// Planner decisions cover executable convergence and stale cleanup steps.
const (
	DecisionCreateLink  Decision = "create-link"
	DecisionCreateLocal Decision = "create-local"
	DecisionAdopt       Decision = "adopt"
	DecisionKeep        Decision = "keep"
	DecisionRepairState Decision = "repair-state"
	DecisionUpdate      Decision = "update"
	DecisionPrune       Decision = "prune"
	DecisionForget      Decision = "forget"
)

// IssueKind identifies why a plan cannot be executed.
type IssueKind string

// IssueCode identifies a machine-readable issue cause when projections need
// more detail than blocked versus conflict.
type IssueCode string

const (
	// IssueConflict identifies a concrete planner conflict.
	IssueConflict IssueKind = "conflict"
	// IssueBlocked identifies an input or path condition that blocks planning.
	IssueBlocked IssueKind = "blocked"
)

const (
	// IssueCodeControlBoundary identifies a placement/control path overlap.
	IssueCodeControlBoundary IssueCode = "control-boundary"
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

// Step describes one ordered executable planner decision. LinkDestination is the
// desired raw destination. ExpectedResolvedTarget and
// ExpectedLinkDestination preserve the state facts that the executor must
// recheck before update or prune.
type Step struct {
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

// Issue describes one reason a plan cannot be executed. PlacementID and Target
// are present when the issue belongs to a concrete placement.
type Issue struct {
	Kind        IssueKind
	Code        IssueCode
	ModuleID    string
	PlacementID string
	Target      string
	Reason      string
}

// Plan is the single planning report: executable Steps and blocking Issues.
// Steps preserve active-before-stale ordering. Step.Reason is the single source
// of truth for why an executable step was selected.
type Plan struct {
	Steps  []Step
	Issues []Issue
}

// HasIssues reports whether the plan is unsafe to execute.
func (plan Plan) HasIssues() bool {
	return len(plan.Issues) != 0
}
