package executor

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/lock"
)

// ErrSessionClosed reports use of a nil, copied, or successfully closed
// mutation session.
var ErrSessionClosed = errors.New("mutation session is closed")

// ErrSessionClosing reports a failed lock release that must be retried before
// the Session can be used again.
var ErrSessionClosing = errors.New("mutation session lock release is pending")

// Session owns the fixed control boundary and the single advisory lock for one
// real mutation. It is a linear capability; value copies are invalid and
// concurrent use is unsupported.
type Session struct {
	self     *Session
	home     string
	controls corepaths.Controls
	release  func() error
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
	session := &Session{
		home:     home,
		controls: controls,
		release:  owner.Release,
	}
	session.self = session
	return session, nil
}

// PublishSelection publishes a changed machine selection within the Session's
// fixed repository and config boundary.
func (session *Session) PublishSelection(machine config.Machine) (bool, error) {
	if err := session.ensureMutable(); err != nil {
		return false, err
	}
	if machine.Repository != session.controls.Repository {
		return false, fmt.Errorf(
			"machine repository %q does not match mutation session repository %q",
			machine.Repository,
			session.controls.Repository,
		)
	}
	if err := ValidateMutationControls(session.controls); err != nil {
		return false, fmt.Errorf("revalidate mutation controls before publishing selection: %w", err)
	}
	current, exists, err := config.LoadMachine(session.controls.Config)
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
	return config.PublishMachine(session.controls.Config, machine)
}

// Converge reloads current state, rebuilds the plan, applies it, verifies
// changed targets, and commits state while the Session lock remains held.
func (session *Session) Converge(
	modules []config.Module,
	scope []string,
) (Result, error) {
	if err := session.ensureMutable(); err != nil {
		return Result{}, err
	}
	return runLocked(convergenceRequest{
		Home:     session.home,
		Controls: session.controls,
		Modules:  modules,
		Scope:    scope,
	}, commitState)
}

// Close releases the Session's single mutation lock. A failed release blocks
// further mutation but preserves ownership so callers can retry Close.
func (session *Session) Close() error {
	if session == nil || session.self != session || session.release == nil {
		return ErrSessionClosed
	}
	session.closing = true
	if err := session.release(); err != nil {
		return err
	}
	session.closing = false
	session.release = nil
	session.self = nil
	return nil
}

func (session *Session) ensureMutable() error {
	if session == nil || session.self != session || session.release == nil {
		return ErrSessionClosed
	}
	if session.closing {
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
