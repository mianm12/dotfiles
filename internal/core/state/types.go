// Package state defines the ownership state model and its strict version 3
// persistence format.
package state

import "errors"

const (
	// Version is the only supported state format version.
	Version = 3
	// MissingWarning explains the recovery limitation when no state file exists.
	MissingWarning = "state is missing; links removed from desired configuration cannot be discovered"
)

var (
	// ErrInvalid reports malformed or semantically unsafe state.
	ErrInvalid = errors.New("invalid state")
	// ErrLegacyVersion reports an incompatible state version older than v3.
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

// Key is the stable identity of one ownership or provenance record.
type Key struct {
	ModuleID    string
	PlacementID string
}

// Snapshot is one complete state v3 value.
type Snapshot struct {
	Home    string
	Records map[Key]Record
}

// Record contains the minimum ownership or provenance evidence for one key.
// ResolvedTarget and LinkDestination are set only for links.
type Record struct {
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
		Records: make(map[Key]Record),
	}, nil
}
