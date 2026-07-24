// Package config strictly loads the replacement repository and machine
// configuration model.
package config

import (
	"errors"
	"io/fs"
)

var (
	// ErrInvalidConfiguration reports a malformed or inconsistent config.
	ErrInvalidConfiguration = errors.New("invalid configuration")
	// ErrNotApplicable reports a required module with no matching platform form.
	ErrNotApplicable = errors.New("module is not applicable")
)

// Platform is the explicit platform input used for module matching.
type Platform struct {
	OS     string
	Distro string
	Arch   string
}

// Machine is one strictly decoded machine selection.
type Machine struct {
	Version      int
	Repository   string
	Profiles     []string
	ExtraModules []string
}

// Scope names the modules needed by one operation. Profile modules may be
// not-applicable; extras and explicitly required modules must apply.
type Scope struct {
	Profiles        []string
	ExtraModules    []string
	RequiredModules []string
}

// Scope returns a detached scope derived from the machine selection.
func (machine Machine) Scope(required ...string) Scope {
	return Scope{
		Profiles:        append([]string(nil), machine.Profiles...),
		ExtraModules:    append([]string(nil), machine.ExtraModules...),
		RequiredModules: append([]string(nil), required...),
	}
}

// Resolution is the deterministic result of loading exactly one scope.
type Resolution struct {
	Modules       []Module
	NotApplicable []string
}

// Module is one portable module or selected variant.
type Module struct {
	ID      string
	Variant string
	Root    string
	Links   []Link
	Locals  []Local
}

// Link is one validated file or directory link placement.
type Link struct {
	ID         string
	Source     string
	SourcePath string
	Target     string
	SourceMode fs.FileMode
}

// Local is one validated local-file placement.
type Local struct {
	ID          string
	Example     string
	ExamplePath string
	Target      string
}
