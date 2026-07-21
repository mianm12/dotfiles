package add

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteLinkItem_CommitsIndependentSourceAndSymlink(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o640)
	sibling := filepath.Join(fixture.home, "sibling")
	if err := os.Link(target, sibling); err != nil {
		t.Fatal(err)
	}
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
	if err != nil {
		t.Fatal(err)
	}
	item := plan.Items()[0]
	siblingBefore, _ := os.Lstat(sibling)

	result, err := executeLinkItem(fixture.control, item, defaultLinkOperations())
	if err != nil {
		t.Fatalf("executeLinkItem() error = %v", err)
	}
	if !result.Valid() || !result.sourcePublished || !result.targetCommitted {
		t.Fatalf("result = %#v, want committed", result)
	}
	linkText, err := os.Readlink(target)
	if err != nil || linkText != item.SourcePath() {
		t.Fatalf("target Readlink() = (%q, %v), want %q", linkText, err, item.SourcePath())
	}
	assertRegularFile(t, item.SourcePath(), "content", 0o640)
	assertRegularFile(t, sibling, "content", 0o640)
	siblingAfter, _ := os.Lstat(sibling)
	sourceInfo, _ := os.Lstat(item.SourcePath())
	if !os.SameFile(siblingBefore, siblingAfter) || os.SameFile(siblingAfter, sourceInfo) {
		t.Fatal("hard-link sibling identity changed or became the source inode")
	}
}

func TestExecuteLinkItem_FinalPreconditionsPreserveTargetAndCleanSource(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, fixture *addFixture, item ItemPlan, operations *linkOperations)
	}{
		{name: "target bytes", mutate: func(t *testing.T, _ *addFixture, item ItemPlan, operations *linkOperations) {
			real := operations.symlink
			operations.symlink = func(source, target string) error {
				if err := real(source, target); err != nil {
					return err
				}
				return os.WriteFile(item.TargetPath(), []byte("changed"), 0o640)
			}
		}},
		{name: "target identity", mutate: func(t *testing.T, _ *addFixture, item ItemPlan, operations *linkOperations) {
			real := operations.symlink
			operations.symlink = func(source, target string) error {
				if err := real(source, target); err != nil {
					return err
				}
				if err := os.Remove(item.TargetPath()); err != nil {
					return err
				}
				return os.WriteFile(item.TargetPath(), item.Snapshot().Content(), item.Snapshot().Mode())
			}
		}},
		{name: "source bytes", mutate: func(t *testing.T, _ *addFixture, item ItemPlan, operations *linkOperations) {
			real := operations.symlink
			operations.symlink = func(source, target string) error {
				if err := real(source, target); err != nil {
					return err
				}
				return os.WriteFile(item.SourcePath(), []byte("changed"), item.Snapshot().Mode())
			}
		}},
		{name: "source mode", mutate: func(t *testing.T, _ *addFixture, item ItemPlan, operations *linkOperations) {
			real := operations.symlink
			operations.symlink = func(source, target string) error {
				if err := real(source, target); err != nil {
					return err
				}
				return os.Chmod(item.SourcePath(), 0o644)
			}
		}},
		{name: "source identity", mutate: func(t *testing.T, _ *addFixture, item ItemPlan, operations *linkOperations) {
			real := operations.symlink
			operations.symlink = func(source, target string) error {
				if err := real(source, target); err != nil {
					return err
				}
				if err := os.Remove(item.SourcePath()); err != nil {
					return err
				}
				return os.WriteFile(item.SourcePath(), item.Snapshot().Content(), item.Snapshot().Mode())
			}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
			target := fixture.writeTarget(t, "config", "content", 0o640)
			plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
			if err != nil {
				t.Fatal(err)
			}
			item := plan.Items()[0]
			operations := defaultLinkOperations()
			test.mutate(t, fixture, item, &operations)

			result, err := executeLinkItem(fixture.control, item, operations)
			if err == nil || result.targetCommitted {
				t.Fatalf("executeLinkItem() = (%#v, %v), want precommit failure", result, err)
			}
			sourceChanged := test.name == "source bytes" || test.name == "source mode" || test.name == "source identity"
			if _, statErr := os.Lstat(item.SourcePath()); !errors.Is(statErr, fs.ErrNotExist) && !sourceChanged {
				t.Fatalf("source Lstat() error = %v, want missing", statErr)
			}
			switch test.name {
			case "source bytes":
				assertRegularFile(t, item.SourcePath(), "changed", 0o640)
			case "source mode":
				assertRegularFile(t, item.SourcePath(), "content", 0o644)
			case "source identity":
				assertRegularFile(t, item.SourcePath(), "content", 0o640)
			case "target bytes":
				assertRegularFile(t, target, "changed", 0o640)
			default:
				assertRegularFile(t, target, "content", 0o640)
			}
		})
	}
}

