//go:build linux

package paths

import "fmt"

func missingNameKey(parent, _ string) (string, error) {
	return "", fmt.Errorf("%w: missing-name rules for %q are not exposed by a generic read-only API", ErrIdentityUnavailable, parent)
}
