// Package converge owns selection, analysis, planning, execution, locking,
// and state commit for one complete dot convergence operation.
package converge

import (
	"errors"
	"sort"

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
	Facts    []ModuleFact
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

// Planner decisions cover executable convergence and stale cleanup actions.
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

// ProblemKind identifies why a plan cannot be executed.
type ProblemKind string

// ProblemCode identifies a machine-readable cause when projections need
// more detail than blocked versus conflict.
type ProblemCode string

const (
	// ProblemConflict identifies a concrete planner conflict.
	ProblemConflict ProblemKind = "conflict"
	// ProblemBlocked identifies an input or path condition that blocks planning.
	ProblemBlocked ProblemKind = "blocked"
)

const (
	// ProblemCodeControlTopology identifies overlapping control families.
	ProblemCodeControlTopology ProblemCode = "control-topology"
	// ProblemCodeControlBoundary identifies a placement/control path overlap.
	ProblemCodeControlBoundary ProblemCode = "control-boundary"
)

// planRequest contains the complete desired set and ownership snapshot for one
// read-only planning pass.
type planRequest struct {
	Home     string
	Controls corepaths.ResolvedControls
	Modules  []config.Module
	State    state.Snapshot
}

// Action describes one ordered executable planner decision. LinkDestination is the
// desired raw destination. ExpectedResolvedTarget and
// ExpectedLinkDestination preserve the state facts that the executor must
// recheck before update or prune.
type Action struct {
	Order                   int
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

// Problem describes one reason a plan cannot be executed. PlacementID and
// Target are present when the problem belongs to a concrete placement.
type Problem struct {
	Kind        ProblemKind
	Code        ProblemCode
	ModuleID    string
	PlacementID string
	Target      string
	Reason      string
}

// Transition is the sole final-state decision for one logical placement key.
// Desired transitions publish FinalRecord after every Action succeeds. Stale
// transitions remove the key from FinalState.
type Transition struct {
	ModuleID    string
	PlacementID string
	Desired     bool
	FinalRecord state.Record
	Actions     []Action
}

// Plan contains one Transition per logical key and all blocking Problems.
// finalState is computed by the planner from transition facts, never by the
// executor from action order.
type Plan struct {
	Transitions []Transition
	Problems    []Problem
	finalState  state.Snapshot
}

// Executable reports whether the plan is safe to execute.
func (plan Plan) Executable() bool {
	return len(plan.Problems) == 0
}

// Actions returns the explicit executor actions in their global plan order.
func (plan Plan) Actions() []Action {
	actions := make([]Action, 0)
	for _, transition := range plan.Transitions {
		actions = append(actions, transition.Actions...)
	}
	sort.SliceStable(actions, func(left, right int) bool {
		return actions[left].Order < actions[right].Order
	})
	return actions
}

func (plan Plan) finalSnapshot() state.Snapshot {
	return cloneSnapshot(plan.finalState)
}
