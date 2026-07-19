package planner

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/mianm12/dotfiles/internal/state"
)

type resolvedDesired struct {
	desired    Desired
	resolution paths.TargetResolution
}

type resolvedHistory struct {
	state      HistoricalState
	targetPath string
	resolution paths.TargetResolution
}

// ObserveProfileTargets 先校验完整 desired/state 的 M1 类型与 target identity，再只读观测 current
// desired 和 orphan leaf。单个历史 alias 会匹配 current desired；多个历史 key 指向同一 identity
// 时 fail closed，且任何失败都不返回部分结果。
func ObserveProfileTargets(
	home string,
	entries []manifest.DesiredEntry,
	loaded state.Loaded,
) (ObservedProfile, error) {
	return observeProfileTargets(home, entries, loaded, nil)
}

func observeProfileTargets(
	home string,
	entries []manifest.DesiredEntry,
	loaded state.Loaded,
	regularDigestTargets map[string]struct{},
) (ObservedProfile, error) {
	if home == "" || !filepath.IsAbs(home) {
		return ObservedProfile{}, fmt.Errorf("effective HOME %q must be a non-empty absolute path", home)
	}
	cleanHome := filepath.Clean(home)
	desired, err := resolveDesiredEntries(entries)
	if err != nil {
		return ObservedProfile{}, err
	}
	history, err := resolveHistoricalEntries(cleanHome, loaded)
	if err != nil {
		return ObservedProfile{}, err
	}
	if err := rejectDuplicateDesiredIdentities(desired); err != nil {
		return ObservedProfile{}, err
	}
	if err := rejectDuplicateHistoricalIdentities(history); err != nil {
		return ObservedProfile{}, err
	}

	matchedHistory := make([]bool, len(history))
	targets := make([]ObservedTarget, 0, len(desired))
	for _, candidate := range desired {
		_, requireDigest := regularDigestTargets[candidate.desired.TargetPath]
		observed, err := observeTarget(candidate.desired.TargetPath, requireDigest)
		if err != nil {
			return ObservedProfile{}, err
		}
		target := ObservedTarget{
			Desired:    candidate.desired,
			Resolution: candidate.resolution,
			Observed:   observed,
		}
		for index, historical := range history {
			if !candidate.resolution.Equal(historical.resolution) {
				continue
			}
			target.State = historical.state
			target.HasState = true
			matchedHistory[index] = true
			break
		}
		targets = append(targets, target)
	}

	orphans := make([]OrphanTarget, 0, len(history))
	for index, historical := range history {
		if matchedHistory[index] {
			continue
		}
		observed, err := ObserveTarget(historical.targetPath)
		if err != nil {
			return ObservedProfile{}, err
		}
		orphans = append(orphans, OrphanTarget{
			TargetPath: historical.targetPath,
			Resolution: historical.resolution,
			State:      historical.state,
			Observed:   observed,
		})
	}
	return ObservedProfile{targets: targets, orphans: orphans}, nil
}

func resolveDesiredEntries(entries []manifest.DesiredEntry) ([]resolvedDesired, error) {
	desired := make([]Desired, len(entries))
	for index, entry := range entries {
		kind, err := desiredKind(entry.Kind)
		if err != nil {
			return nil, fmt.Errorf("module %q source %q: %w", entry.Module, entry.Source, err)
		}
		if entry.TargetPath == "" || !filepath.IsAbs(entry.TargetPath) {
			return nil, fmt.Errorf(
				"module %q source %q target path %q must be a non-empty absolute path",
				entry.Module,
				entry.Source,
				entry.TargetPath,
			)
		}
		desired[index] = Desired{
			Module:     entry.Module,
			Source:     entry.Source,
			SourcePath: entry.SourcePath,
			Target:     entry.Target,
			TargetPath: filepath.Clean(entry.TargetPath),
			Kind:       kind,
			Mode:       entry.Mode,
			Content:    append([]byte(nil), entry.Content...),
		}
	}
	slices.SortFunc(desired, func(left, right Desired) int {
		if order := strings.Compare(left.Module, right.Module); order != 0 {
			return order
		}
		return strings.Compare(left.Source, right.Source)
	})
	resolved := make([]resolvedDesired, len(desired))
	for index, candidate := range desired {
		resolution, err := paths.ResolveTarget(candidate.TargetPath)
		if err != nil {
			return nil, fmt.Errorf(
				"resolve desired module %q source %q target %q: %w",
				candidate.Module,
				candidate.Source,
				candidate.Target,
				err,
			)
		}
		resolved[index] = resolvedDesired{desired: candidate, resolution: resolution}
	}
	return resolved, nil
}

