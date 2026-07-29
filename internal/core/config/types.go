// Package config strictly loads the repository and machine configuration model.
package config

import (
	"errors"
)

// ErrInvalidConfiguration reports a malformed or inconsistent config.
var ErrInvalidConfiguration = errors.New("invalid configuration")

// ApplicabilityState is the result of matching one module against a platform.
type ApplicabilityState string

const (
	// ApplicabilityApplicable means every constrained field is known and matches.
	ApplicabilityApplicable ApplicabilityState = "applicable"
	// ApplicabilityNotApplicable means at least one known field does not match.
	ApplicabilityNotApplicable ApplicabilityState = "not-applicable"
	// ApplicabilityIndeterminate means no known field mismatches but evidence is incomplete.
	ApplicabilityIndeterminate ApplicabilityState = "indeterminate"
)

// ModuleApplicability contains one applicability result and its uncertainty diagnostic.
type ModuleApplicability struct {
	State      ApplicabilityState
	Diagnostic string
}

// PlatformField is one detected platform value or an explicit unknown diagnostic.
type PlatformField struct {
	Value      string
	Known      bool
	Diagnostic string
}

// KnownPlatformField returns a known platform field.
func KnownPlatformField(value string) PlatformField {
	return PlatformField{Value: value, Known: true}
}

// UnknownPlatformField returns an unknown platform field with its diagnostic.
func UnknownPlatformField(diagnostic string) PlatformField {
	return PlatformField{Diagnostic: diagnostic}
}

// Platform is the explicit, evidence-carrying input used for module matching.
type Platform struct {
	OS     PlatformField
	Distro PlatformField
	Arch   PlatformField
}

// Machine is one strictly decoded machine selection.
type Machine struct {
	Version      int
	Repository   string
	Profiles     []string
	ExtraModules []string
}

// Module is one portable module or selected variant.
type Module struct {
	ID      string
	Variant string
	Links   []Link
	Locals  []Local
}

// Link is one validated file or directory link placement.
type Link struct {
	ID         string
	SourcePath string
	Target     string
}

// Local is one validated local-file placement.
type Local struct {
	ID          string
	ExamplePath string
	Target      string
}
