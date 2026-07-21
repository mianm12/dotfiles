package add

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishSource_CopiesIndependentBytesAndMode(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, ".config/app/config", "content\n", 0o640)
	sibling := filepath.Join(fixture.home, "sibling")
	if err := os.Link(target, sibling); err != nil {
		t.Fatal(err)
	}
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
	if err != nil {
		t.Fatal(err)
	}
	item := plan.Items()[0]

	publication, err := publishSource(item, defaultPublicationOperations())
	if err != nil {
		t.Fatalf("publishSource() error = %v", err)
	}
	if !publication.Valid() || !publication.Created() {
		t.Fatalf("publication = %#v, want valid created result", publication)
	}
	assertRegularFile(t, item.SourcePath(), "content\n", 0o640)
	targetInfo, _ := os.Lstat(target)
	sourceInfo, _ := os.Lstat(item.SourcePath())
	siblingInfo, _ := os.Lstat(sibling)
	if os.SameFile(targetInfo, sourceInfo) || os.SameFile(siblingInfo, sourceInfo) {
		t.Fatal("published source shares the target hard-link inode")
	}
	assertRegularFile(t, target, "content\n", 0o640)
	assertRegularFile(t, sibling, "content\n", 0o640)
}

func TestPublishSource_ReusesEquivalentSourceWithoutRewrite(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o600)
	source := filepath.Join(fixture.repo, "modules", "app", "config")
	writeAddFile(t, source, "content", 0o600)
	before, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
	if err != nil {
		t.Fatal(err)
	}

	publication, err := publishSource(plan.Items()[0], defaultPublicationOperations())
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.Lstat(source)
	if !publication.Valid() || publication.Created() || !os.SameFile(before, after) {
		t.Fatalf("equivalent source was rewritten: publication=%#v", publication)
	}
}

func TestPublishSource_FailuresPreserveTargetAndDoNotLeavePublishedSource(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*publicationOperations, error)
	}{
		{name: "create", mutate: func(ops *publicationOperations, injected error) {
			ops.createTemp = func(string, string) (publicationFile, error) { return nil, injected }
		}},
		{name: "write", mutate: func(ops *publicationOperations, injected error) {
			real := ops.createTemp
			ops.createTemp = func(dir, pattern string) (publicationFile, error) {
				file, err := real(dir, pattern)
				return &publicationFileFailure{publicationFile: file, writeErr: injected}, err
			}
		}},
		{name: "short write", mutate: func(ops *publicationOperations, _ error) {
			real := ops.createTemp
			ops.createTemp = func(dir, pattern string) (publicationFile, error) {
				file, err := real(dir, pattern)
				return &publicationFileFailure{publicationFile: file, shortWrite: true}, err
			}
		}},
		{name: "chmod", mutate: func(ops *publicationOperations, injected error) {
			real := ops.createTemp
			ops.createTemp = func(dir, pattern string) (publicationFile, error) {
				file, err := real(dir, pattern)
				return &publicationFileFailure{publicationFile: file, chmodErr: injected}, err
			}
		}},
		{name: "sync", mutate: func(ops *publicationOperations, injected error) {
			real := ops.createTemp
			ops.createTemp = func(dir, pattern string) (publicationFile, error) {
				file, err := real(dir, pattern)
				return &publicationFileFailure{publicationFile: file, syncErr: injected}, err
			}
		}},
		{name: "close", mutate: func(ops *publicationOperations, injected error) {
			real := ops.createTemp
			ops.createTemp = func(dir, pattern string) (publicationFile, error) {
				file, err := real(dir, pattern)
				return &publicationFileFailure{publicationFile: file, closeErr: injected}, err
			}
		}},
		{name: "publish", mutate: func(ops *publicationOperations, injected error) {
			ops.publish = func(string, string) error { return injected }
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
			target := fixture.writeTarget(t, "nested/config", "content", 0o640)
			plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
			if err != nil {
				t.Fatal(err)
			}
			item := plan.Items()[0]
			ops := defaultPublicationOperations()
			injected := errors.New("injected " + test.name)
			test.mutate(&ops, injected)

			publication, err := publishSource(item, ops)
			if err == nil {
				t.Fatal("publishSource() error = nil")
			}
			if publication.Created() {
				if cleanupErr := cleanupSourcePublication(publication, defaultPublicationOperations()); cleanupErr != nil {
					t.Fatalf("cleanupSourcePublication() error = %v", cleanupErr)
				}
			}
			assertRegularFile(t, target, "content", 0o640)
			if _, statErr := os.Lstat(item.SourcePath()); !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("source Lstat() error = %v, want missing", statErr)
			}
			assertNoAddTemporaryEntries(t, filepath.Dir(item.SourcePath()))
		})
	}
}

func TestPublishSource_DirectorySyncFailureReturnsOwnedPublicationForCleanup(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o600)
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
	if err != nil {
		t.Fatal(err)
	}
	item := plan.Items()[0]
	ops := defaultPublicationOperations()
	injected := errors.New("directory sync failed")
	ops.syncDirectory = func(string) error { return injected }

	publication, err := publishSource(item, ops)
	if !errors.Is(err, injected) || !publication.Valid() || !publication.Created() {
		t.Fatalf("publishSource() = (%#v, %v), want owned publication and sync error", publication, err)
	}
	if cleanupErr := cleanupSourcePublication(publication, defaultPublicationOperations()); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
	if _, statErr := os.Lstat(item.SourcePath()); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("source Lstat() error = %v, want missing", statErr)
	}
}

