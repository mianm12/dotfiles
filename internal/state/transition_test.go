package state

import (
	"errors"
	"reflect"
	"testing"
)

func TestTransitionEntries_MissingBaselineCreatesValidSnapshot(t *testing.T) {
	loaded := Loaded{status: StatusMissing}
	updates := []EntryUpdate{{
		Key:       "~/.zshrc",
		Module:    "zsh",
		Kind:      KindSymlink,
		Source:    "modules/zsh/zshrc",
		LinkDest:  "/repo/modules/zsh/zshrc",
		AppliedAt: "2026-07-20T00:00:00Z",
	}}

	snapshot, changed, err := TransitionEntries(loaded, updates)
	if err != nil {
		t.Fatalf("TransitionEntries() error = %v", err)
	}
	if !changed || snapshot.Version() != 1 {
		t.Fatalf("TransitionEntries() = (version=%d, changed=%t), want (1, true)", snapshot.Version(), changed)
	}
	entry, ok := snapshot.Entry("~/.zshrc")
	if !ok || entry.Module() != "zsh" || entry.Kind() != KindSymlink ||
		entry.Source() != "modules/zsh/zshrc" ||
		entry.LinkDest() != "/repo/modules/zsh/zshrc" ||
		entry.AppliedAt() != "2026-07-20T00:00:00Z" {
		t.Fatalf("transitioned entry = (%#v, %t)", entry, ok)
	}
	if _, err := Encode(snapshot); err != nil {
		t.Fatalf("Encode(transitioned snapshot) error = %v", err)
	}
}

func TestTransition_MissingBaselineCreatesRunOnceSnapshot(t *testing.T) {
	snapshot, changed, err := Transition(Loaded{status: StatusMissing}, ChangeSet{
		RunOnceUpdates: []RunOnceUpdate{{
			Key:        "zsh/hooks/setup.sh",
			Hash:       "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			ExecutedAt: "2026-07-22T01:02:03Z",
		}},
	})
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if !changed || snapshot.Version() != 1 || len(snapshot.EntryKeys()) != 0 {
		t.Fatalf("Transition() = (%#v, %t), want run_once-only v1 change", snapshot, changed)
	}
	record, exists := snapshot.RunOnce("zsh/hooks/setup.sh")
	if !exists || record.Hash() != "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ||
		record.ExecutedAt() != "2026-07-22T01:02:03Z" {
		t.Fatalf("run_once record = (%#v, %t)", record, exists)
	}
	encoded, err := Encode(snapshot)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	roundTripped, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode(Encode()) error = %v", err)
	}
	if !snapshotsEqual(snapshot, roundTripped) {
		t.Fatal("run_once transition changed state v1 round trip")
	}
}

func TestTransition_AtomicallyCombinesEntryAndRunOnceEffects(t *testing.T) {
	loaded := loadedTransitionFixture(t)
	before, ok := loaded.Snapshot()
	if !ok {
		t.Fatal("fixture loaded state has no snapshot")
	}
	changes := ChangeSet{
		EntryUpdates: []EntryUpdate{{
			Key:       "~/.added",
			Module:    "new",
			Kind:      KindScaffold,
			Source:    "modules/new/added.template",
			AppliedAt: "2026-07-22T01:02:03Z",
		}},
		EntryDeletes: []string{"~/.old"},
		RunOnceUpdates: []RunOnceUpdate{
			{
				Key:        "keep/hooks/setup",
				Hash:       "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				ExecutedAt: "2026-07-22T01:02:04Z",
			},
			{
				Key:        "new/hooks/bootstrap.sh",
				Hash:       "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				ExecutedAt: "2026-07-22T01:02:05Z",
			},
		},
	}

	snapshot, changed, err := Transition(loaded, changes)
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if !changed {
		t.Fatal("Transition() changed = false, want true")
	}
	if _, exists := snapshot.Entry("~/.old"); exists {
		t.Fatal("entry delete was not applied")
	}
	if entry, exists := snapshot.Entry("~/.added"); !exists || entry.Module() != "new" {
		t.Fatalf("entry upsert = (%#v, %t)", entry, exists)
	}
	if record, exists := snapshot.RunOnce("keep/hooks/setup"); !exists ||
		record.Hash() != changes.RunOnceUpdates[0].Hash {
		t.Fatalf("updated run_once = (%#v, %t)", record, exists)
	}
	if record, exists := snapshot.RunOnce("new/hooks/bootstrap.sh"); !exists ||
		record.Hash() != changes.RunOnceUpdates[1].Hash {
		t.Fatalf("new run_once = (%#v, %t)", record, exists)
	}
	if !reflect.DeepEqual(before.EntryKeys(), []string{"~/.old", "~/.unrelated", "~/.zshrc"}) {
		t.Fatalf("input entries mutated: %v", before.EntryKeys())
	}
	if record, _ := before.RunOnce("keep/hooks/setup"); record.Hash() != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("input run_once mutated: %#v", record)
	}
}

