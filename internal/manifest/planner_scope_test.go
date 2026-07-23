package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidatedProfileRenderScope_LoadsOnlyRequestedModule(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repository")
	control := writeGlobalControlFixture(t, home, repository)
	requestedRoot := filepath.Join(repository, "modules", "requested")
	unrequestedRoot := filepath.Join(repository, "modules", "unrequested")
	writeSourceFile(t, requestedRoot, "config", "literal requested\n")
	writeSourceFile(t, unrequestedRoot, "config", "literal unrequested\n")
	profile := testResolvedProfile(
		ResolvedModule{
			Name: "unrequested", SourceDir: unrequestedRoot, TargetRoot: "~/unrequested",
			FileRules: []ResolvedFileRule{{Source: "config", Kind: FileKindScaffold, Mode: "0644"}},
		},
		ResolvedModule{
			Name: "requested", SourceDir: requestedRoot, TargetRoot: "~/requested",
			FileRules: []ResolvedFileRule{{Source: "config", Kind: FileKindScaffold, Mode: "0644"}},
		},
	)

	validated, err := profile.ValidatePathBoundaries(control)
	if err != nil {
		t.Fatalf("ValidatePathBoundaries() error = %v", err)
	}
	before := snapshotTree(t, root)
	scoped, err := validated.RenderScope([]string{"requested"}, testRuntimeContext(home))
	if err != nil {
		t.Fatalf("RenderScope(requested) error = %v", err)
	}
	entries := scoped.Entries()
	if len(entries) != 1 || entries[0].Module != "requested" || string(entries[0].Content) != "literal requested\n" {
		t.Fatalf("RenderScope(requested) entries = %#v, want one loaded requested entry", entries)
	}
	if !reflect.DeepEqual(scoped.Modules(), []string{"requested"}) || scoped.Full() {
		t.Fatalf("RenderScope(requested) scope = modules %v full=%t", scoped.Modules(), scoped.Full())
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("RenderScope() changed fixture: before=%v after=%v", before, after)
	}

	entries[0].Content[0] = 'X'
	if again := scoped.Entries(); string(again[0].Content) != "literal requested\n" {
		t.Fatalf("mutating Entries() result changed scope: %#v", again)
	}
	if full := validated.Entries(); len(full) != 2 || full[0].Content != nil || full[1].Content != nil {
		t.Fatalf("validated structural entries changed by scoped render: %#v", full)
	}
	if full, err := validated.RenderScope(nil, testRuntimeContext(home)); err != nil || len(full.Entries()) != 2 {
		t.Fatalf("RenderScope(full) = (%#v, %v), want both literal scaffolds", full, err)
	}
}

func TestValidatedProfileRenderScope_RejectsDifferentValidatedHomeWithoutWrites(t *testing.T) {
	root := t.TempDir()
	validatedHome := filepath.Join(root, "home-a")
	otherHome := filepath.Join(root, "home-b")
	repository := filepath.Join(root, "repository")
	control := writeGlobalControlFixture(t, validatedHome, repository)
	if err := os.Mkdir(otherHome, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", otherHome, err)
	}
	moduleRoot := filepath.Join(repository, "modules", "app")
	writeSourceFile(t, moduleRoot, "config", "literal\n")
	writeSourceFile(t, moduleRoot, "hooks/setup", "#!/bin/sh\n")
	profile := testResolvedProfile(ResolvedModule{
		Name:       "app",
		SourceDir:  moduleRoot,
		TargetRoot: "~/app",
		RunOnce:    []string{"hooks/setup"},
	})
	validated, err := profile.ValidatePathBoundaries(control)
	if err != nil {
		t.Fatalf("ValidatePathBoundaries() error = %v", err)
	}
	before := snapshotTree(t, root)
	context := testRuntimeContext(otherHome)
	copyOfValidated := validated
	copyOfValidated.home = otherHome
	if validated.Home() != filepath.Clean(validatedHome) {
		t.Fatalf("mutating capability copy changed validated HOME: %q", validated.Home())
	}

	scoped, err := validated.RenderScope(nil, context)
	if err == nil || !strings.Contains(err.Error(), "does not match validated HOME") {
		t.Fatalf("RenderScope(other HOME) = (%#v, %v), want validated HOME mismatch", scoped, err)
	}
	if scoped.Entries() != nil || scoped.Hooks() != nil || scoped.Modules() != nil {
		t.Fatalf("RenderScope(other HOME) returned partial scope: %#v", scoped)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("RenderScope(other HOME) changed fixture: before=%v after=%v", before, after)
	}
}

