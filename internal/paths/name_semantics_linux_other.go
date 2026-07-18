//go:build linux && !amd64 && !arm64

package paths

import "fmt"

func missingNameKey(parent, _ string) (string, error) {
	return "", fmt.Errorf(
		"%w: missing-name rules for %q are unavailable on this Linux architecture",
		ErrIdentityUnavailable,
		parent,
	)
}
