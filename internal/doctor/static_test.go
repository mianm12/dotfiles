package doctor

import (
	"context"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCheckManifest_ProfilesRemainIndependentAndUnassignedStayLocal(t *testing.T) {
	root, repo := newGitRepository(t)
	writeFile(t, filepath.Join(repo, "dot.toml"), `
requires = ">=0.1.0"
[profiles]
alpha = ["alpha"]
beta = ["beta"]
`)
	for _, module := range []string{"alpha", "beta", "unassigned"} {
		writeFile(t, filepath.Join(repo, "modules", module, "dot.toml"), `target = "~/shared"`)
		writeFile(t, filepath.Join(repo, "modules", module, "same"), module)
	}

	result := CheckManifest(context.Background(), manifestOptions(t, root, repo, "v0.1.0"))
	if result.ExitCode() != 0 || len(result.Findings()) != 0 {
		t.Fatalf("CheckManifest() findings = %#v, exit %d; want independently valid profiles", result.Findings(), result.ExitCode())
	}
}

func TestCheckManifest_ProfileSelectionOnlyNarrowsProfileBoundaries(t *testing.T) {
	root, repo := newGitRepository(t)
	writeFile(t, filepath.Join(repo, "dot.toml"), `
requires = ">=0.1.0"
[profiles]
good = ["good"]
bad = ["left", "right"]
`)
	writeFile(t, filepath.Join(repo, "modules", "good", "file"), "good")
	for _, module := range []string{"left", "right"} {
		writeFile(t, filepath.Join(repo, "modules", module, "dot.toml"), `target = "~/collision"`)
		writeFile(t, filepath.Join(repo, "modules", module, "file"), module)
	}

	all := CheckManifest(context.Background(), manifestOptions(t, root, repo, "v0.1.0"))
	if got := findingChecks(all); !reflect.DeepEqual(got, []string{"manifest.profile"}) {
		t.Fatalf("all-profile checks = %v, want bad profile collision; findings = %#v", got, all.Findings())
	}

	selectedOptions := manifestOptions(t, root, repo, "v0.1.0")
	selectedOptions.Profile = "good"
	selected := CheckManifest(context.Background(), selectedOptions)
	if selected.ExitCode() != 0 || len(selected.Findings()) != 0 {
		t.Fatalf("selected profile findings = %#v, exit %d; want clean", selected.Findings(), selected.ExitCode())
	}
}

func TestCheckManifest_ExplicitProfileStillChecksAllModuleLocalRules(t *testing.T) {
	root, repo := newGitRepository(t)
	writeFile(t, filepath.Join(repo, "dot.toml"), `
requires = ">=0.1.0"
[profiles]
selected = ["selected"]
other = ["other"]
`)
	writeFile(t, filepath.Join(repo, "modules", "selected", "file"), "selected")
	otherOS := "darwin"
	if runtime.GOOS == "darwin" {
		otherOS = "linux"
	}
	writeFile(t, filepath.Join(repo, "modules", "other", "dot.toml"),
		"target = { "+otherOS+" = \"~/other\" }\n")
	writeFile(t, filepath.Join(repo, "modules", "other", "broken.template"), `{{ printf "not allowed" }}`)

	options := manifestOptions(t, root, repo, "v0.1.0")
	options.Profile = "selected"
	result := CheckManifest(context.Background(), options)
	want := []string{"manifest.modules", "manifest.templates"}
	if got := findingChecks(result); !reflect.DeepEqual(got, want) {
		t.Fatalf("selected-profile checks = %v, want %v; findings = %#v", got, want, result.Findings())
	}
}

func TestCheckManifest_InvalidLocalTargetOutsideEffectiveProfiles(t *testing.T) {
	otherOS := "darwin"
	if runtime.GOOS == "darwin" {
		otherOS = "linux"
	}
	tests := []struct {
		name     string
		profiles string
		module   string
	}{
		{name: "unassigned", profiles: "base = []\n"},
		{name: "inactive", profiles: "base = [\"app\"]\n", module: "os = [\"" + otherOS + "\"]\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, repo := newGitRepository(t)
			writeFile(t, filepath.Join(repo, "dot.toml"),
				"requires = \">=0.1.0\"\n[profiles]\n"+tt.profiles)
			if tt.module != "" {
				writeFile(t, filepath.Join(repo, "modules", "app", "dot.toml"), tt.module)
			}
			writeFile(t, filepath.Join(repo, "modules", "app", ".template"), "value")

			result := CheckManifest(context.Background(), manifestOptions(t, root, repo, "v0.1.0"))
			if got := findingChecks(result); !reflect.DeepEqual(got, []string{"manifest.templates"}) {
				t.Fatalf("CheckManifest() checks = %v, want invalid module-local target; findings = %#v", got, result.Findings())
			}
			if text := findingsText(result); !strings.Contains(text, "empty target basename") {
				t.Fatalf("CheckManifest() findings = %q, want empty target basename", text)
			}
		})
	}
}

