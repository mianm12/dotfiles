package cli

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/config"
)

func TestDetectPlatformDarwinDoesNotReadOSRelease(t *testing.T) {
	t.Parallel()

	platform := detectPlatform(
		"darwin",
		"arm64",
		func(path string) ([]byte, error) {
			t.Fatalf("readFile(%q) was called on macOS", path)
			return nil, nil
		},
	)

	assertKnownPlatformField(t, "OS", platform.OS, "macos")
	assertKnownPlatformField(t, "Arch", platform.Arch, "aarch64")
	assertUnknownPlatformField(
		t,
		"Distro",
		platform.Distro,
		"only detected on Linux",
	)
}

func TestDetectPlatformReadsLinuxDistributionID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "Ubuntu unquoted",
			data: "NAME=Ubuntu\nID=ubuntu\n",
			want: "ubuntu",
		},
		{
			name: "Ubuntu double quoted",
			data: "NAME=\"Ubuntu\"\nID=\"ubuntu\"\n",
			want: "ubuntu",
		},
		{
			name: "Arch single quoted",
			data: "NAME=\"Arch Linux\"\nID='arch'\n",
			want: "arch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			readCount := 0
			platform := detectPlatform(
				"linux",
				"amd64",
				func(path string) ([]byte, error) {
					readCount++
					if path != "/etc/os-release" {
						t.Fatalf("readFile path = %q, want /etc/os-release", path)
					}
					return []byte(test.data), nil
				},
			)

			if readCount != 1 {
				t.Fatalf("readFile call count = %d, want 1", readCount)
			}
			assertKnownPlatformField(t, "OS", platform.OS, "linux")
			assertKnownPlatformField(t, "Distro", platform.Distro, test.want)
			assertKnownPlatformField(t, "Arch", platform.Arch, "x86_64")
		})
	}
}

func TestDetectPlatformReportsUnknownLinuxDistribution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		data           string
		readErr        error
		wantDiagnostic string
	}{
		{
			name:           "missing file",
			readErr:        fs.ErrNotExist,
			wantDiagnostic: "read /etc/os-release",
		},
		{
			name:           "unreadable file",
			readErr:        fs.ErrPermission,
			wantDiagnostic: "permission denied",
		},
		{
			name:           "missing ID",
			data:           "NAME=Linux\n",
			wantDiagnostic: "ID is missing",
		},
		{
			name:           "empty ID",
			data:           "ID=\n",
			wantDiagnostic: "ID is empty",
		},
		{
			name:           "empty quoted ID",
			data:           "ID=\"\"\n",
			wantDiagnostic: "ID is empty",
		},
		{
			name:           "duplicate ID",
			data:           "ID=ubuntu\nID=arch\n",
			wantDiagnostic: "ID is declared more than once",
		},
		{
			name:           "unmatched quote",
			data:           "ID=\"ubuntu\n",
			wantDiagnostic: "ID has an unmatched quote",
		},
		{
			name:           "unsupported whitespace",
			data:           "ID=ubuntu linux\n",
			wantDiagnostic: "unsupported quoting or whitespace",
		},
		{
			name:           "whitespace before equals",
			data:           "ID =ubuntu\n",
			wantDiagnostic: "ID is missing",
		},
		{
			name:           "whitespace after equals",
			data:           "ID= ubuntu\n",
			wantDiagnostic: "unsupported quoting or whitespace",
		},
		{
			name:           "non-lowercase token",
			data:           "ID=Ubuntu\n",
			wantDiagnostic: "must be a lowercase token",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			readCount := 0
			platform := detectPlatform(
				"linux",
				"amd64",
				func(path string) ([]byte, error) {
					readCount++
					if path != "/etc/os-release" {
						t.Fatalf("readFile path = %q, want /etc/os-release", path)
					}
					return []byte(test.data), test.readErr
				},
			)

			if readCount != 1 {
				t.Fatalf("readFile call count = %d, want 1", readCount)
			}
			assertKnownPlatformField(t, "OS", platform.OS, "linux")
			assertUnknownPlatformField(
				t,
				"Distro",
				platform.Distro,
				test.wantDiagnostic,
			)
			assertKnownPlatformField(t, "Arch", platform.Arch, "x86_64")
		})
	}
}

