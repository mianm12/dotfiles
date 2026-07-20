package paths

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func TestValidateTargetSet_Valid(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("fixture\n"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", path, err)
		}
	}
	inputs := []LabeledTarget{
		{Label: "module alpha source first", Path: first},
		{Label: "module beta source second", Path: second},
	}
	before := snapshotFixtureTree(t, root)

	validated, err := ValidateTargetSet(inputs)
	if err != nil {
		t.Fatalf("ValidateTargetSet() error = %v", err)
	}
	if len(validated.targets) != len(inputs) {
		t.Fatalf("validated target count = %d, want %d", len(validated.targets), len(inputs))
	}
	if after := snapshotFixtureTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("ValidateTargetSet() changed target tree: before=%v after=%v", before, after)
	}
	inputs[0].Label = "changed"
	if validated.targets[0].input.Label == inputs[0].Label {
		t.Fatal("mutating inputs changed validated target provenance")
	}
	if empty, err := ValidateTargetSet(nil); err != nil || len(empty.targets) != 0 {
		t.Fatalf("ValidateTargetSet(nil) = (%#v, %v), want empty success", empty, err)
	}
}

func TestValidateTargetSet_RejectsEqualIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "same")
	if err := os.WriteFile(path, []byte("fixture\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	assertTargetSetOverlap(
		t,
		[]LabeledTarget{
			{Label: "module alpha source first", Path: path},
			{Label: "module beta source second", Path: filepath.Join(filepath.Dir(path), ".", filepath.Base(path))},
		},
		TargetRelationEqual,
	)
}

func TestValidateTargetSet_RejectsAncestorsInEitherOrder(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", child, err)
	}

	tests := []struct {
		name         string
		inputs       []LabeledTarget
		wantRelation TargetRelation
	}{
		{
			name: "parent first",
			inputs: []LabeledTarget{
				{Label: "parent target", Path: parent},
				{Label: "child target", Path: child},
			},
			wantRelation: TargetRelationLeftAncestor,
		},
		{
			name: "child first",
			inputs: []LabeledTarget{
				{Label: "child target", Path: child},
				{Label: "parent target", Path: parent},
			},
			wantRelation: TargetRelationRightAncestor,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertTargetSetOverlap(t, test.inputs, test.wantRelation)
		})
	}
}

func TestValidateTargetSet_RejectsSymlinkTraversalAncestors(t *testing.T) {
	t.Run("leaf symlink and displayed child", func(t *testing.T) {
		root := t.TempDir()
		realDirectory := filepath.Join(root, "real")
		if err := os.Mkdir(realDirectory, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", realDirectory, err)
		}
		alias := filepath.Join(root, "alias")
		if err := os.Symlink("real", alias); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", "real", alias, err)
		}
		child := filepath.Join(realDirectory, "child")
		if err := os.WriteFile(child, []byte("fixture\n"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", child, err)
		}

		assertTargetSetOverlap(t, []LabeledTarget{
			{Label: "alias leaf", Path: alias},
			{Label: "alias child", Path: filepath.Join(alias, "child")},
		}, TargetRelationLeftAncestor)
	})

	t.Run("chained intermediate symlink", func(t *testing.T) {
		root := t.TempDir()
		realDirectory := filepath.Join(root, "real")
		if err := os.Mkdir(realDirectory, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", realDirectory, err)
		}
		bridge := filepath.Join(root, "bridge")
		alias := filepath.Join(root, "alias")
		if err := os.Symlink("real", bridge); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", "real", bridge, err)
		}
		if err := os.Symlink("bridge", alias); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", "bridge", alias, err)
		}
		child := filepath.Join(realDirectory, "child")
		if err := os.WriteFile(child, []byte("fixture\n"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", child, err)
		}

		assertTargetSetOverlap(t, []LabeledTarget{
			{Label: "bridge leaf", Path: bridge},
			{Label: "alias child", Path: filepath.Join(alias, "child")},
		}, TargetRelationLeftAncestor)
	})

	t.Run("component traversed before dot dot", func(t *testing.T) {
		root := t.TempDir()
		detour := filepath.Join(root, "detour")
		realDirectory := filepath.Join(root, "real")
		for _, directory := range []string{detour, realDirectory} {
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatalf("os.Mkdir(%q) error = %v", directory, err)
			}
		}
		alias := filepath.Join(root, "alias")
		if err := os.Symlink(filepath.FromSlash("detour/../real"), alias); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", "detour/../real", alias, err)
		}
		child := filepath.Join(realDirectory, "child")
		if err := os.WriteFile(child, []byte("fixture\n"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", child, err)
		}

		assertTargetSetOverlap(t, []LabeledTarget{
			{Label: "detour leaf", Path: detour},
			{Label: "alias child", Path: filepath.Join(alias, "child")},
		}, TargetRelationLeftAncestor)
	})
}

