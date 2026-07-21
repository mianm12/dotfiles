package add

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteScaffoldItem_PublishesSourceWithoutMutatingTargetOrHardLink(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o644)
	sibling := filepath.Join(fixture.home, "sibling")
	if err := os.Link(target, sibling); err != nil {
		t.Fatal(err)
	}
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeScaffold})
	if err != nil {
		t.Fatal(err)
	}
	item := plan.Items()[0]
	targetBefore, _ := os.Lstat(target)
	siblingBefore, _ := os.Lstat(sibling)

	result, err := executeScaffoldItem(fixture.control, item, defaultPublicationOperations())
	if err != nil {
		t.Fatalf("executeScaffoldItem() error = %v", err)
	}
	if !result.Valid() || !result.sourcePublished || !result.stateReady || result.targetCommitted {
		t.Fatalf("executeScaffoldItem() result = %#v", result)
	}
	assertRegularFile(t, item.SourcePath(), "content", 0o644)
	assertRegularFile(t, target, "content", 0o644)
	assertRegularFile(t, sibling, "content", 0o644)
	targetAfter, _ := os.Lstat(target)
	siblingAfter, _ := os.Lstat(sibling)
	sourceInfo, _ := os.Lstat(item.SourcePath())
	if !os.SameFile(targetBefore, targetAfter) || !os.SameFile(siblingBefore, siblingAfter) ||
		!os.SameFile(targetAfter, siblingAfter) || os.SameFile(targetAfter, sourceInfo) {
		t.Fatal("scaffold publication changed target/sibling identity or reused their inode")
	}
}

func TestExecuteScaffoldItem_TargetIdentityChangeAfterPublicationCleansOwnedSource(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o644)
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeScaffold})
	if err != nil {
		t.Fatal(err)
	}
	item := plan.Items()[0]
	operations := defaultPublicationOperations()
	realSync := operations.syncDirectory
	operations.syncDirectory = func(directory string) error {
		if err := realSync(directory); err != nil {
			return err
		}
		replacement := filepath.Join(fixture.home, "replacement")
		writeAddFile(t, replacement, "content", 0o644)
		return os.Rename(replacement, target)
	}

	result, err := executeScaffoldItem(fixture.control, item, operations)
	if err == nil || !result.Valid() || !result.sourcePublished || result.stateReady || result.targetCommitted {
		t.Fatalf("executeScaffoldItem() = (%#v, %v)", result, err)
	}
	if _, statErr := os.Lstat(item.SourcePath()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("source Lstat() error = %v, want missing after safe cleanup", statErr)
	}
	assertRegularFile(t, target, "content", 0o644)
}

func TestExecuteScaffoldItem_EquivalentSourceIsNeverCleanedOnTargetFailure(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o644)
	first, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeScaffold})
	if err != nil {
		t.Fatal(err)
	}
	item := first.Items()[0]
	if _, err := publishSource(item, defaultPublicationOperations()); err != nil {
		t.Fatal(err)
	}
	resumed, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeScaffold})
	if err != nil {
		t.Fatal(err)
	}
	item = resumed.Items()[0]
	if !item.SourceExists() {
		t.Fatal("equivalent source was not recognized")
	}
	operations := defaultPublicationOperations()
	realRead := operations.readFile
	operations.readFile = func(path string) ([]byte, error) {
		if path == target {
			return []byte("changed"), nil
		}
		return realRead(path)
	}
	result, err := executeScaffoldItem(fixture.control, item, operations)
	if err == nil || result.stateReady {
		t.Fatalf("executeScaffoldItem() = (%#v, %v)", result, err)
	}
	assertRegularFile(t, item.SourcePath(), "content", 0o644)
}
