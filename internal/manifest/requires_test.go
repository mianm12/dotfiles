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
	content := `
requires = ">=0.3.0"

[future]
unknown = "allowed during pre-read"
`
	if err := os.WriteFile(filepath.Join(repo, "dot.toml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	got, err := ReadRequirement(repo)
	if err != nil {
		t.Fatalf("ReadRequirement() error = %v", err)
	}
	if got.String() != ">=0.3.0" {
		t.Fatalf("ReadRequirement().String() = %q", got.String())
	}
}

func TestReadRequirementRepositoryUnavailable(t *testing.T) {
	_, err := ReadRequirement(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, ErrRepositoryUnavailable) {
		t.Fatalf("ReadRequirement() error = %v, want ErrRepositoryUnavailable", err)
	}
}

func TestReadRequirementRejectsInvalidManifest(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "missing requires", content: `[profiles]`},
		{name: "wrong type", content: `requires = 3`},
		{name: "unsupported syntax", content: `requires = "^0.3.0"`},
		{name: "v prefix", content: `requires = ">=v0.3.0"`},
		{name: "invalid toml", content: `requires =`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			manifestPath := filepath.Join(repo, "dot.toml")
			if err := os.WriteFile(manifestPath, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			_, err := ReadRequirement(repo)
			if err == nil {
				t.Fatal("ReadRequirement() error = nil, want error")
			}
			if !strings.Contains(err.Error(), manifestPath) {
				t.Fatalf("ReadRequirement() error = %q, want manifest path %q", err, manifestPath)
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
		{name: "development", cli: "dev", requires: ">=999.0.0", wantSatisfied: true, wantDevelopmentBuild: true},
		{name: "large components", cli: "v999999999999999999999.0.0", requires: ">=999999999999999999998.9.9", wantSatisfied: true},
		{name: "invalid build version", cli: "1.2.3", requires: ">=1.0.0", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requirement, err := ParseRequirement(tt.requires)
			if err != nil {
				t.Fatalf("ParseRequirement() error = %v", err)
			}
			satisfied, developmentBuild, err := Satisfies(tt.cli, requirement)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Satisfies() error = %v, wantErr %v", err, tt.wantErr)
			}
			if satisfied != tt.wantSatisfied || developmentBuild != tt.wantDevelopmentBuild {
				t.Fatalf("Satisfies() = (%v, %v), want (%v, %v)", satisfied, developmentBuild, tt.wantSatisfied, tt.wantDevelopmentBuild)
			}
		})
	}
}

func TestSatisfiesRejectsZeroRequirement(t *testing.T) {
	for _, cliVersion := range []string{"v1.2.3", "dev"} {
		t.Run(cliVersion, func(t *testing.T) {
			_, _, err := Satisfies(cliVersion, Requirement{})
			if err == nil {
				t.Fatal("Satisfies() error = nil, want zero-value requirement error")
			}
		})
	}
}