func TestValidateTargetSet_RejectsSelfTraversal(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", realDirectory, err)
	}
	bridge := filepath.Join(root, "bridge")
	if err := os.Symlink("real", bridge); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", "real", bridge, err)
	}
	detour := filepath.Join(root, "detour")
	if err := os.Symlink(filepath.FromSlash("bridge/.."), detour); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", "bridge/..", detour, err)
	}
	input := LabeledTarget{
		Label: "module bad self-traversing target",
		Path:  filepath.Join(detour, "bridge"),
	}
	resolution := mustResolveTarget(t, input.Path)
	if !resolution.Traverses(resolution) {
		t.Fatal("fixture target does not traverse its own leaf identity")
	}

	validated, err := ValidateTargetSet([]LabeledTarget{input})
	if !errors.Is(err, ErrTargetOverlap) {
		t.Fatalf("ValidateTargetSet() error = %v, want ErrTargetOverlap", err)
	}
	if validated.targets != nil {
		t.Fatalf("ValidateTargetSet() targets = %#v, want zero result", validated.targets)
	}
	var traversal interface{ Target() LabeledTarget }
	if !errors.As(err, &traversal) {
		t.Fatalf("ValidateTargetSet() error = %T %v, want structured self-traversal error", err, err)
	}
	if traversal.Target() != input {
		t.Errorf("self-traversal target = %#v, want %#v", traversal.Target(), input)
	}
	for _, want := range []string{input.Label, input.Path, "traverses its own leaf"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateTargetSet() error = %q, want %q", err, want)
		}
	}
}

func TestValidateTargetSet_PreservesMutualSymlinkAncestorRelation(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	if err := os.Symlink(root, left); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", root, left, err)
	}
	if err := os.Symlink(root, right); err != nil {
		t.Fatalf("os.Symlink(%q, %q) error = %v", root, right, err)
	}

	assertTargetSetOverlap(
		t,
		[]LabeledTarget{
			{Label: "left reached through right", Path: filepath.Join(right, "left")},
			{Label: "right reached through left", Path: filepath.Join(left, "right")},
		},
		TargetRelationLeftAncestor|TargetRelationRightAncestor,
	)
}

func TestValidateTargetSet_DoesNotInventRelations(t *testing.T) {
	t.Run("string prefix siblings", func(t *testing.T) {
		root := t.TempDir()
		inputs := []LabeledTarget{
			{Label: "foo", Path: filepath.Join(root, "foo")},
			{Label: "foobar", Path: filepath.Join(root, "foobar")},
		}
		for _, input := range inputs {
			if err := os.WriteFile(input.Path, []byte("fixture\n"), 0o600); err != nil {
				t.Fatalf("os.WriteFile(%q) error = %v", input.Path, err)
			}
		}
		if _, err := ValidateTargetSet(inputs); err != nil {
			t.Fatalf("ValidateTargetSet() error = %v", err)
		}
	})

	t.Run("leaf symlink and direct real child", func(t *testing.T) {
		root := t.TempDir()
		realDirectory := filepath.Join(root, "real")
		if err := os.Mkdir(realDirectory, 0o700); err != nil {
			t.Fatalf("os.Mkdir(%q) error = %v", realDirectory, err)
		}
		alias := filepath.Join(root, "alias")
		if err := os.Symlink("real", alias); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", "real", alias, err)
		}
		realChild := filepath.Join(realDirectory, "child")
		if err := os.WriteFile(realChild, []byte("fixture\n"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", realChild, err)
		}

		if _, err := ValidateTargetSet([]LabeledTarget{
			{Label: "alias leaf", Path: alias},
			{Label: "real child", Path: realChild},
		}); err != nil {
			t.Fatalf("ValidateTargetSet() error = %v", err)
		}
	})

	t.Run("different hard-link entries", func(t *testing.T) {
		root := t.TempDir()
		first := filepath.Join(root, "first")
		second := filepath.Join(root, "second")
		if err := os.WriteFile(first, []byte("content"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", first, err)
		}
		if err := os.Link(first, second); err != nil {
			t.Fatalf("os.Link(%q, %q) error = %v", first, second, err)
		}
		firstInfo, err := os.Lstat(first)
		if err != nil {
			t.Fatalf("os.Lstat(%q) error = %v", first, err)
		}
		secondInfo, err := os.Lstat(second)
		if err != nil {
			t.Fatalf("os.Lstat(%q) error = %v", second, err)
		}
		if !os.SameFile(firstInfo, secondInfo) {
			t.Fatal("hard-link fixture does not share a file object")
		}

		if _, err := ValidateTargetSet([]LabeledTarget{
			{Label: "first link", Path: first},
			{Label: "second link", Path: second},
		}); err != nil {
			t.Fatalf("ValidateTargetSet() error = %v", err)
		}
	})
}

func TestValidateTargetSet_NameAliasesMatchFilesystem(t *testing.T) {
	tests := []struct {
		name   string
		actual string
		alias  string
	}{
		{name: "case", actual: "TargetCase", alias: "targetcase"},
		{name: "Unicode", actual: "caf\u00e9", alias: "cafe\u0301"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			actual := filepath.Join(root, test.actual)
			alias := filepath.Join(root, test.alias)
			if err := os.WriteFile(actual, []byte("content"), 0o600); err != nil {
				t.Fatalf("os.WriteFile(%q) error = %v", actual, err)
			}
			_, lookupErr := os.Lstat(alias)
			_, err := ValidateTargetSet([]LabeledTarget{
				{Label: "actual", Path: actual},
				{Label: "alias", Path: alias},
			})

			switch {
			case lookupErr == nil:
				if !errors.Is(err, ErrTargetOverlap) {
					t.Fatalf("filesystem accepts alias but validator error = %v, want overlap", err)
				}
			case errors.Is(lookupErr, fs.ErrNotExist):
				if err == nil || errors.Is(err, ErrIdentityUnavailable) {
					return
				}
				if errors.Is(err, ErrTargetOverlap) {
					t.Fatalf("filesystem distinguishes names but validator reports overlap: %v", err)
				}
				t.Fatalf("ValidateTargetSet() error = %v, want success or ErrIdentityUnavailable", err)
			default:
				t.Fatalf("observe filesystem alias %q: %v", alias, lookupErr)
			}
		})
	}
}

