// Package converge owns selection, analysis, the convergence loop, execution,
// locking, and state commit for one complete dot operation.
package converge

import (
	"io/fs"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

// Environment fixes the invocation paths for one convergence call and defers
// platform observation until the authoritative analysis boundary.
type Environment struct {
	Home       string
	ConfigPath string
	StatePath  string
	LockPath   string
	Platform   func() config.Platform
}

// ModuleFact contains only selection and observation facts for one inventory module.
type ModuleFact struct {
	ID             string
	Selection      string
	ManifestLoaded bool
	Applicability  string
	Variant        string
	StatePresent   bool
	Diagnostic     string
}

// Op is one user-visible loop line.
type Op string

// Loop operations are the only public vocabulary for preview and execution.
const (
	OpLink    Op = "link"
	OpFile    Op = "file"
	OpReplace Op = "replace"
	OpRemove  Op = "remove"
	OpRecord  Op = "record"
	OpForget  Op = "forget"
	OpChmod   Op = "chmod"
	OpSkip    Op = "skip"
)

// Line is one loop result for a desired or stale placement.
type Line struct {
	Op          Op
	ModuleID    string
	PlacementID string
	Target      string
	Control     string
	Path        string
	Mode        string
	Reason      string
}

// Report is one complete read-only convergence result.
type Report struct {
	Facts        []ModuleFact
	Lines        []Line
	StateWarning string
}

// HasSkip reports whether any line refuses mutation.
func (report Report) HasSkip() bool {
	for _, line := range report.Lines {
		if line.Op == OpSkip {
			return true
		}
	}
	return false
}

// ApplyResult reports one complete apply result.
type ApplyResult struct {
	Report          Report
	Done            []Line
	TargetsChanged  bool
	StateChanged    bool
	ControlsChanged bool
}

// SelectionResult reports one config-only selection mutation.
type SelectionResult struct {
	Machine         config.Machine
	Changed         bool
	ProfileSelected bool
}

type loopRequest struct {
	Home              string
	Controls          corepaths.LexicalControls
	Modules           []config.Module
	State             state.Snapshot
	IncompleteModules map[string]struct{}
}

type loopLine struct {
	Line
	source     string
	dest       string
	beforeDest string
	targetID   string
	mode       fs.FileMode
}
