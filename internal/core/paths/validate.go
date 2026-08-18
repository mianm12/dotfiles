package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrControlTopology reports control prefixes that are not isolated.
var ErrControlTopology = errors.New("control paths conflict")

// Controls contains the protected paths named by the placement specification.
type Controls struct {
	Repository string
	Config     string
	State      string
	Lock       string
}

// LexicalControls is one cleaned and validated lexical control-path snapshot.
type LexicalControls struct {
	paths Controls
	valid bool
}

// NormalizeControls cleans and validates one control-prefix snapshot without
// observing placement targets.
func NormalizeControls(controls Controls) (LexicalControls, error) {
	repository, err := cleanAbsolute("repository", controls.Repository)
	if err != nil {
		return LexicalControls{}, err
	}
	paths, err := cleanCoordinationPaths(controls.Config, controls.State, controls.Lock)
	if err != nil {
		return LexicalControls{}, err
	}
	if err := validateControlPrefixes(repository, paths.configRoot, paths.stateRoot); err != nil {
		return LexicalControls{}, err
	}
	return LexicalControls{
		paths: Controls{
			Repository: repository,
			Config:     paths.config,
			State:      paths.state,
			Lock:       paths.lock,
		},
		valid: true,
	}, nil
}

// ValidateLockBoundary validates the config/state coordination needed before a
// mutation may create and acquire its advisory lock.
func ValidateLockBoundary(config, state, lock string) error {
	paths, err := cleanCoordinationPaths(config, state, lock)
	if err != nil {
		return err
	}
	if prefixesOverlap(paths.configRoot, paths.stateRoot) {
		return fmt.Errorf(
			"%w: machine config root %q overlaps state root %q",
			ErrControlTopology,
			paths.configRoot,
			paths.stateRoot,
		)
	}
	return validateExistingRoots(paths.configRoot, paths.stateRoot)
}

// Paths returns the cleaned lexical paths captured by this snapshot.
func (controls LexicalControls) Paths() (Controls, error) {
	if err := controls.validate(); err != nil {
		return Controls{}, err
	}
	return controls.paths, nil
}

// TargetOverlaps reports whether target intersects a protected control prefix.
func (controls LexicalControls) TargetOverlaps(home string, target Target) (bool, error) {
	if err := controls.validate(); err != nil {
		return false, err
	}
	absolute, err := target.Absolute(home)
	if err != nil {
		return false, err
	}
	for _, prefix := range controlPrefixes(controls.paths) {
		if prefixesOverlap(prefix, absolute) {
			return true, nil
		}
	}
	return false, nil
}

func (controls LexicalControls) validate() error {
	if !controls.valid {
		return fmt.Errorf("%w: control snapshot is uninitialized", ErrControlTopology)
	}
	return nil
}

type coordinationPaths struct {
	configRoot string
	config     string
	stateRoot  string
	state      string
	lock       string
}

func cleanCoordinationPaths(config, state, lock string) (coordinationPaths, error) {
	cleanConfig, err := cleanAbsolute("machine config", config)
	if err != nil {
		return coordinationPaths{}, err
	}
	cleanState, err := cleanAbsolute("state", state)
	if err != nil {
		return coordinationPaths{}, err
	}
	cleanLock, err := cleanAbsolute("lock", lock)
	if err != nil {
		return coordinationPaths{}, err
	}

	paths := coordinationPaths{
		configRoot: filepath.Dir(cleanConfig),
		config:     cleanConfig,
		stateRoot:  filepath.Dir(cleanState),
		state:      cleanState,
		lock:       cleanLock,
	}
	if !directChild(paths.configRoot, paths.config) {
		return coordinationPaths{}, fmt.Errorf(
			"%w: machine config %q must be a direct child of config root %q",
			ErrControlTopology,
			paths.config,
			paths.configRoot,
		)
	}
	if paths.stateRoot != filepath.Dir(paths.lock) ||
		!directChild(paths.stateRoot, paths.state) ||
		!directChild(paths.stateRoot, paths.lock) ||
		paths.state == paths.lock {
		return coordinationPaths{}, fmt.Errorf(
			"%w: state %q and lock %q must be distinct siblings under one state root",
			ErrControlTopology,
			paths.state,
			paths.lock,
		)
	}
	return paths, nil
}

func validateControlPrefixes(repository, configRoot, stateRoot string) error {
	prefixes := [...]struct {
		label string
		path  string
	}{
		{"repository", repository},
		{"machine config root", configRoot},
		{"state root", stateRoot},
	}
	for left := range prefixes {
		for right := left + 1; right < len(prefixes); right++ {
			if !prefixesOverlap(prefixes[left].path, prefixes[right].path) {
				continue
			}
			return fmt.Errorf(
				"%w: %s %q overlaps %s %q",
				ErrControlTopology,
				prefixes[left].label,
				prefixes[left].path,
				prefixes[right].label,
				prefixes[right].path,
			)
		}
	}
	return nil
}

func validateExistingRoots(roots ...string) error {
	for _, root := range roots {
		info, err := os.Lstat(root)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%w: inspect control root %q: %w", ErrControlTopology, root, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf(
				"%w: control root %q is a symbolic link; expected a directory",
				ErrControlTopology,
				root,
			)
		}
		if !info.IsDir() {
			return fmt.Errorf(
				"%w: control root %q is not a directory",
				ErrControlTopology,
				root,
			)
		}
	}
	return nil
}

func controlPrefixes(paths Controls) []string {
	return []string{
		paths.Repository,
		filepath.Dir(paths.Config),
		filepath.Dir(paths.State),
	}
}

func prefixesOverlap(left, right string) bool {
	return sameOrDescendant(left, right) || sameOrDescendant(right, left)
}

func directChild(parent, child string) bool {
	return parent != child && filepath.Dir(child) == parent
}
