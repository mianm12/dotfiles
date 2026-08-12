// Package state defines the ownership state model and its strict version 4
// persistence format.
package state

import (
	"errors"
	"maps"
)

const (
	// Version is the only supported state format version.
	Version = 4
	// MissingWarning explains the recovery limitation when no state file exists.
	MissingWarning = "state is missing; links removed from desired configuration cannot be discovered"
)

var (
	// ErrInvalid reports malformed or semantically unsafe state.
	ErrInvalid = errors.New("invalid state")
	// ErrLegacyVersion reports an incompatible state version older than v4.
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

// Snapshot is one complete state v4 value.
type Snapshot struct {
	Home  string
	Links map[Key]LinkRecord
}

// LinkRecord contains the minimum ownership evidence for one managed link.
type LinkRecord struct {
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
		Home:  cleanHome,
		Links: make(map[Key]LinkRecord),
	}, nil
}

// Equal reports whether two snapshots contain the same HOME binding and link
// ownership facts.
func Equal(left, right Snapshot) bool {
	return left.Home == right.Home && maps.Equal(left.Links, right.Links)
}