func TestTransition_RejectsInvalidOrDuplicateRunOnceUpdatesAtomically(t *testing.T) {
	valid := RunOnceUpdate{
		Key:        "zsh/hooks/setup.sh",
		Hash:       "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ExecutedAt: "2026-07-22T01:02:03Z",
	}
	tests := []struct {
		name    string
		updates []RunOnceUpdate
	}{
		{name: "invalid key", updates: []RunOnceUpdate{{Key: "../setup.sh", Hash: valid.Hash, ExecutedAt: valid.ExecutedAt}}},
		{name: "invalid hash", updates: []RunOnceUpdate{{Key: valid.Key, Hash: "sha256:short", ExecutedAt: valid.ExecutedAt}}},
		{name: "invalid time", updates: []RunOnceUpdate{{Key: valid.Key, Hash: valid.Hash, ExecutedAt: "not-a-time"}}},
		{name: "duplicate key", updates: []RunOnceUpdate{valid, valid}},
	}
	entry := EntryUpdate{
		Key:       "~/.would-change",
		Module:    "zsh",
		Kind:      KindScaffold,
		Source:    "modules/zsh/would-change.template",
		AppliedAt: "2026-07-22T01:02:03Z",
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot, changed, err := Transition(loadedTransitionFixture(t), ChangeSet{
				EntryUpdates:   []EntryUpdate{entry},
				RunOnceUpdates: test.updates,
			})
			if !errors.Is(err, ErrTransition) {
				t.Fatalf("Transition() error = %v, want ErrTransition", err)
			}
			if changed || snapshot.Version() != 0 {
				t.Fatalf("Transition() = (%#v, %t), want zero/false", snapshot, changed)
			}
		})
	}
}

func TestTransition_EmptyOrEquivalentChangesDoNotRequestStore(t *testing.T) {
	for _, loaded := range []Loaded{{status: StatusMissing}, loadedTransitionFixture(t)} {
		snapshot, changed, err := Transition(loaded, ChangeSet{})
		if err != nil || changed || snapshot.Version() != 1 {
			t.Fatalf("Transition(empty) = (%#v, %t, %v)", snapshot, changed, err)
		}
	}

	loaded := loadedTransitionFixture(t)
	snapshot, changed, err := Transition(loaded, ChangeSet{RunOnceUpdates: []RunOnceUpdate{{
		Key:        "keep/hooks/setup",
		Hash:       "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ExecutedAt: "2026-07-19T00:00:00Z",
	}}})
	if err != nil || changed {
		t.Fatalf("Transition(equivalent run_once) = (%#v, %t, %v), want unchanged", snapshot, changed, err)
	}
}