func TestCheckManifest_ProfileResolveErrorsPreserveProfileProvenance(t *testing.T) {
	root, repo := newGitRepository(t)
	writeFile(t, filepath.Join(repo, "dot.toml"), `
requires = ">=0.1.0"
[profiles]
alpha = ["app"]
beta = ["app"]
`)
	otherOS := "darwin"
	if runtime.GOOS == "darwin" {
		otherOS = "linux"
	}
	writeFile(t, filepath.Join(repo, "modules", "app", "dot.toml"),
		"target = { "+otherOS+" = \"~/app\" }\n")
	writeFile(t, filepath.Join(repo, "modules", "app", "file"), "app")

	all := CheckManifest(context.Background(), manifestOptions(t, root, repo, "v0.1.0"))
	if got := findingChecks(all); !reflect.DeepEqual(got, []string{
		"manifest.modules",
		"manifest.profile",
		"manifest.profile",
	}) {
		t.Fatalf("all-profile checks = %v, want module root cause and two profile impacts; findings = %#v", got, all.Findings())
	}
	allFindings := all.Findings()
	for index, profile := range []string{"alpha", "beta"} {
		if want := "profile \"" + profile + "\" resolve:"; !strings.Contains(allFindings[index+1].Message, want) {
			t.Errorf("all-profile finding[%d] = %q, want containing %q", index+1, allFindings[index+1].Message, want)
		}
	}

	selectedOptions := manifestOptions(t, root, repo, "v0.1.0")
	selectedOptions.Profile = "beta"
	selected := CheckManifest(context.Background(), selectedOptions)
	if got := findingChecks(selected); !reflect.DeepEqual(got, []string{
		"manifest.modules",
		"manifest.profile",
	}) {
		t.Fatalf("selected-profile checks = %v, want module root cause and selected profile impact; findings = %#v", got, selected.Findings())
	}
	selectedMessage := selected.Findings()[1].Message
	if !strings.Contains(selectedMessage, `profile "beta" resolve:`) || strings.Contains(selectedMessage, `profile "alpha"`) {
		t.Fatalf("selected-profile finding = %q, want only beta provenance", selectedMessage)
	}
}

func TestCheckManifest_UnsatisfiedRequirementStillRunsTemplatesAndPaths(t *testing.T) {
	root, repo := newGitRepository(t)
	writeFile(t, filepath.Join(repo, "dot.toml"), `
requires = ">=2.0.0"
[profiles]
base = ["left", "right"]
`)
	for _, module := range []string{"left", "right"} {
		writeFile(t, filepath.Join(repo, "modules", module, "dot.toml"), `target = "~/collision"`)
	}
	writeFile(t, filepath.Join(repo, "modules", "left", "broken.template"), `{{ .undeclared }}`)
	writeFile(t, filepath.Join(repo, "modules", "right", "broken"), "right")

	result := CheckManifest(context.Background(), manifestOptions(t, root, repo, "v1.0.0"))
	want := []string{"manifest.profile", "manifest.requires", "manifest.templates"}
	if got := findingChecks(result); !reflect.DeepEqual(got, want) {
		t.Fatalf("CheckManifest() checks = %v, want %v; findings = %#v", got, want, result.Findings())
	}
}

