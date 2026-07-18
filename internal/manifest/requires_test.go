package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRequirement(t *testing.T) {
	repo := t.TempDir()
	// 宽松预读只解释 requires；未知字段留给完整 manifest loader 校验。
	content := `
requires = ">=0.3.0"

[future]
unknown = "allowed during pre-read"
`
	manifestPath := filepath.Join(repo, "dot.toml")
	if err := os.WriteFile(manifestPath, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", manifestPath, err)
	}

	got, err := ReadRequirement(repo)
	if err != nil {
		t.Fatalf("ReadRequirement() error = %v, want nil", err)
	}
	want := ">=0.3.0"
	if got.String() != want {
		t.Fatalf("ReadRequirement().String() = %q, want %q", got.String(), want)
	}
}

func TestReadRequirement_RepositoryUnavailable(t *testing.T) {
	_, err := ReadRequirement(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, ErrRepositoryUnavailable) {
		t.Fatalf("ReadRequirement() error = %v, want ErrRepositoryUnavailable", err)
	}
}

func TestReadRequirement_RejectsDanglingRepositorySymlink(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Symlink(filepath.Join(root, "missing"), repo); err != nil {
		t.Fatalf("os.Symlink(%q) error = %v", repo, err)
	}

	_, err := ReadRequirement(repo)
	if err == nil {
		t.Fatal("ReadRequirement() error = nil, want dangling symlink error")
	}
	if errors.Is(err, ErrRepositoryUnavailable) {
		t.Fatalf("ReadRequirement() error = %v, want read error", err)
	}
}

func TestReadRequirement_RejectsInvalidManifest(t *testing.T) {
	tests := []struct {
		name                   string
		content                string
		wantInvalidRequirement bool
	}{
		{name: "missing requires", content: `[profiles]`, wantInvalidRequirement: true},
		{name: "wrong type", content: `requires = 3`},
		{name: "unsupported syntax", content: `requires = "^0.3.0"`, wantInvalidRequirement: true},
		{name: "v prefix", content: `requires = ">=v0.3.0"`, wantInvalidRequirement: true},
		{name: "invalid toml", content: `requires =`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			manifestPath := filepath.Join(repo, "dot.toml")
			if err := os.WriteFile(manifestPath, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("os.WriteFile(%q) error = %v", manifestPath, err)
			}
			_, err := ReadRequirement(repo)
			if err == nil {
				t.Fatal("ReadRequirement() error = nil, want error")
			}
			if !strings.Contains(err.Error(), manifestPath) {
				t.Fatalf("ReadRequirement() error = %q, want manifest path %q", err, manifestPath)
			}
			if got := errors.Is(err, ErrInvalidRequirement); got != tt.wantInvalidRequirement {
				t.Fatalf("errors.Is(ReadRequirement(), ErrInvalidRequirement) = %v, want %v", got, tt.wantInvalidRequirement)
			}
		})
	}
}

func TestSatisfies(t *testing.T) {
	tests := []struct {
		name                 string
		cli                  string
		requires             string
		wantSatisfied        bool
		wantDevelopmentBuild bool
		wantErr              bool
	}{
		{name: "zero minimum", cli: "v0.0.0", requires: ">=0.0.0", wantSatisfied: true},
		{name: "equal", cli: "v1.2.3", requires: ">=1.2.3", wantSatisfied: true},
		{name: "newer minor", cli: "v1.3.0", requires: ">=1.2.9", wantSatisfied: true},
		{name: "older", cli: "v1.2.2", requires: ">=1.2.3"},
		{name: "longer major component", cli: "v10.0.0", requires: ">=9.9.9", wantSatisfied: true},
		{name: "shorter major component", cli: "v9.9.9", requires: ">=10.0.0"},
		{name: "development", cli: "dev", requires: ">=999.0.0", wantSatisfied: true, wantDevelopmentBuild: true},
		{
			name:          "components beyond integer range",
			cli:           "v999999999999999999999.0.0",
			requires:      ">=999999999999999999998.9.9",
			wantSatisfied: true,
		},
		{name: "invalid build version", cli: "1.2.3", requires: ">=1.0.0", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requirement, err := ParseRequirement(tt.requires)
			if err != nil {
				t.Fatalf("ParseRequirement() error = %v, want nil", err)
			}
			satisfied, developmentBuild, err := Satisfies(tt.cli, requirement)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Satisfies() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Satisfies() error = %v, want nil", err)
			}
			if satisfied != tt.wantSatisfied || developmentBuild != tt.wantDevelopmentBuild {
				t.Fatalf("Satisfies() = (%v, %v), want (%v, %v)", satisfied, developmentBuild, tt.wantSatisfied, tt.wantDevelopmentBuild)
			}
		})
	}
}

func TestSatisfies_RejectsZeroRequirement(t *testing.T) {
	for _, cliVersion := range []string{"v1.2.3", "dev"} {
		t.Run(cliVersion, func(t *testing.T) {
			_, _, err := Satisfies(cliVersion, Requirement{})
			if err == nil {
				t.Fatal("Satisfies() error = nil, want zero-value requirement error")
			}
		})
	}
}