func TestTransitionEntries_LoadedPreservesUnrelatedStateAndRunOnce(t *testing.T) {
	loaded := loadedTransitionFixture(t)
	before, ok := loaded.Snapshot()
	if !ok {
		t.Fatal("fixture loaded state has no snapshot")
	}
	updates := []EntryUpdate{
		{
			Key:         "~/.new",
			PreviousKey: "~/.old",
			Module:      "app",
			Kind:        KindScaffold,
			Source:      "modules/app/new.template",
			AppliedAt:   "2026-07-20T00:00:01Z",
		},
		{
			Key:       "~/.zshrc",
			Module:    "zsh",
			Kind:      KindSymlink,
			Source:    "modules/zsh/new-zshrc",
			LinkDest:  "/repo/modules/zsh/new-zshrc",
			AppliedAt: "2026-07-20T00:00:02Z",
		},
	}

	snapshot, changed, err := TransitionEntries(loaded, updates)
	if err != nil {
		t.Fatalf("TransitionEntries() error = %v", err)
	}
	if !changed {
		t.Fatal("TransitionEntries() changed = false, want true")
	}
	if _, exists := snapshot.Entry("~/.old"); exists {
		t.Fatal("PreviousKey remains after atomic key migration")
	}
	if entry, exists := snapshot.Entry("~/.new"); !exists || entry.Kind() != KindScaffold {
		t.Fatalf("new scaffold entry = (%#v, %t)", entry, exists)
	}
	if entry, exists := snapshot.Entry("~/.unrelated"); !exists || entry.Module() != "keep" {
		t.Fatalf("unrelated entry = (%#v, %t), want preserved", entry, exists)
	}
	if record, exists := snapshot.RunOnce("keep/hooks/setup"); !exists ||
		record.Hash() != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		record.ExecutedAt() != "2026-07-19T00:00:00Z" {
		t.Fatalf("run_once record = (%#v, %t), want preserved", record, exists)
	}
	if !reflect.DeepEqual(before.EntryKeys(), []string{"~/.old", "~/.unrelated", "~/.zshrc"}) {
		t.Fatalf("input Snapshot mutated, keys = %v", before.EntryKeys())
	}
	if entry, _ := before.Entry("~/.zshrc"); entry.Source() != "modules/zsh/zshrc" {
		t.Fatalf("input Snapshot entry mutated = %#v", entry)
	}
}

func TestTransitionEntries_MixesUpsertsAndDeletesAndPreservesRunOnce(t *testing.T) {
	loaded := loadedTransitionFixture(t)
	updates := []EntryUpdate{{
		Key:       "~/.added",
		Module:    "new",
		Kind:      KindScaffold,
		Source:    "modules/new/added.template",
		AppliedAt: "2026-07-20T00:00:03Z",
	}}

	snapshot, changed, err := TransitionEntries(loaded, updates, "~/.old", "~/.zshrc")
	if err != nil {
		t.Fatalf("TransitionEntries() error = %v", err)
	}
	if !changed {
		t.Fatal("TransitionEntries() changed = false, want true")
	}
	if got := snapshot.EntryKeys(); !reflect.DeepEqual(got, []string{"~/.added", "~/.unrelated"}) {
		t.Fatalf("transitioned keys = %v", got)
	}
	if record, exists := snapshot.RunOnce("keep/hooks/setup"); !exists ||
		record.Hash() != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("run_once record = (%#v, %t), want preserved", record, exists)
	}
	if _, err := Encode(snapshot); err != nil {
		t.Fatalf("Encode(transitioned snapshot) error = %v", err)
	}
}

func TestTransitionEntries_RejectsAmbiguousDeletes(t *testing.T) {
	valid := EntryUpdate{
		Key:       "~/.zshrc",
		Module:    "zsh",
		Kind:      KindSymlink,
		Source:    "modules/zsh/zshrc",
		LinkDest:  "/repo/modules/zsh/zshrc",
		AppliedAt: "2026-07-20T00:00:00Z",
	}
	tests := []struct {
		name    string
		updates []EntryUpdate
		deletes []string
	}{
		{name: "duplicate delete", deletes: []string{"~/.old", "~/.old"}},
		{name: "missing delete", deletes: []string{"~/.missing"}},
		{name: "upsert and delete same key", updates: []EntryUpdate{valid}, deletes: []string{"~/.zshrc"}},
		{
			name: "migration previous key and delete same key",
			updates: []EntryUpdate{{
				Key:         "~/.new",
				PreviousKey: "~/.old",
				Module:      "app",
				Kind:        KindScaffold,
				Source:      "modules/app/new.template",
				AppliedAt:   "2026-07-20T00:00:00Z",
			}},
			deletes: []string{"~/.old"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot, changed, err := TransitionEntries(loadedTransitionFixture(t), test.updates, test.deletes...)
			if !errors.Is(err, ErrTransition) {
				t.Fatalf("TransitionEntries() error = %v, want ErrTransition", err)
			}
			if changed || snapshot.Version() != 0 {
				t.Fatalf("TransitionEntries() = (%#v, %t), want zero/false", snapshot, changed)
			}
		})
	}
}

