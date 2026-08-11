// Package converge owns selection, analysis, planning, execution, locking,
// and state commit for one complete dot convergence operation.
package converge

import (
	"errors"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

var (
	// ErrBlocked reports a fully expressed safety blocker or a changed preflight.
	ErrBlocked = errors.New("convergence blocked")
	// ErrPartial reports that a mutation may have changed targets or control data.
	ErrPartial = errors.New("convergence may be partially applied")
	// ErrUninitialized reports that no machine selection exists yet.
	ErrUninitialized = errors.New("machine is not initialized")
	// ErrControl reports that control paths or entries cannot be used safely.
	ErrControl = errors.New("invalid convergence control")
	// ErrState reports that ownership state cannot be read reliably.
	ErrState = errors.New("unreadable convergence state")
)

// Environment fixes every machine-specific input for one convergence call.
type Environment struct {
	Home       string
	ConfigPath string
	StatePath  string
	LockPath   string
	Platform   config.Platform
}

// ModuleReport is the orthogonal status projection for one inventory module.
type ModuleReport struct {
	ID            string
	Summary       string
	Selection     string
	Applicability string
	Convergence   string
	Variant       string
	NamedVariant  bool
	Reason        string
}

// Report is one complete read-only convergence result.
type Report struct {
	Modules  []ModuleReport
	Plan     Plan
	Warnings []string
}

// ApplyResult reports a successfully committed convergence run.
type ApplyResult struct {
	Report         Report
	TargetsChanged bool
	StateChanged   bool
}

// SelectionResult reports one config-only selection mutation.
type SelectionResult struct {
	Machine         config.Machine
	Changed         bool
	ProfileSelected bool
}

// Kind exposes placement kinds needed to project committed cleanup results.
type Kind = state.Kind

const (
	// KindLink identifies a managed symbolic-link placement.
	KindLink = state.KindLink
	// KindLocal identifies a user-owned local-file placement.
	KindLocal = state.KindLocal
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
	// IssueCodeControlTopology identifies overlapping control families.
	IssueCodeControlTopology IssueCode = "control-topology"
	// IssueCodeControlBoundary identifies a placement/control path overlap.
	IssueCodeControlBoundary IssueCode = "control-boundary"
)

// planRequest contains the complete desired set and ownership snapshot for one
// read-only planning pass.
type planRequest struct {
	Home     string
	Controls corepaths.Controls
	Modules  []config.Module
	State    state.Snapshot
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
// Complete reports whether every desired placement and stale state record
// received a decision. Steps preserve active-before-stale ordering. Step.Reason
// is the single source of truth for why an executable step was selected.
type Plan struct {
	Complete bool
	Steps    []Step
	Issues   []Issue
}

// Executable reports whether the complete plan is safe to execute.
func (plan Plan) Executable() bool {
	return plan.Complete && len(plan.Issues) == 0
}
