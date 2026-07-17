//go:build darwin

package paths

import (
	"errors"
	"syscall"
	"testing"
)

func TestMissingNameKey_QueryFailureIsIdentityUnavailable(t *testing.T) {
	queryErr := syscall.EINVAL
	_, err := missingNameKeyWithQuery(t.TempDir(), "missing", func(int) (int, error) {
		return 0, queryErr
	})
	if !errors.Is(err, ErrIdentityUnavailable) {
		t.Fatalf("missingNameKeyWithQuery() error = %v, want ErrIdentityUnavailable", err)
	}
	if !errors.Is(err, queryErr) {
		t.Fatalf("missingNameKeyWithQuery() error = %v, want query cause %v", err, queryErr)
	}
}