func TestTransitionEntries_EmptyUpdatesDoNotRequestStore(t *testing.T) {
	for _, loaded := range []Loaded{{status: StatusMissing}, loadedTransitionFixture(t)} {
		snapshot, changed, err := TransitionEntries(loaded, nil)
		if err != nil {
			t.Fatalf("TransitionEntries() error = %v", err)
		}
		if changed {
			t.Fatal("TransitionEntries() changed = true, want false")
		}
		if loaded.Missing() {
			if snapshot.Version() != 1 || len(snapshot.EntryKeys()) != 0 || len(snapshot.RunOnceKeys()) != 0 {
				t.Fatalf("missing empty baseline snapshot = %#v", snapshot)
			}
		} else if got, _ := loaded.Snapshot(); !snapshotsEqual(snapshot, got) {
			t.Fatal("loaded empty transition did not return an equal snapshot")
		}
	}
}

func TestTransitionEntries_RejectsAmbiguousOrInvalidUpdates(t *testing.T) {
	valid := EntryUpdate{
		Key:       "~/.zshrc",
		Module:    "zsh",
		Kind:      KindSymlink,
		Source:    "modules/zsh/zshrc",
		LinkDest:  "/repo/modules/zsh/zshrc",
		AppliedAt: "2026-07-20T00:00:00Z",
	}
	tests := []struct {
		name    string
		loaded  Loaded
		updates []EntryUpdate
	}{
		{name: "invalid baseline", loaded: Loaded{}, updates: []EntryUpdate{valid}},
		{name: "duplicate key", loaded: Loaded{status: StatusMissing}, updates: []EntryUpdate{valid, valid}},
		{
			name:   "previous key reused",
			loaded: loadedTransitionFixture(t),
			updates: []EntryUpdate{
				{Key: "~/.first", PreviousKey: "~/.old", Module: "app", Kind: KindScaffold, Source: "modules/app/first", AppliedAt: "2026-07-20T00:00:00Z"},
				{Key: "~/.second", PreviousKey: "~/.old", Module: "app", Kind: KindScaffold, Source: "modules/app/second", AppliedAt: "2026-07-20T00:00:00Z"},
			},
		},
		{name: "missing previous key", loaded: Loaded{status: StatusMissing}, updates: []EntryUpdate{{
			Key: "~/.new", PreviousKey: "~/.missing", Module: "app", Kind: KindScaffold, Source: "modules/app/new", AppliedAt: "2026-07-20T00:00:00Z",
		}}},
		{name: "invalid entry", loaded: Loaded{status: StatusMissing}, updates: []EntryUpdate{{
			Key: "~/.bad", Module: "app", Kind: KindScaffold, Source: "outside/app", AppliedAt: "not-a-time",
		}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot, changed, err := TransitionEntries(test.loaded, test.updates)
			if !errors.Is(err, ErrTransition) {
				t.Fatalf("TransitionEntries() error = %v, want ErrTransition", err)
			}
			if changed || snapshot.Version() != 0 {
				t.Fatalf("TransitionEntries() = (%#v, %t), want zero/false", snapshot, changed)
			}
		})
	}
}

func loadedTransitionFixture(t *testing.T) Loaded {
	t.Helper()
	snapshot, err := Decode([]byte(`{
  "version": 1,
  "entries": {
    "~/.old": {"module":"app","kind":"scaffold","source":"modules/app/old.template","applied_at":"2026-07-18T00:00:00Z"},
    "~/.unrelated": {"module":"keep","kind":"scaffold","source":"modules/keep/file.template","applied_at":"2026-07-18T00:00:00Z"},
    "~/.zshrc": {"module":"zsh","kind":"symlink","source":"modules/zsh/zshrc","link_dest":"/repo/modules/zsh/zshrc","applied_at":"2026-07-18T00:00:00Z"}
  },
  "run_once": {
    "keep/hooks/setup": {"hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","executed_at":"2026-07-19T00:00:00Z"}
  }
}`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	return Loaded{status: StatusLoaded, snapshot: snapshot}
}
