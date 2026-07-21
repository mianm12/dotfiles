package add

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishSource_ScaffoldUsesSharedFailureAndCleanupProtocol(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*publicationOperations, error)
	}{
		{name: "create", mutate: func(operations *publicationOperations, injected error) {
			operations.createTemp = func(string, string) (publicationFile, error) { return nil, injected }
		}},
		{name: "write", mutate: func(operations *publicationOperations, injected error) {
			real := operations.createTemp
			operations.createTemp = func(directory, pattern string) (publicationFile, error) {
				file, err := real(directory, pattern)
				return &publicationFileFailure{publicationFile: file, writeErr: injected}, err
			}
		}},
		{name: "file sync", mutate: func(operations *publicationOperations, injected error) {
			real := operations.createTemp
			operations.createTemp = func(directory, pattern string) (publicationFile, error) {
				file, err := real(directory, pattern)
				return &publicationFileFailure{publicationFile: file, syncErr: injected}, err
			}
		}},
		{name: "publish", mutate: func(operations *publicationOperations, injected error) {
			operations.publish = func(string, string) error { return injected }
		}},
		{name: "directory sync", mutate: func(operations *publicationOperations, injected error) {
			operations.syncDirectory = func(string) error { return injected }
		}},
		{name: "published temp cleanup", mutate: func(operations *publicationOperations, injected error) {
			operations.remove = func(string) error { return injected }
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
			target := fixture.writeTarget(t, "config", "content", 0o644)
			plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeScaffold})
			if err != nil {
				t.Fatal(err)
			}
			item := plan.Items()[0]
			operations := defaultPublicationOperations()
			injected := errors.New("injected " + test.name)
			test.mutate(&operations, injected)

			publication, err := publishSource(item, operations)
			if err == nil {
				t.Fatal("publishSource() error = nil")
			}
			if publication.Created() {
				if cleanupErr := cleanupSourcePublication(publication, defaultPublicationOperations()); cleanupErr != nil {
					t.Fatalf("cleanupSourcePublication() error = %v", cleanupErr)
				}
			}
			if _, statErr := os.Lstat(item.SourcePath()); !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("source Lstat() error = %v, want missing", statErr)
			}
			assertRegularFile(t, target, "content", 0o644)
			assertNoAddTemporaryEntries(t, filepath.Dir(item.SourcePath()))
		})
	}
}

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