func TestPublishSource_PostPublishTemporaryCleanupCanBeRetriedSafely(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o600)
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
	if err != nil {
		t.Fatal(err)
	}
	item := plan.Items()[0]
	ops := defaultPublicationOperations()
	injected := errors.New("temporary cleanup failed")
	ops.remove = func(string) error { return injected }

	publication, err := publishSource(item, ops)
	if !errors.Is(err, injected) || !publication.Created() || publication.temporary == "" {
		t.Fatalf("publishSource() = (%#v, %v), want owned source/temp cleanup failure", publication, err)
	}
	if cleanupErr := cleanupSourcePublication(publication, defaultPublicationOperations()); cleanupErr != nil {
		t.Fatalf("cleanupSourcePublication() retry error = %v", cleanupErr)
	}
	if _, statErr := os.Lstat(item.SourcePath()); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("source Lstat() error = %v, want missing", statErr)
	}
	assertNoAddTemporaryEntries(t, filepath.Dir(item.SourcePath()))
}

func TestPublishSource_NoClobberRacePreservesUnexpectedDestination(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o600)
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
	if err != nil {
		t.Fatal(err)
	}
	item := plan.Items()[0]
	ops := defaultPublicationOperations()
	ops.publish = func(_, destination string) error {
		if err := os.WriteFile(destination, []byte("racer"), 0o644); err != nil {
			return err
		}
		return fs.ErrExist
	}

	publication, err := publishSource(item, ops)
	if err == nil || publication.Valid() {
		t.Fatalf("publishSource() = (%#v, %v), want no-clobber failure", publication, err)
	}
	assertRegularFile(t, item.SourcePath(), "racer", 0o644)
	assertRegularFile(t, target, "content", 0o600)
}

func TestPublishSource_CleanupRefusesReplacedTemporaryPath(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o600)
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
	if err != nil {
		t.Fatal(err)
	}
	item := plan.Items()[0]
	ops := defaultPublicationOperations()
	realCreate := ops.createTemp
	injected := errors.New("write failed after replacement")
	var replaced string
	ops.createTemp = func(directory, pattern string) (publicationFile, error) {
		file, err := realCreate(directory, pattern)
		if err != nil {
			return nil, err
		}
		return &replaceTemporaryOnWrite{publicationFile: file, injected: injected, replaced: &replaced}, nil
	}

	publication, err := publishSource(item, ops)
	if !errors.Is(err, injected) || publication.Valid() || replaced == "" {
		t.Fatalf("publishSource() = (%#v, %v), replaced=%q", publication, err, replaced)
	}
	assertRegularFile(t, replaced, "foreign", 0o644)
	assertRegularFile(t, target, "content", 0o600)
	if _, statErr := os.Lstat(item.SourcePath()); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("source Lstat() error = %v, want missing", statErr)
	}
}

func TestCleanupOwnedRegularTemporary_RefusesInPlaceContentOrModeChanges(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(string) error
	}{
		{name: "bytes", mutate: func(path string) error {
			return os.WriteFile(path, []byte("changed"), 0o600)
		}},
		{name: "mode", mutate: func(path string) error {
			return os.Chmod(path, 0o640)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "temporary.swp")
			if err := os.WriteFile(path, []byte("owned"), 0o600); err != nil {
				t.Fatal(err)
			}
			operations := defaultPublicationOperations()
			ownedInfo, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := captureRegularFileEvidence(path, ownedInfo, operations)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.mutate(path); err != nil {
				t.Fatal(err)
			}

			err = cleanupOwnedRegularTemporary(path, evidence, operations)
			if err == nil {
				t.Fatal("cleanupOwnedRegularTemporary() error = nil")
			}
			if _, statErr := os.Lstat(path); statErr != nil {
				t.Fatalf("changed temporary was removed: %v", statErr)
			}
		})
	}
}

type publicationFileFailure struct {
	publicationFile
	writeErr   error
	shortWrite bool
	chmodErr   error
	syncErr    error
	closeErr   error
}

type replaceTemporaryOnWrite struct {
	publicationFile
	injected error
	replaced *string
}

func (file *replaceTemporaryOnWrite) Write([]byte) (int, error) {
	name := file.Name()
	if err := file.Close(); err != nil {
		return 0, err
	}
	if err := os.Remove(name); err != nil {
		return 0, err
	}
	if err := os.WriteFile(name, []byte("foreign"), 0o644); err != nil {
		return 0, err
	}
	*file.replaced = name
	return 0, file.injected
}

func (file *publicationFileFailure) Write(content []byte) (int, error) {
	if file.writeErr != nil {
		return 0, file.writeErr
	}
	if file.shortWrite {
		return len(content) - 1, nil
	}
	return file.publicationFile.Write(content)
}

func (file *publicationFileFailure) Chmod(mode fs.FileMode) error {
	if file.chmodErr != nil {
		return file.chmodErr
	}
	return file.publicationFile.Chmod(mode)
}

func (file *publicationFileFailure) Sync() error {
	if file.syncErr != nil {
		return file.syncErr
	}
	return file.publicationFile.Sync()
}

func (file *publicationFileFailure) Close() error {
	err := file.publicationFile.Close()
	if file.closeErr != nil {
		return errors.Join(err, file.closeErr)
	}
	return err
}

func assertRegularFile(t *testing.T, path, content string, mode fs.FileMode) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != content {
		t.Fatalf("ReadFile(%q) = (%q, %v), want %q", path, got, err, content)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != mode {
		t.Fatalf("Lstat(%q) = (%v, %v), want regular mode %04o", path, info, err, mode)
	}
}

func assertNoAddTemporaryEntries(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".swp" {
			t.Fatalf("temporary entry remains: %q", filepath.Join(parent, entry.Name()))
		}
	}
}

var _ io.Writer = (*publicationFileFailure)(nil)
