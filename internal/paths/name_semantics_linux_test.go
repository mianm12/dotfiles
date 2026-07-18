//go:build linux && (amd64 || arm64)

package paths

import (
	"errors"
	"os"
	"testing"
)

func TestClassifyLinuxMissingName(t *testing.T) {
	tests := []struct {
		name           string
		filesystemType int64
		flags          uintptr
		input          string
		want           string
		wantError      bool
	}{
		{
			name:           "ext-family byte-sensitive ASCII",
			filesystemType: extFilesystemMagic,
			input:          "Config",
			want:           "Config",
		},
		{
			name:           "ext-family byte-sensitive invalid UTF-8",
			filesystemType: extFilesystemMagic,
			input:          string([]byte{0xff, 'x'}),
			want:           string([]byte{0xff, 'x'}),
		},
		{
			name:           "Btrfs byte-sensitive name",
			filesystemType: btrfsFilesystemMagic,
			input:          "Config",
			want:           "Config",
		},
		{
			name:           "casefold directory",
			filesystemType: extFilesystemMagic,
			flags:          fsCasefoldFlag,
			input:          "config",
			wantError:      true,
		},
		{
			name:           "Btrfs synthetic casefold directory",
			filesystemType: btrfsFilesystemMagic,
			flags:          fsCasefoldFlag,
			input:          "config",
			wantError:      true,
		},
		{
			name:           "unknown filesystem",
			filesystemType: 0x1234,
			input:          "config",
			wantError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := classifyLinuxMissingName(tt.filesystemType, tt.flags, tt.input)
			if tt.wantError {
				if !errors.Is(err, ErrIdentityUnavailable) {
					t.Fatalf("classifyLinuxMissingName() error = %v, want ErrIdentityUnavailable", err)
				}
				if got != "" {
					t.Fatalf("classifyLinuxMissingName() = %q, want empty", got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("classifyLinuxMissingName() = %q, %v; want %q, nil", got, err, tt.want)
			}
		})
	}
}

func TestMissingNameKeyWithQuery(t *testing.T) {
	parent := t.TempDir()
	tests := []struct {
		name      string
		query     func(*os.File) (int64, uintptr, error)
		wantError bool
	}{
		{
			name: "success",
			query: func(directory *os.File) (int64, uintptr, error) {
				if directory.Name() != parent {
					t.Fatalf("query directory = %q, want %q", directory.Name(), parent)
				}
				return extFilesystemMagic, 0, nil
			},
		},
		{
			name: "query failure",
			query: func(*os.File) (int64, uintptr, error) {
				return 0, 0, os.ErrPermission
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := missingNameKeyWithQuery(parent, "missing", tt.query)
			if tt.wantError {
				if !errors.Is(err, ErrIdentityUnavailable) || !errors.Is(err, os.ErrPermission) {
					t.Fatalf("missingNameKeyWithQuery() error = %v, want wrapped errors", err)
				}
				if got != "" {
					t.Fatalf("missingNameKeyWithQuery() = %q, want empty", got)
				}
				return
			}
			if err != nil || got != "missing" {
				t.Fatalf("missingNameKeyWithQuery() = %q, %v; want missing, nil", got, err)
			}
		})
	}

	missingParent := parent + "/absent"
	if got, err := missingNameKeyWithQuery(missingParent, "child", nil); got != "" || err == nil {
		t.Fatalf("missingNameKeyWithQuery(missing parent) = %q, %v; want open error", got, err)
	}
}