func TestCheckManifest_ProfilePathBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, repo string)
		wantText string
	}{
		{
			name: "ancestor target",
			setup: func(t *testing.T, repo string) {
				writeFile(t, filepath.Join(repo, "modules", "parent", "dot.toml"),
					"target = \"~\"\n[files.file]\ntarget = \"~/tree\"\n")
				writeFile(t, filepath.Join(repo, "modules", "parent", "file"), "parent")
				writeFile(t, filepath.Join(repo, "modules", "child", "dot.toml"),
					"target = \"~\"\n[files.file]\ntarget = \"~/tree/child\"\n")
				writeFile(t, filepath.Join(repo, "modules", "child", "file"), "child")
			},
			wantText: "ancestor",
		},
		{
			name: "control plane",
			setup: func(t *testing.T, repo string) {
				writeFile(t, filepath.Join(repo, "modules", "app", "dot.toml"), `target = "~/.config/dot"`)
				writeFile(t, filepath.Join(repo, "modules", "app", "config.toml"), "config")
			},
			wantText: "control",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, repo := newGitRepository(t)
			modules := "[\"parent\", \"child\"]"
			if tt.name == "control plane" {
				modules = "[\"app\"]"
			}
			writeFile(t, filepath.Join(repo, "dot.toml"),
				"requires = \">=0.1.0\"\n[profiles]\nbase = "+modules+"\n")
			tt.setup(t, repo)

			result := CheckManifest(context.Background(), manifestOptions(t, root, repo, "v0.1.0"))
			if got := findingChecks(result); !reflect.DeepEqual(got, []string{"manifest.profile"}) {
				t.Fatalf("CheckManifest() checks = %v, want profile boundary error; findings = %#v", got, result.Findings())
			}
			if text := findingsText(result); !strings.Contains(text, tt.wantText) {
				t.Fatalf("CheckManifest() findings = %q, want containing %q", text, tt.wantText)
			}
		})
	}
}

func TestCheckManifest_UnknownSelectedProfileIsError(t *testing.T) {
	root, repo := newGitRepository(t)
	writeFile(t, filepath.Join(repo, "dot.toml"), "requires = \">=0.1.0\"\n[profiles]\nbase = []\n")
	options := manifestOptions(t, root, repo, "v0.1.0")
	options.Profile = "missing"

	result := CheckManifest(context.Background(), options)
	if got := findingChecks(result); !reflect.DeepEqual(got, []string{"manifest.profile"}) {
		t.Fatalf("CheckManifest() checks = %v, want unknown profile error", got)
	}
}

func TestCheckManifest_ControlPlaneTopologyIsCheckedOnce(t *testing.T) {
	root, repo := newGitRepository(t)
	writeFile(t, filepath.Join(repo, "dot.toml"),
		"requires = \">=0.1.0\"\n[profiles]\nalpha = []\nbeta = []\n")
	options := manifestOptions(t, root, repo, "v0.1.0")
	options.Config = repo

	result := CheckManifest(context.Background(), options)
	if got := findingChecks(result); !reflect.DeepEqual(got, []string{"paths.control"}) {
		t.Fatalf("CheckManifest() checks = %v, want one global control-plane error; findings = %#v", got, result.Findings())
	}
}

func findingChecks(result Result) []string {
	checks := make([]string, 0, len(result.Findings()))
	for _, finding := range result.Findings() {
		checks = append(checks, finding.Check)
	}
	return checks
}
