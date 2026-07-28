package config_test

import (
	"errors"
	"strings"
	"testing"

	coreconfig "github.com/mianm12/dotfiles/internal/core/config"
)

func TestInspectModulePortableApplicability(t *testing.T) {
	unknownPlatform := coreconfig.Platform{
		OS:     coreconfig.UnknownPlatformField("operating system is unavailable"),
		Distro: coreconfig.UnknownPlatformField("distribution is unavailable"),
		Arch:   coreconfig.UnknownPlatformField("architecture is unavailable"),
	}

	tests := []struct {
		name           string
		manifest       string
		platform       coreconfig.Platform
		want           coreconfig.ApplicabilityState
		wantDiagnostic string
	}{
		{
			name:     "unconstrained module ignores unknown platform fields",
			platform: unknownPlatform,
			want:     coreconfig.ApplicabilityApplicable,
		},
		{
			name: "known mismatch wins over an earlier unknown field",
			manifest: `
[match]
os = ["linux"]
arch = ["aarch64"]
`,
			platform: coreconfig.Platform{
				OS:     coreconfig.UnknownPlatformField("operating system is unavailable"),
				Distro: coreconfig.UnknownPlatformField("distribution is unavailable"),
				Arch:   coreconfig.KnownPlatformField("x86_64"),
			},
			want: coreconfig.ApplicabilityNotApplicable,
		},
		{
			name: "constrained unknown field is indeterminate",
			manifest: `
[match]
os = ["linux"]
distro = ["ubuntu"]
arch = ["x86_64"]
`,
			platform: coreconfig.Platform{
				OS:     coreconfig.KnownPlatformField("linux"),
				Distro: coreconfig.UnknownPlatformField("/etc/os-release is unreadable"),
				Arch:   coreconfig.KnownPlatformField("x86_64"),
			},
			want:           coreconfig.ApplicabilityIndeterminate,
			wantDiagnostic: "platform distro is unknown: /etc/os-release is unreadable",
		},
		{
			name: "all constrained fields known and matching are applicable",
			manifest: `
[match]
os = ["linux"]
distro = ["ubuntu"]
arch = ["x86_64"]
`,
			platform: testPlatform("linux", "ubuntu", "x86_64"),
			want:     coreconfig.ApplicabilityApplicable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			repositoryRoot := writeRepository(t, root, `
version = 1

[profiles]
base = ["app"]
`)
			writeModule(t, repositoryRoot, "app", test.manifest)

			repository, err := coreconfig.OpenRepository(repositoryRoot)
			if err != nil {
				t.Fatalf("OpenRepository() error = %v", err)
			}
			module, exists, applicability, err := repository.InspectModule("app", test.platform)
			if err != nil {
				t.Fatalf("InspectModule() error = %v", err)
			}
			if !exists {
				t.Fatal("InspectModule() exists = false, want true")
			}
			if applicability.State != test.want {
				t.Fatalf(
					"InspectModule() applicability = %#v, want state %q",
					applicability,
					test.want,
				)
			}
			if test.wantDiagnostic == "" {
				if applicability.Diagnostic != "" {
					t.Fatalf(
						"InspectModule() diagnostic = %q, want empty",
						applicability.Diagnostic,
					)
				}
			} else if !strings.Contains(applicability.Diagnostic, test.wantDiagnostic) {
				t.Fatalf(
					"InspectModule() diagnostic = %q, want containing %q",
					applicability.Diagnostic,
					test.wantDiagnostic,
				)
			}
			if test.want == coreconfig.ApplicabilityApplicable && module.ID != "app" {
				t.Fatalf("InspectModule() module = %#v, want app", module)
			}
		})
	}
}

func TestInspectModuleVariantApplicability(t *testing.T) {
	tests := []struct {
		name            string
		manifest        string
		platform        coreconfig.Platform
		want            coreconfig.ApplicabilityState
		wantDiagnostics []string
		wantErr         error
	}{
		{
			name: "definite match plus possible match is indeterminate",
			manifest: `
[variants.generic]
root = "."

[variants.generic.match]
os = ["linux"]
arch = ["x86_64"]

[variants.ubuntu]
root = "."

[variants.ubuntu.match]
os = ["linux"]
distro = ["ubuntu"]
`,
			platform: coreconfig.Platform{
				OS:     coreconfig.KnownPlatformField("linux"),
				Distro: coreconfig.UnknownPlatformField("/etc/os-release has no usable ID"),
				Arch:   coreconfig.KnownPlatformField("x86_64"),
			},
			want: coreconfig.ApplicabilityIndeterminate,
			wantDiagnostics: []string{
				`variant "generic" matches`,
				`variant "ubuntu" may match`,
				"platform distro is unknown",
			},
		},
		{
			name: "possible match without definite match is indeterminate",
			manifest: `
[variants.macos]
root = "."

[variants.macos.match]
os = ["macos"]

[variants.ubuntu]
root = "."

[variants.ubuntu.match]
os = ["linux"]
distro = ["ubuntu"]
`,
			platform: coreconfig.Platform{
				OS:     coreconfig.KnownPlatformField("linux"),
				Distro: coreconfig.UnknownPlatformField("/etc/os-release is missing"),
				Arch:   coreconfig.KnownPlatformField("x86_64"),
			},
			want: coreconfig.ApplicabilityIndeterminate,
			wantDiagnostics: []string{
				`variant "ubuntu" may match`,
				"platform distro is unknown",
			},
		},
		{
			name: "two definite matches remain invalid configuration",
			manifest: `
[variants.by-arch]
root = "."

[variants.by-arch.match]
arch = ["x86_64"]

[variants.by-os]
root = "."

[variants.by-os.match]
os = ["linux"]
`,
			platform: testPlatform("linux", "ubuntu", "x86_64"),
			wantErr:  coreconfig.ErrInvalidConfiguration,
		},
		{
			name: "all known mismatches are not applicable",
			manifest: `
[variants.macos]
root = "."

[variants.macos.match]
os = ["macos"]

[variants.ubuntu]
root = "."

[variants.ubuntu.match]
os = ["linux"]
distro = ["ubuntu"]
`,
			platform: testPlatform("linux", "gentoo", "x86_64"),
			want:     coreconfig.ApplicabilityNotApplicable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			repositoryRoot := writeRepository(t, root, `
version = 1

[profiles]
base = ["app"]
`)
			writeModule(t, repositoryRoot, "app", test.manifest)

			repository, err := coreconfig.OpenRepository(repositoryRoot)
			if err != nil {
				t.Fatalf("OpenRepository() error = %v", err)
			}
			module, exists, applicability, err := repository.InspectModule("app", test.platform)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("InspectModule() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("InspectModule() error = %v", err)
			}
			if !exists {
				t.Fatal("InspectModule() exists = false, want true")
			}
			if applicability.State != test.want {
				t.Fatalf(
					"InspectModule() applicability = %#v, want state %q",
					applicability,
					test.want,
				)
			}
			for _, diagnostic := range test.wantDiagnostics {
				if !strings.Contains(applicability.Diagnostic, diagnostic) {
					t.Fatalf(
						"InspectModule() diagnostic = %q, want containing %q",
						applicability.Diagnostic,
						diagnostic,
					)
				}
			}
			if module.Variant != "" {
				t.Fatalf(
					"InspectModule() selected variant = %q, want none for state %q",
					module.Variant,
					test.want,
				)
			}
		})
	}
}
