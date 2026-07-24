// Package state defines the replacement ownership state model and its strict
// version 2 persistence format.
package state

import "errors"

const (
	// Version is the only state format version supported by the replacement core.
	Version = 2
	// MissingWarning explains the recovery limitation when no state file exists.
	MissingWarning = "state is missing; links removed from desired configuration cannot be discovered"
)

var (
	// ErrInvalid reports malformed or semantically unsafe state.
	ErrInvalid = errors.New("invalid state")
	// ErrLegacyVersion reports the incompatible legacy version 1 format.
	ErrLegacyVersion = errors.New("legacy state version")
	// ErrTooNew reports a state version newer than this binary supports.
	ErrTooNew = errors.New("state version is newer than this binary")
	// ErrHomeMismatch reports state bound to a different absolute HOME.
	ErrHomeMismatch = errors.New("state home does not match current home")
)

// Kind identifies the ownership semantics of a placement record.
type Kind string

const (
	// KindLink records link ownership.
	KindLink Kind = "link"
	// KindLocal records local provenance only.
	KindLocal Kind = "local"
)

// Snapshot is one complete state v2 value.
type Snapshot struct {
	Home    string
	Modules map[string]Module
}

// Module contains placement records keyed by placement ID.
type Module struct {
	Placements map[string]Placement
}

// Placement contains the minimum ownership or provenance evidence for one
// placement. ResolvedTarget and LinkDestination are set only for links.
type Placement struct {
	Kind            Kind
	Target          string
	ResolvedTarget  string
	LinkDestination string
}

// Loaded is the result of reading a state path. Missing state contains a valid
// empty Snapshot and a warning instead of an error.
type Loaded struct {
	Snapshot Snapshot
	Missing  bool
	Warning  string
}

// New returns an empty state bound to home.
func New(home string) (Snapshot, error) {
	cleanHome, err := cleanExpectedHome(home)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Home:    cleanHome,
		Modules: make(map[string]Module),
	}, nil
}
