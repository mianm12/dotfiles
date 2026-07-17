//go:build darwin

package paths

import (
	"errors"
	"syscall"
	"testing"
)

func TestMissingNameKeyWithQuery_NameSemantics(t *testing.T) {
	parent := t.TempDir()
	tests := []struct {
		name            string
		input           string
		caseSensitivity int
		want            string
		wantUnavailable bool
	}{
		{
			name:            "case sensitive preserves spelling",
			input:           "Missing-CASE_123",
			caseSensitivity: 1,
			want:            "Missing-CASE_123",
		},
		{
			name:            "case insensitive folds ASCII",
			input:           "Missing-CASE_123",
			caseSensitivity: 0,
			want:            "missing-case_123",
		},
		{
			name:            "Unicode is unavailable",
			input:           "missing-\u00e9",
			caseSensitivity: 0,
			wantUnavailable: true,
		},
		{
			name:            "unknown capability is unavailable",
			input:           "missing",
			caseSensitivity: 2,
			wantUnavailable: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := missingNameKeyWithQuery(parent, test.input, func(int) (int, error) {
				return test.caseSensitivity, nil
			})
			if test.wantUnavailable {
				if !errors.Is(err, ErrIdentityUnavailable) {
					t.Fatalf("missingNameKeyWithQuery() error = %v, want ErrIdentityUnavailable", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("missingNameKeyWithQuery() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("missingNameKeyWithQuery() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMissingNameKeyWithQuery_QueryFailureIsIdentityUnavailable(t *testing.T) {
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