func TestDetectPlatformReportsUnavailableLinuxReader(t *testing.T) {
	t.Parallel()

	platform := detectPlatform("linux", "amd64", nil)

	assertKnownPlatformField(t, "OS", platform.OS, "linux")
	assertUnknownPlatformField(t, "Distro", platform.Distro, "reader is unavailable")
	assertKnownPlatformField(t, "Arch", platform.Arch, "x86_64")
}

func TestDetectPlatformPreservesUnknownRuntimeEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		goos               string
		goarch             string
		wantOSDiagnostic   string
		wantArchDiagnostic string
		wantArch           string
	}{
		{
			name:             "unsupported OS",
			goos:             "plan9",
			goarch:           "riscv64",
			wantOSDiagnostic: "unsupported",
			wantArch:         "riscv64",
		},
		{
			name:               "empty OS and architecture",
			wantOSDiagnostic:   "GOOS is empty",
			wantArchDiagnostic: "GOARCH is empty",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			platform := detectPlatform(
				test.goos,
				test.goarch,
				func(path string) ([]byte, error) {
					t.Fatalf("readFile(%q) was called for GOOS %q", path, test.goos)
					return nil, nil
				},
			)

			assertUnknownPlatformField(
				t,
				"OS",
				platform.OS,
				test.wantOSDiagnostic,
			)
			assertUnknownPlatformField(
				t,
				"Distro",
				platform.Distro,
				"only detected on Linux",
			)
			if test.wantArchDiagnostic != "" {
				assertUnknownPlatformField(
					t,
					"Arch",
					platform.Arch,
					test.wantArchDiagnostic,
				)
				return
			}
			assertKnownPlatformField(t, "Arch", platform.Arch, test.wantArch)
		})
	}
}

func TestNormalizeArchitecture(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: "amd64", want: "x86_64"},
		{input: "arm64", want: "aarch64"},
		{input: "386", want: "386"},
		{input: "RISCV64", want: "riscv64"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()

			if got := normalizeArchitecture(test.input); got != test.want {
				t.Fatalf(
					"normalizeArchitecture(%q) = %q, want %q",
					test.input,
					got,
					test.want,
				)
			}
		})
	}
}

func FuzzOSReleaseID(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("ID=ubuntu\n"),
		[]byte("ID=\"ubuntu\"\n"),
		[]byte("ID='arch'\n"),
		[]byte("NAME=Linux\n"),
		[]byte("ID=ubuntu\nID=arch\n"),
		[]byte("ID=\"ubuntu\n"),
		{0, 1, 2, '\n'},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		distro, err := osReleaseID(data)
		if err != nil {
			if distro != "" {
				t.Fatalf("osReleaseID() = (%q, %v), want empty result on error", distro, err)
			}
			return
		}
		if distro == "" {
			t.Fatal("osReleaseID() succeeded with an empty ID")
		}
		parsed, err := parseOSReleaseID(distro)
		if err != nil || parsed != distro {
			t.Fatalf(
				"osReleaseID() returned non-canonical ID %q: parse = (%q, %v)",
				distro,
				parsed,
				err,
			)
		}
	})
}

func assertKnownPlatformField(
	t *testing.T,
	name string,
	field config.PlatformField,
	want string,
) {
	t.Helper()

	if !field.Known {
		t.Fatalf("%s.Known = false, diagnostic = %q", name, field.Diagnostic)
	}
	if field.Value != want {
		t.Fatalf("%s.Value = %q, want %q", name, field.Value, want)
	}
	if field.Diagnostic != "" {
		t.Fatalf("%s.Diagnostic = %q, want empty", name, field.Diagnostic)
	}
}

func assertUnknownPlatformField(
	t *testing.T,
	name string,
	field config.PlatformField,
	wantDiagnostic string,
) {
	t.Helper()

	if field.Known {
		t.Fatalf("%s.Known = true, value = %q", name, field.Value)
	}
	if field.Value != "" {
		t.Fatalf("%s.Value = %q, want empty", name, field.Value)
	}
	if !strings.Contains(field.Diagnostic, wantDiagnostic) {
		t.Fatalf(
			"%s.Diagnostic = %q, want substring %q",
			name,
			field.Diagnostic,
			wantDiagnostic,
		)
	}
}