func desiredKind(kind manifest.FileKind) (DesiredKind, error) {
	switch kind {
	case manifest.FileKindLink:
		return DesiredLink, nil
	case manifest.FileKindScaffold:
		return DesiredScaffold, nil
	default:
		return "", fmt.Errorf("unsupported desired kind %q", kind)
	}
}

func resolveHistoricalEntries(home string, loaded state.Loaded) ([]resolvedHistory, error) {
	switch loaded.Status() {
	case state.StatusMissing:
		return nil, nil
	case state.StatusLoaded:
		// continue below
	default:
		return nil, fmt.Errorf("state input is neither missing nor strictly loaded")
	}
	snapshot, ok := loaded.Snapshot()
	if !ok {
		return nil, fmt.Errorf("strictly loaded state has no valid snapshot")
	}
	keys := snapshot.EntryKeys()
	resolved := make([]resolvedHistory, 0, len(keys))
	for _, key := range keys {
		entry, ok := snapshot.Entry(key)
		if !ok {
			return nil, fmt.Errorf("strict state entry %q disappeared", key)
		}
		historical, err := historicalState(key, entry)
		if err != nil {
			return nil, err
		}
		targetPath := filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(key, "~/")))
		resolution, err := paths.ResolveTarget(targetPath)
		if err != nil {
			return nil, fmt.Errorf("resolve historical state target %q: %w", key, err)
		}
		resolved = append(resolved, resolvedHistory{
			state:      historical,
			targetPath: targetPath,
			resolution: resolution,
		})
	}
	return resolved, nil
}

func historicalState(key string, entry state.Entry) (HistoricalState, error) {
	var kind StateKind
	switch entry.Kind() {
	case state.KindSymlink:
		kind = StateSymlink
	case state.KindScaffold:
		kind = StateScaffold
	case state.KindRendered:
		return HistoricalState{}, fmt.Errorf("historical state target %q uses rendered kind unsupported in M1", key)
	default:
		return HistoricalState{}, fmt.Errorf("historical state target %q uses unsupported kind %q", key, entry.Kind())
	}
	return HistoricalState{
		Key:       key,
		Module:    entry.Module(),
		Kind:      kind,
		Source:    entry.Source(),
		LinkDest:  entry.LinkDest(),
		AppliedAt: entry.AppliedAt(),
	}, nil
}

func rejectDuplicateDesiredIdentities(entries []resolvedDesired) error {
	for left := range entries {
		for right := left + 1; right < len(entries); right++ {
			if entries[left].resolution.Equal(entries[right].resolution) {
				return fmt.Errorf(
					"%w: desired module %q source %q and module %q source %q have equal target identity",
					paths.ErrTargetOverlap,
					entries[left].desired.Module,
					entries[left].desired.Source,
					entries[right].desired.Module,
					entries[right].desired.Source,
				)
			}
		}
	}
	return nil
}

func rejectDuplicateHistoricalIdentities(entries []resolvedHistory) error {
	for left := range entries {
		for right := left + 1; right < len(entries); right++ {
			if entries[left].resolution.Equal(entries[right].resolution) {
				return fmt.Errorf(
					"%w: %w: state keys %q and %q have equal target identity",
					state.ErrCorrupt,
					state.ErrTargetIdentityConflict,
					entries[left].state.Key,
					entries[right].state.Key,
				)
			}
		}
	}
	return nil
}