func TestValidateTargetSet_FailsClosed(t *testing.T) {
	t.Run("file blocks intermediate directory", func(t *testing.T) {
		root := t.TempDir()
		file := filepath.Join(root, "file")
		if err := os.WriteFile(file, []byte("content"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", file, err)
		}
		_, err := ValidateTargetSet([]LabeledTarget{
			{Label: "file target", Path: file},
			{Label: "blocked child target", Path: filepath.Join(file, "child")},
		})
		if !errors.Is(err, ErrPathBlocked) || !strings.Contains(err.Error(), "blocked child target") {
			t.Fatalf("ValidateTargetSet() error = %v, want labeled ErrPathBlocked", err)
		}
	})

	t.Run("dangling ancestor symlink", func(t *testing.T) {
		root := t.TempDir()
		dangling := filepath.Join(root, "dangling")
		if err := os.Symlink("missing", dangling); err != nil {
			t.Fatalf("os.Symlink(%q, %q) error = %v", "missing", dangling, err)
		}
		_, err := ValidateTargetSet([]LabeledTarget{{
			Label: "blocked alias child",
			Path:  filepath.Join(dangling, "child"),
		}})
		if !errors.Is(err, ErrPathBlocked) || !strings.Contains(err.Error(), "blocked alias child") {
			t.Fatalf("ValidateTargetSet() error = %v, want labeled ErrPathBlocked", err)
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		_, err := ValidateTargetSet([]LabeledTarget{{Label: "relative target", Path: "relative"}})
		if err == nil || !strings.Contains(err.Error(), "relative target") {
			t.Fatalf("ValidateTargetSet() error = %v, want labeled path error", err)
		}
	})

	t.Run("empty provenance", func(t *testing.T) {
		_, err := ValidateTargetSet([]LabeledTarget{{Path: filepath.Join(t.TempDir(), "target")}})
		if err == nil || !strings.Contains(err.Error(), "empty provenance") {
			t.Fatalf("ValidateTargetSet() error = %v, want provenance error", err)
		}
	})

	t.Run("ordinary IO cause", func(t *testing.T) {
		overlong := strings.Repeat("x", 4096)
		validated, err := ValidateTargetSet([]LabeledTarget{{
			Label: "overlong target",
			Path:  filepath.Join(t.TempDir(), overlong, "child"),
		}})
		if !errors.Is(err, syscall.ENAMETOOLONG) || !strings.Contains(err.Error(), "overlong target") {
			t.Fatalf("ValidateTargetSet() error = %v, want labeled ENAMETOOLONG cause", err)
		}
		if validated.targets != nil {
			t.Fatalf("ValidateTargetSet() targets = %#v, want zero result", validated.targets)
		}
	})
}

func assertTargetSetOverlap(t *testing.T, inputs []LabeledTarget, wantRelation TargetRelation) {
	t.Helper()

	validated, err := ValidateTargetSet(inputs)
	if !errors.Is(err, ErrTargetOverlap) {
		t.Fatalf("ValidateTargetSet() error = %v, want ErrTargetOverlap", err)
	}
	if validated.targets != nil {
		t.Fatalf("ValidateTargetSet() targets = %#v, want zero result", validated.targets)
	}
	var conflict *TargetConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("ValidateTargetSet() error = %T %v, want *TargetConflictError", err, err)
	}
	if conflict.Left() != inputs[0] || conflict.Right() != inputs[1] {
		t.Errorf(
			"TargetConflictError inputs = (%#v, %#v), want (%#v, %#v)",
			conflict.Left(),
			conflict.Right(),
			inputs[0],
			inputs[1],
		)
	}
	if conflict.Relation() != wantRelation {
		t.Errorf("TargetConflictError.Relation() = %v, want %v", conflict.Relation(), wantRelation)
	}
	for _, input := range inputs {
		for _, want := range []string{input.Label, input.Path} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("ValidateTargetSet() error = %q, want %q", err, want)
			}
		}
	}
	if !strings.Contains(err.Error(), wantRelation.String()) {
		t.Errorf("ValidateTargetSet() error = %q, want relation %q", err, wantRelation)
	}
}
