// Package state defines the ownership state model and its version 5
// persistence format.
package state

import "errors"

const (
	// Version is the only supported state format version.
	Version = 5
	// MissingWarning explains the recovery limitation when no state file exists.
	MissingWarning = "state is missing; links removed from desired configuration cannot be discovered"
)

var (
	// ErrInvalid reports malformed or semantically unsafe state.
	ErrInvalid = errors.New("invalid state")
	// ErrLegacyVersion reports an incompatible state version older than v5.
	ErrLegacyVersion = errors.New("legacy state version")
	// ErrTooNew reports a state version newer than this binary supports.
	ErrTooNew = errors.New("state version is newer than this binary")
	// ErrHomeMismatch reports state bound to a different absolute HOME.
	ErrHomeMismatch = errors.New("state home does not match current home")
)

// Key is the stable identity of one link ownership record.
type Key struct {
	ModuleID    string
	PlacementID string
}

// Snapshot is one complete state v5 value.
type Snapshot struct {
	Home  string
	Links map[Key]LinkRecord
}

// LinkRecord is the cached ownership evidence for one managed link.
type LinkRecord struct {
	Target string
	Dest   string
}

// Loaded is the result of reading a state path. Missing state contains a valid
// empty Snapshot and a warning instead of an error.
type Loaded struct {
	Snapshot Snapshot
	Warning  string
}

// New returns an empty state bound to home.
func New(home string) (Snapshot, error) {
	cleanHome, err := cleanExpectedHome(home)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Home:  cleanHome,
		Links: make(map[Key]LinkRecord),
	}, nil
}
