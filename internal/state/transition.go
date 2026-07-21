package state

import (
	"errors"
	"fmt"
)

// ErrTransition 表示 apply 成功 entry 不能无歧义地应用到严格 state 基线。
var ErrTransition = errors.New("state entry transition failed")

// EntryUpdate 描述一次成功 file action 对 state v1 entry 的完整 upsert。
// PreviousKey 非空时，旧 key 与新 entry 必须在同一个 Snapshot transition 中迁移。
type EntryUpdate struct {
	Key         string
	PreviousKey string
	Module      string
	Kind        Kind
	Source      string
	LinkDest    string
	AppliedAt   string
}

// TransitionEntries 在 missing 或 strict loaded 基线上原子应用成功 entry upsert 与 delete。
// 返回值不共享基线 map；未涉及 entries 与全部 run_once 原样保留。changed=false 表示调用方
// 不需要 Store，仍返回一个有效且等价的 Snapshot。
func TransitionEntries(loaded Loaded, updates []EntryUpdate, deletes ...string) (Snapshot, bool, error) {
	baseline, err := transitionBaseline(loaded)
	if err != nil {
		return Snapshot{}, false, err
	}
	validated, err := validateEntryUpdates(baseline, updates)
	if err != nil {
		return Snapshot{}, false, err
	}
	if err := validateEntryDeletes(baseline, updates, deletes); err != nil {
		return Snapshot{}, false, err
	}

	candidate := cloneSnapshot(baseline)
	for index, update := range updates {
		if update.PreviousKey != "" {
			delete(candidate.entries, update.PreviousKey)
		}
		candidate.entries[update.Key] = validated[index]
	}
	for _, key := range deletes {
		delete(candidate.entries, key)
	}
	if snapshotsEqual(baseline, candidate) {
		return candidate, false, nil
	}
	return candidate, true, nil
}

func validateEntryDeletes(baseline Snapshot, updates []EntryUpdate, deletes []string) error {
	reserved := make(map[string]struct{}, len(updates)*2)
	for _, update := range updates {
		reserved[update.Key] = struct{}{}
		if update.PreviousKey != "" {
			reserved[update.PreviousKey] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(deletes))
	for index, key := range deletes {
		if err := validateTargetKey(key); err != nil {
			return fmt.Errorf("%w: delete %d key %q: %w", ErrTransition, index, key, err)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate delete key %q", ErrTransition, key)
		}
		seen[key] = struct{}{}
		if _, exists := reserved[key]; exists {
			return fmt.Errorf("%w: key %q is both updated and deleted", ErrTransition, key)
		}
		if _, exists := baseline.entries[key]; !exists {
			return fmt.Errorf("%w: delete key %q is absent", ErrTransition, key)
		}
	}
	return nil
}

func transitionBaseline(loaded Loaded) (Snapshot, error) {
	switch loaded.Status() {
	case StatusMissing:
		return Snapshot{
			version: 1,
			entries: make(map[string]Entry),
			runOnce: make(map[string]RunOnceRecord),
			valid:   true,
		}, nil
	case StatusLoaded:
		snapshot, ok := loaded.Snapshot()
		if !ok {
			return Snapshot{}, fmt.Errorf("%w: loaded baseline has no valid Snapshot", ErrTransition)
		}
		return cloneSnapshot(snapshot), nil
	default:
		return Snapshot{}, fmt.Errorf("%w: invalid load status %d", ErrTransition, loaded.Status())
	}
}

func validateEntryUpdates(baseline Snapshot, updates []EntryUpdate) ([]Entry, error) {
	entries := make([]Entry, len(updates))
	keys := make(map[string]struct{}, len(updates))
	previousKeys := make(map[string]struct{}, len(updates))
	for index, update := range updates {
		if err := validateTargetKey(update.Key); err != nil {
			return nil, fmt.Errorf("%w: update %d key %q: %w", ErrTransition, index, update.Key, err)
		}
		if _, exists := keys[update.Key]; exists {
			return nil, fmt.Errorf("%w: duplicate update key %q", ErrTransition, update.Key)
		}
		keys[update.Key] = struct{}{}

		if update.PreviousKey != "" {
			if err := validateTargetKey(update.PreviousKey); err != nil {
				return nil, fmt.Errorf(
					"%w: update %d previous key %q: %w",
					ErrTransition,
					index,
					update.PreviousKey,
					err,
				)
			}
			if update.PreviousKey == update.Key {
				return nil, fmt.Errorf("%w: update key %q equals PreviousKey", ErrTransition, update.Key)
			}
			if _, exists := previousKeys[update.PreviousKey]; exists {
				return nil, fmt.Errorf("%w: PreviousKey %q is reused", ErrTransition, update.PreviousKey)
			}
			previousKeys[update.PreviousKey] = struct{}{}
			if _, exists := baseline.entries[update.PreviousKey]; !exists {
				return nil, fmt.Errorf("%w: PreviousKey %q is absent", ErrTransition, update.PreviousKey)
			}
			if _, exists := baseline.entries[update.Key]; exists {
				return nil, fmt.Errorf("%w: migrated key %q already exists", ErrTransition, update.Key)
			}
		}

		entry, err := entryFromUpdate(update)
		if err != nil {
			return nil, fmt.Errorf("%w: update %d key %q: %w", ErrTransition, index, update.Key, err)
		}
		entries[index] = entry
	}
	for key := range keys {
		if _, exists := previousKeys[key]; exists {
			return nil, fmt.Errorf("%w: key %q is both a new key and PreviousKey", ErrTransition, key)
		}
	}
	return entries, nil
}

func entryFromUpdate(update EntryUpdate) (Entry, error) {
	if update.Kind != KindSymlink && update.Kind != KindScaffold {
		return Entry{}, fmt.Errorf("unsupported apply entry kind %q", update.Kind)
	}
	kind := string(update.Kind)
	raw := rawEntry{
		Module:    &update.Module,
		Kind:      &kind,
		Source:    &update.Source,
		AppliedAt: &update.AppliedAt,
	}
	if update.Kind == KindSymlink {
		raw.LinkDest = &update.LinkDest
	} else if update.LinkDest != "" {
		return Entry{}, fmt.Errorf("scaffold update must not contain link_dest")
	}
	return validateEntry(raw)
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	entries := make(map[string]Entry, len(snapshot.entries))
	for key, entry := range snapshot.entries {
		entries[key] = entry
	}
	runOnce := make(map[string]RunOnceRecord, len(snapshot.runOnce))
	for key, record := range snapshot.runOnce {
		runOnce[key] = record
	}
	return Snapshot{
		version: snapshot.version,
		entries: entries,
		runOnce: runOnce,
		valid:   snapshot.valid,
	}
}
