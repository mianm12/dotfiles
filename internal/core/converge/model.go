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

// ModuleFact contains only observed or resolved facts for one inventory module.
// CLI projections derive presentation from these facts and the plan.
type ModuleFact struct {
	ID             string
	Selection      string
	ManifestLoaded bool
	Applicability  string
	Variant        string
	StatePresent   bool
	Diagnostic     string
}

// Report is one complete read-only convergence result.
type Report struct {
	Facts []ModuleFact
	Plan  Plan
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

// Decision is the result of applying the ordered planning rules to one desired
// or stale placement.
type Decision string

// Planner decisions cover executable convergence and stale cleanup actions.
const (
	DecisionCreateLink  Decision = "create-link"
	DecisionCreateLocal Decision = "create-local"
	DecisionAdopt       Decision = "adopt"
	DecisionRepairState Decision = "repair-state"
	DecisionUpdate      Decision = "update"
	DecisionPrune       Decision = "prune"
	DecisionForget      Decision = "forget"
)

// IssueSeverity identifies whether an issue is informational or prevents
// execution.
type IssueSeverity string

// Issue severity values distinguish non-blocking warnings from blockers.
const (
	IssueWarning IssueSeverity = "warning"
	IssueBlocker IssueSeverity = "blocker"
)

// IssueCode is the stable machine-readable cause of one planning issue.
type IssueCode string

// Issue codes form the stable machine-readable planning vocabulary.
const (
	IssueCodeStateMissing           IssueCode = "state-missing"
	IssueCodeStalePreserved         IssueCode = "stale-preserved"
	IssueCodeSelectionIndeterminate IssueCode = "selection-indeterminate"
	IssueCodeSelectionNotApplicable IssueCode = "selection-not-applicable"
	IssueCodeControlTopology        IssueCode = "control-topology"
	IssueCodeControlBoundary        IssueCode = "control-boundary"
	IssueCodeTargetConflict         IssueCode = "target-conflict"
	IssueCodeOwnershipConflict      IssueCode = "ownership-conflict"
	IssueCodeTopologyConflict       IssueCode = "topology-conflict"
	IssueCodePlacementTypeChange    IssueCode = "placement-type-change"
)

// Recovery is the user action attached to an Issue or runtime failure.
type Recovery string

// Recovery values describe the bounded user response to an issue or failure.
const (
	RecoveryNone            Recovery = "none"
	RecoveryInit            Recovery = "init"
	RecoveryPaths           Recovery = "paths"
	RecoveryArchiveState    Recovery = "archive-state"
	RecoveryManualMigration Recovery = "manual-migration"
	RecoveryRerunApply      Recovery = "rerun-apply"
)

// planRequest contains the complete desired set and ownership snapshot for one
// read-only planning pass.
type planRequest struct {
	Home     string
	Controls corepaths.ResolvedControls
	Modules  []config.Module
	State    state.Snapshot
}

// Action describes one ordered executable planner decision. LinkDestination
// is the desired raw destination. ExpectedResolvedTarget and
// ExpectedLinkDestination preserve the state facts that the executor must
// recheck before update or prune.
type Action struct {
	ModuleID                string
	PlacementID             string
	Decision                Decision
	Target                  string
	ResolvedTarget          string
	Source                  string
	LinkDestination         string
	ExpectedResolvedTarget  string
	ExpectedLinkDestination string
	Reason                  string
}

// Issue describes one warning or blocker. PlacementID and Target are present
// when the issue belongs to a concrete placement.
type Issue struct {
	Severity    IssueSeverity
	Code        IssueCode
	ModuleID    string
	PlacementID string
	Target      string
	Reason      string
	Recovery    Recovery
}

// Plan contains the one semantic Action sequence, all structured Issues, and
// the ownership snapshot to commit after every Action succeeds. Blocked plans
// do not carry a committable nextState.
type Plan struct {
	Actions   []Action
	Issues    []Issue
	nextState state.Snapshot
}

// Executable reports whether the plan is safe to execute.
func (plan Plan) Executable() bool {
	for _, issue := range plan.Issues {
		if issue.Severity == IssueBlocker {
			return false
		}
	}
	return true
}

func (plan Plan) nextSnapshot() state.Snapshot {
	return cloneSnapshot(plan.nextState)
}
