package executor

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/lock"
)

// ErrSessionClosed reports use of a mutation session after its lock was
// successfully released.
var ErrSessionClosed = errors.New("mutation session is closed")

// ErrSessionClosing reports a failed lock release that must be retried before
// the Session can be used again.
var ErrSessionClosing = errors.New("mutation session lock release is pending")

// Session owns the fixed control boundary and the single advisory lock for one
// real mutation.
type Session struct {
	state *sessionState
}

type sessionState struct {
	mu       sync.Mutex
	home     string
	controls corepaths.Controls
	owner    *lock.Ownership
	release  func() error
	closed   bool
	closing  bool
}

// OpenSession validates the complete control boundary before acquiring its
// single mutation lock.
func OpenSession(home string, controls corepaths.Controls) (*Session, error) {
	if home == "" || !filepath.IsAbs(home) {
		return nil, fmt.Errorf("executor HOME must be a non-empty absolute path")
	}
	home = filepath.Clean(home)
	controls = cleanControls(controls)
	if err := ValidateMutationControls(controls); err != nil {
		return nil, fmt.Errorf("validate executor mutation controls: %w", err)
	}
	owner, err := lock.Acquire(filepath.Dir(controls.Lock), controls.Lock)
	if err != nil {
		return nil, err
	}
	return &Session{
		state: &sessionState{
			home:     home,
			controls: controls,
			owner:    owner,
			release:  owner.Release,
		},
	}, nil
}

// PublishSelection publishes a changed machine selection within the Session's
// fixed repository and config boundary.
func (session *Session) PublishSelection(machine config.Machine) (bool, error) {
	state, err := session.mutableState()
	if err != nil {
		return false, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := state.ensureMutable(); err != nil {
		return false, err
	}
	if machine.Repository != state.controls.Repository {
		return false, fmt.Errorf(
			"machine repository %q does not match mutation session repository %q",
			machine.Repository,
			state.controls.Repository,
		)
	}
	if err := ValidateMutationControls(state.controls); err != nil {
		return false, fmt.Errorf("revalidate mutation controls before publishing selection: %w", err)
	}
	current, exists, err := config.LoadMachine(state.controls.Config)
	if err != nil {
		return false, err
	}
	if exists {
		equal, err := equalMachine(current, machine)
		if err != nil {
			return false, err
		}
		if equal {
			return false, nil
		}
	}
	return config.PublishMachine(state.controls.Config, machine)
}

// Converge reloads current state, rebuilds the plan, applies it, verifies
// changed targets, and commits state while the Session lock remains held.
func (session *Session) Converge(
	modules []config.Module,
	scope []string,
) (Result, error) {
	state, err := session.mutableState()
	if err != nil {
		return Result{}, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := state.ensureMutable(); err != nil {
		return Result{}, err
	}
	return runLocked(convergenceRequest{
		Home:     state.home,
		Controls: state.controls,
		Modules:  modules,
		Scope:    append([]string(nil), scope...),
	}, commitState)
}

// Close releases the Session's single mutation lock. A failed release blocks
// further mutation but preserves ownership so callers can retry Close.
func (session *Session) Close() error {
	if session == nil || session.state == nil {
		return ErrSessionClosed
	}
	state := session.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed || state.owner == nil || state.release == nil {
		return ErrSessionClosed
	}
	state.closing = true
	if err := state.release(); err != nil {
		return err
	}
	state.closed = true
	state.closing = false
	state.owner = nil
	state.release = nil
	return nil
}

func (session *Session) mutableState() (*sessionState, error) {
	if session == nil || session.state == nil {
		return nil, ErrSessionClosed
	}
	return session.state, nil
}

func (state *sessionState) ensureMutable() error {
	if state.closed || state.owner == nil || state.release == nil {
		return ErrSessionClosed
	}
	if state.closing {
		return ErrSessionClosing
	}
	return nil
}

func cleanControls(controls corepaths.Controls) corepaths.Controls {
	return corepaths.Controls{
		Repository: cleanPath(controls.Repository),
		Config:     cleanPath(controls.Config),
		State:      cleanPath(controls.State),
		Lock:       cleanPath(controls.Lock),
	}
}

func cleanPath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func equalMachine(left, right config.Machine) (bool, error) {
	leftData, err := config.MarshalMachine(left)
	if err != nil {
		return false, err
	}
	rightData, err := config.MarshalMachine(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftData, rightData), nil
}
