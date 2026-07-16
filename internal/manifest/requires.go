package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var (
	// ErrRepositoryUnavailable means the configured repository has not been installed.
	ErrRepositoryUnavailable = errors.New("repository unavailable")
	requirementPattern       = regexp.MustCompile(`^>=([0-9]+)\.([0-9]+)\.([0-9]+)$`)
	releasePattern           = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)
)

// Requirement is a validated minimum CLI version constraint.
type Requirement struct {
	Raw     string
	minimum numericVersion
}

type numericVersion [3]string

// ReadRequirement performs the permissive top-level requires pre-read.
func ReadRequirement(repo string) (Requirement, error) {
	info, err := os.Stat(repo)
	if err != nil {
		if os.IsNotExist(err) {
			return Requirement{}, ErrRepositoryUnavailable
		}
		return Requirement{}, fmt.Errorf("inspect repository %q: %w", repo, err)
	}
	if !info.IsDir() {
		return Requirement{}, fmt.Errorf("repository path %q is not a directory", repo)
	}

	manifestPath := filepath.Join(repo, "dot.toml")
	file, err := os.Open(manifestPath)
	if err != nil {
		return Requirement{}, fmt.Errorf("open manifest %q: %w", manifestPath, err)
	}

	var document struct {
		Requires *string `toml:"requires"`
	}
	decodeErr := toml.NewDecoder(file).Decode(&document)
	closeErr := file.Close()
	if decodeErr != nil {
		return Requirement{}, fmt.Errorf("decode manifest %q for requires: %w", manifestPath, decodeErr)
	}
	if closeErr != nil {
		return Requirement{}, fmt.Errorf("close manifest %q after reading: %w", manifestPath, closeErr)
	}
	if document.Requires == nil {
		return Requirement{}, fmt.Errorf("manifest %q: required top-level requires is missing", manifestPath)
	}

	return ParseRequirement(*document.Requires)
}

// ParseRequirement validates the only supported constraint syntax.
func ParseRequirement(raw string) (Requirement, error) {
	match := requirementPattern.FindStringSubmatch(raw)
	if match == nil {
		return Requirement{}, fmt.Errorf("invalid requires %q: want >=MAJOR.MINOR.PATCH", raw)
	}
	return Requirement{
		Raw:     raw,
		minimum: numericVersion{normalize(match[1]), normalize(match[2]), normalize(match[3])},
	}, nil
}

// Satisfies reports compatibility. Development builds skip only the version comparison.
func Satisfies(cliVersion string, requirement Requirement) (satisfied, development bool, err error) {
	if cliVersion == "dev" {
		return true, true, nil
	}

	match := releasePattern.FindStringSubmatch(cliVersion)
	if match == nil {
		return false, false, fmt.Errorf("invalid CLI build version %q: want dev or vMAJOR.MINOR.PATCH", cliVersion)
	}
	current := numericVersion{normalize(match[1]), normalize(match[2]), normalize(match[3])}
	return compare(current, requirement.minimum) >= 0, false, nil
}

func normalize(component string) string {
	component = strings.TrimLeft(component, "0")
	if component == "" {
		return "0"
	}
	return component
}

func compare(left, right numericVersion) int {
	for index := range left {
		if len(left[index]) < len(right[index]) {
			return -1
		}
		if len(left[index]) > len(right[index]) {
			return 1
		}
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