func TestExecuteLinkItem_TargetPublishAndCleanupFailuresRespectCommitPoint(t *testing.T) {
	t.Run("temporary directory failure", func(t *testing.T) {
		fixture, item := newLinkItemFixture(t)
		operations := defaultLinkOperations()
		injected := errors.New("mkdir temp failed")
		operations.mkdirTemp = func(string, string) (string, error) { return "", injected }

		result, err := executeLinkItem(fixture.control, item, operations)
		if !errors.Is(err, injected) || result.targetCommitted {
			t.Fatalf("executeLinkItem() = (%#v, %v)", result, err)
		}
		assertRegularFile(t, item.TargetPath(), "content", 0o600)
		if _, statErr := os.Lstat(item.SourcePath()); !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatalf("source Lstat() error = %v, want missing", statErr)
		}
	})

	t.Run("temporary symlink failure", func(t *testing.T) {
		fixture, item := newLinkItemFixture(t)
		operations := defaultLinkOperations()
		injected := errors.New("symlink failed")
		operations.symlink = func(string, string) error { return injected }

		result, err := executeLinkItem(fixture.control, item, operations)
		if !errors.Is(err, injected) || result.targetCommitted {
			t.Fatalf("executeLinkItem() = (%#v, %v)", result, err)
		}
		assertRegularFile(t, item.TargetPath(), "content", 0o600)
		if _, statErr := os.Lstat(item.SourcePath()); !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatalf("source Lstat() error = %v, want missing", statErr)
		}
	})

	t.Run("publish failure", func(t *testing.T) {
		fixture, item := newLinkItemFixture(t)
		operations := defaultLinkOperations()
		injected := errors.New("rename failed")
		operations.rename = func(string, string) error { return injected }

		result, err := executeLinkItem(fixture.control, item, operations)
		if !errors.Is(err, injected) || result.targetCommitted {
			t.Fatalf("executeLinkItem() = (%#v, %v)", result, err)
		}
		assertRegularFile(t, item.TargetPath(), "content", 0o600)
		if _, statErr := os.Lstat(item.SourcePath()); !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatalf("source Lstat() error = %v, want missing", statErr)
		}
	})

	t.Run("postcommit cleanup failure", func(t *testing.T) {
		fixture, item := newLinkItemFixture(t)
		operations := defaultLinkOperations()
		realRemove := operations.remove
		injected := errors.New("directory cleanup failed")
		operations.remove = func(path string) error {
			if filepath.Base(path) != targetTemporaryLinkName {
				return injected
			}
			return realRemove(path)
		}

		result, err := executeLinkItem(fixture.control, item, operations)
		if !errors.Is(err, injected) || !result.Valid() || !result.targetCommitted {
			t.Fatalf("executeLinkItem() = (%#v, %v), want committed cleanup error", result, err)
		}
		if link, readErr := os.Readlink(item.TargetPath()); readErr != nil || link != item.SourcePath() {
			t.Fatalf("committed target = (%q, %v)", link, readErr)
		}
		assertRegularFile(t, item.SourcePath(), "content", 0o600)
	})
}

func TestExecuteLinkItem_RejectsChangedAncestorTopologyAtFinalPrecondition(t *testing.T) {
	fixture := newAddFixture(t, map[string]string{"app": `target = "~/alias"`})
	realParent := filepath.Join(fixture.home, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(fixture.home, "alias")
	if err := os.Symlink(realParent, alias); err != nil {
		t.Fatal(err)
	}
	target := fixture.writeTarget(t, "real/config", "content", 0o600)
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{filepath.Join(alias, "config")}, Module: "app", Mode: ModeLink})
	if err != nil {
		t.Fatal(err)
	}
	item := plan.Items()[0]
	operations := defaultLinkOperations()
	realSymlink := operations.symlink
	operations.symlink = func(source, destination string) error {
		if err := realSymlink(source, destination); err != nil {
			return err
		}
		if err := os.Remove(alias); err != nil {
			return err
		}
		hop := filepath.Join(fixture.home, "hop")
		if err := os.Symlink(realParent, hop); err != nil {
			return err
		}
		return os.Symlink(hop, alias)
	}

	result, err := executeLinkItem(fixture.control, item, operations)
	if err == nil || result.targetCommitted {
		t.Fatalf("executeLinkItem() = (%#v, %v), want topology precondition failure", result, err)
	}
	assertRegularFile(t, target, "content", 0o600)
	if _, statErr := os.Lstat(item.SourcePath()); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("source Lstat() error = %v, want missing", statErr)
	}
}

func newLinkItemFixture(t *testing.T) (*addFixture, ItemPlan) {
	t.Helper()
	fixture := newAddFixture(t, map[string]string{"app": `target = "~"`})
	target := fixture.writeTarget(t, "config", "content", 0o600)
	plan, err := Preflight(fixture.load(t), Request{Paths: []string{target}, Module: "app", Mode: ModeLink})
	if err != nil {
		t.Fatal(err)
	}
	return fixture, plan.Items()[0]
}