func TestValidatedProfileRenderScope_ValidatesEffectiveModules(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repository")
	control := writeGlobalControlFixture(t, home, repository)
	moduleRoot := filepath.Join(repository, "modules", "app")
	writeSourceFile(t, moduleRoot, "config", "config")
	validated, err := testResolvedProfile(ResolvedModule{
		Name:       "app",
		SourceDir:  moduleRoot,
		TargetRoot: "~/app",
	}).ValidatePathBoundaries(control)
	if err != nil {
		t.Fatalf("ValidatePathBoundaries() error = %v", err)
	}

	if got := validated.Modules(); !reflect.DeepEqual(got, []string{"app"}) {
		t.Fatalf("ValidatedProfile.Modules() = %v, want [app]", got)
	}
	for _, requested := range [][]string{{"missing"}, {"app", "missing"}, {""}} {
		if scoped, err := validated.RenderScope(requested, testRuntimeContext(home)); err == nil {
			t.Fatalf("RenderScope(%v) = %#v, nil; want scope error", requested, scoped)
		}
	}

	scoped, err := validated.RenderScope([]string{"app", "app"}, testRuntimeContext(home))
	if err != nil {
		t.Fatalf("RenderScope(duplicate app) error = %v", err)
	}
	if !reflect.DeepEqual(scoped.Modules(), []string{"app"}) {
		t.Fatalf("RenderScope(duplicate app) modules = %v, want deduplicated [app]", scoped.Modules())
	}
}

func TestValidatedProfileRenderScope_ExposesStableM1HookDescriptors(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repository")
	control := writeGlobalControlFixture(t, home, repository)
	alphaRoot := filepath.Join(repository, "modules", "alpha")
	betaRoot := filepath.Join(repository, "modules", "beta")
	for _, path := range []string{
		filepath.Join(alphaRoot, "hooks", "second"),
		filepath.Join(alphaRoot, "hooks", "first"),
		filepath.Join(betaRoot, "hooks", "only"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", path, err)
		}
	}
	profile := testResolvedProfile(
		ResolvedModule{
			Name:       "beta",
			SourceDir:  betaRoot,
			TargetRoot: "~/beta",
			RunOnce:    []string{"hooks/only"},
		},
		ResolvedModule{
			Name:       "alpha",
			SourceDir:  alphaRoot,
			TargetRoot: "~/alpha",
			RunOnce:    []string{"hooks/second", "hooks/first"},
		},
	)
	validated, err := profile.ValidatePathBoundaries(control)
	if err != nil {
		t.Fatalf("ValidatePathBoundaries() error = %v", err)
	}
	scoped, err := validated.RenderScope(nil, testRuntimeContext(home))
	if err != nil {
		t.Fatalf("RenderScope(full) error = %v", err)
	}

	want := []HookDescriptor{
		{
			Module:         "alpha",
			ModulePath:     alphaRoot,
			Script:         "hooks/second",
			ScriptPath:     filepath.Join(alphaRoot, "hooks", "second"),
			TargetRoot:     "~/alpha",
			TargetRootPath: filepath.Join(home, "alpha"),
		},
		{
			Module:         "alpha",
			ModulePath:     alphaRoot,
			Script:         "hooks/first",
			ScriptPath:     filepath.Join(alphaRoot, "hooks", "first"),
			TargetRoot:     "~/alpha",
			TargetRootPath: filepath.Join(home, "alpha"),
		},
		{
			Module:         "beta",
			ModulePath:     betaRoot,
			Script:         "hooks/only",
			ScriptPath:     filepath.Join(betaRoot, "hooks", "only"),
			TargetRoot:     "~/beta",
			TargetRootPath: filepath.Join(home, "beta"),
		},
	}
	if got := scoped.Hooks(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ScopedProfile.Hooks() = %#v, want %#v", got, want)
	}
	if !scoped.Full() || !reflect.DeepEqual(scoped.Modules(), []string{"alpha", "beta"}) {
		t.Fatalf("full scope = modules %v full=%t", scoped.Modules(), scoped.Full())
	}

	alpha, err := validated.RenderScope([]string{"alpha"}, testRuntimeContext(home))
	if err != nil {
		t.Fatalf("RenderScope(alpha) error = %v", err)
	}
	if hooks := alpha.Hooks(); len(hooks) != 2 || hooks[0].Module != "alpha" || hooks[1].Module != "alpha" {
		t.Fatalf("RenderScope(alpha) hooks = %#v, want only alpha", hooks)
	}
}
