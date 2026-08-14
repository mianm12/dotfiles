package converge

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mianm12/dotfiles/internal/core/config"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
	"github.com/mianm12/dotfiles/internal/core/state"
)

type desiredPlacement struct {
	key            state.Key
	kind           placementKind
	target         corepaths.Target
	path           string
	source         string
	destination    string
	controlOverlap bool
}

type placementKind uint8

const (
	placementLink placementKind = iota
	placementLocal
)

type activeObservation struct {
	local  localKind
	actual actual
}

type stateLinkObservation struct {
	target         corepaths.Target
	path           string
	actual         actual
	controlOverlap bool
}

type loopInput struct {
	desired           []desiredPlacement
	state             state.Snapshot
	active            map[state.Key]activeObservation
	stateLinks        map[state.Key]stateLinkObservation
	incompleteModules map[string]struct{}
}

func buildLines(request planRequest) ([]planned, error) {
	home, err := validatePlanRequest(request)
	if err != nil {
		return nil, err
	}
	desired, err := resolveDesired(home, request.Controls, request.Modules)
	if err != nil {
		return nil, err
	}
	input, err := observeLoopInput(
		request.Controls,
		desired,
		request.State,
		request.IncompleteModules,
	)
	if err != nil {
		return nil, err
	}
	return decide(input), nil
}

func observeLoopInput(
	controls corepaths.ResolvedControls,
	desired []desiredPlacement,
	snapshot state.Snapshot,
	incompleteModules map[string]struct{},
) (loopInput, error) {
	input := loopInput{
		desired:           append([]desiredPlacement(nil), desired...),
		state:             snapshot,
		active:            make(map[state.Key]activeObservation, len(desired)),
		stateLinks:        make(map[state.Key]stateLinkObservation, len(snapshot.Links)),
		incompleteModules: incompleteModules,
	}

	for _, placement := range desired {
		if placement.kind == placementLocal {
			local, err := observeLocal(placement.path)
			if err != nil {
				return loopInput{}, err
			}
			input.active[placement.key] = activeObservation{local: local}
			continue
		}
		observed, err := observeLink(placement.path)
		if err != nil {
			return loopInput{}, err
		}
		input.active[placement.key] = activeObservation{actual: observed}
	}

	for index, placement := range input.desired {
		overlaps, overlapErr := controls.TargetOverlaps(snapshot.Home, placement.target)
		if overlapErr != nil {
			return loopInput{}, overlapErr
		}
		input.desired[index].controlOverlap = overlaps
	}

	for _, key := range stateKeys(snapshot) {
		record := snapshot.Links[key]
		current, err := corepaths.ResolveStoredTarget(record.Target)
		if err != nil {
			return loopInput{}, fmt.Errorf(
				"resolve state link %s: %w",
				placementLabel(key.ModuleID, key.PlacementID),
				err,
			)
		}
		path, err := current.Absolute(snapshot.Home)
		if err != nil {
			return loopInput{}, fmt.Errorf(
				"expand state link %s: %w",
				placementLabel(key.ModuleID, key.PlacementID),
				err,
			)
		}
		overlaps, err := controls.TargetOverlaps(snapshot.Home, current)
		if err != nil {
			return loopInput{}, err
		}
		observed, err := observeLink(path)
		if err != nil {
			return loopInput{}, fmt.Errorf(
				"inspect state link %s: %w",
				placementLabel(key.ModuleID, key.PlacementID),
				err,
			)
		}
		input.stateLinks[key] = stateLinkObservation{
			target:         current,
			path:           path,
			actual:         observed,
			controlOverlap: overlaps,
		}
	}
	return input, nil
}

func decide(input loopInput) []planned {
	used := recordUsage(input.desired, input.state)
	activeSkips := make(map[int]string)
	staleSkips := make(map[state.Key]string)
	var lines []planned

	markActiveSkip := func(index int, reason string) {
		if _, exists := activeSkips[index]; !exists {
			activeSkips[index] = reason
		}
	}
	markStaleSkip := func(key state.Key, reason string) {
		if _, exists := staleSkips[key]; !exists {
			staleSkips[key] = reason
		}
	}

	for left := range input.desired {
		for right := left + 1; right < len(input.desired); right++ {
			if !corepaths.TargetsConflict(input.desired[left].target, input.desired[right].target) {
				continue
			}
			reason := "desired targets are lexically equal or nested"
			markActiveSkip(left, reason)
			markActiveSkip(right, reason)
		}
	}

	for _, key := range stateKeys(input.state) {
		stale := input.stateLinks[key].target
		for index, desired := range input.desired {
			if used[key] && desired.key == key {
				continue
			}
			if !corepaths.TargetsConflict(stale, desired.target) {
				continue
			}
			markActiveSkip(index, "desired target is lexically equal or nested with stale "+
				placementLabel(key.ModuleID, key.PlacementID))
			markStaleSkip(key, "stale target is lexically equal or nested with desired "+
				placementLabel(desired.key.ModuleID, desired.key.PlacementID))
		}
	}

	for index, desired := range input.desired {
		key := desired.key
		subject := subjectForDesired(desired)
		if desired.kind == placementLocal {
			if _, hasRecord := input.state.Links[key]; hasRecord {
				markActiveSkip(index, "existing link ownership conflicts with desired local")
			}
		}
		if desired.controlOverlap {
			markActiveSkip(index, "target overlaps a protected control path")
		}
		if reason, skipped := activeSkips[index]; skipped {
			lines = append(lines, skipLine(subject, reason))
			continue
		}
		if line, skip := decideActive(desired, input.active[key], input.state.Links[key], used[key]); skip != nil {
			lines = append(lines, skipLine(subject, skip.Reason))
			continue
		} else if line != nil {
			lines = append(lines, *line)
		}
	}

	for _, key := range stateKeys(input.state) {
		if used[key] {
			continue
		}
		if _, incomplete := input.incompleteModules[key.ModuleID]; incomplete {
			continue
		}
		subject := subjectForState(key, input.stateLinks[key])
		if reason, skipped := staleSkips[key]; skipped {
			lines = append(lines, skipLine(subject, reason))
			continue
		}
		lines = append(lines, decideStale(input, key))
	}

	sortPlanned(lines)
	return lines
}

func decideActive(
	desired desiredPlacement,
	observed activeObservation,
	record state.LinkRecord,
	hasState bool,
) (*planned, *Line) {
	subject := subjectForDesired(desired)
	if desired.kind == placementLocal {
		switch observed.local {
		case localPresent:
			return nil, nil
		case localUnreachable:
			return nil, skipPtr(
				subject,
				"local target is unreachable because an ancestor is not a directory",
			)
		}
		line := fileLine(desired)
		return &line, nil
	}

	actual := observed.actual
	if actual.kind != actualAbsent && actual.kind != actualSymlink {
		return nil, skipPtr(subject, fmt.Sprintf("actual target is %s", actual.kind))
	}
	if actual.kind == actualAbsent {
		line := linkLine(desired)
		return &line, nil
	}
	if actual.linkDestination == desired.destination {
		if hasState && record == recordForDesired(desired) {
			return nil, nil
		}
		line := recordLine(desired)
		return &line, nil
	}
	if hasState && actual.linkDestination == record.Dest {
		line := replaceLine(desired, record)
		return &line, nil
	}
	return nil, skipPtr(subject, "actual symlink is not explained by desired or state")
}

func decideStale(input loopInput, key state.Key) planned {
	record := input.state.Links[key]
	observed := input.stateLinks[key]
	subject := subjectForState(key, observed)
	if observed.actual.kind == actualAbsent {
		return forgetLine(subject, record, "stale target is absent")
	}
	if observed.controlOverlap {
		return forgetLine(subject, record, "stale target overlaps a protected control path")
	}
	if stateLinkOwned(record, observed) {
		return removeLine(subject, record)
	}
	if observed.actual.kind != actualSymlink {
		return forgetLine(subject, record, fmt.Sprintf("stale target is now %s", observed.actual.kind))
	}
	if observed.actual.linkDestination != record.Dest {
		return forgetLine(subject, record, "stale symlink destination changed")
	}
	return forgetLine(subject, record, "stale ownership evidence no longer matches")
}

func recordUsage(
	desired []desiredPlacement,
	snapshot state.Snapshot,
) map[state.Key]bool {
	used := make(map[state.Key]bool, len(snapshot.Links))
	for _, placement := range desired {
		record, exists := snapshot.Links[placement.key]
		if !exists {
			continue
		}
		if placement.kind == placementLocal || placement.target.Relative() == record.Target {
			used[placement.key] = true
		}
	}
	return used
}

func stateLinkOwned(record state.LinkRecord, observed stateLinkObservation) bool {
	return observed.actual.kind == actualSymlink &&
		observed.actual.linkDestination == record.Dest
}

func resolveDesired(
	home string,
	controls corepaths.ResolvedControls,
	modules []config.Module,
) ([]desiredPlacement, error) {
	pathInputs := make([]corepaths.Placement, 0)
	for _, module := range modules {
		for _, link := range module.Links {
			pathInputs = append(pathInputs, corepaths.Placement{
				Label:  placementLabel(module.ID, link.ID),
				Target: link.Target,
			})
		}
		for _, local := range module.Locals {
			pathInputs = append(pathInputs, corepaths.Placement{
				Label:  placementLabel(module.ID, local.ID),
				Target: local.Target,
			})
		}
	}
	resolved, err := controls.Expand(home, pathInputs)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]corepaths.Target, len(resolved))
	for _, placement := range resolved {
		targets[placement.Label] = placement.Target
	}

	desired := make([]desiredPlacement, 0, len(resolved))
	for _, module := range modules {
		for _, link := range module.Links {
			target := targets[placementLabel(module.ID, link.ID)]
			path, err := target.Absolute(home)
			if err != nil {
				return nil, err
			}
			desired = append(desired, desiredPlacement{
				key:         state.Key{ModuleID: module.ID, PlacementID: link.ID},
				kind:        placementLink,
				target:      target,
				path:        path,
				source:      link.SourcePath,
				destination: link.SourcePath,
			})
		}
		for _, local := range module.Locals {
			target := targets[placementLabel(module.ID, local.ID)]
			path, err := target.Absolute(home)
			if err != nil {
				return nil, err
			}
			desired = append(desired, desiredPlacement{
				key:    state.Key{ModuleID: module.ID, PlacementID: local.ID},
				kind:   placementLocal,
				target: target,
				path:   path,
				source: local.ExamplePath,
			})
		}
	}
	slices.SortFunc(desired, compareDesired)
	return desired, nil
}

func compareDesired(left, right desiredPlacement) int {
	if byModule := strings.Compare(left.key.ModuleID, right.key.ModuleID); byModule != 0 {
		return byModule
	}
	if byPlacement := strings.Compare(left.key.PlacementID, right.key.PlacementID); byPlacement != 0 {
		return byPlacement
	}
	return strings.Compare(left.target.Relative(), right.target.Relative())
}

func validatePlanRequest(request planRequest) (string, error) {
	if request.Home == "" || !filepath.IsAbs(request.Home) {
		return "", fmt.Errorf("planner HOME must be a non-empty absolute path")
	}
	home := filepath.Clean(request.Home)
	if request.State.Home != home {
		return "", fmt.Errorf(
			"planner state HOME %q does not match %q",
			request.State.Home,
			home,
		)
	}
	return home, nil
}

type placementSubject struct {
	moduleID    string
	placementID string
	target      string
	targetID    string
}

func subjectForDesired(desired desiredPlacement) placementSubject {
	return placementSubject{
		moduleID:    desired.key.ModuleID,
		placementID: desired.key.PlacementID,
		target:      desired.path,
		targetID:    desired.target.Relative(),
	}
}

func subjectForState(key state.Key, observed stateLinkObservation) placementSubject {
	return placementSubject{
		moduleID:    key.ModuleID,
		placementID: key.PlacementID,
		target:      observed.path,
		targetID:    observed.target.Relative(),
	}
}

func skipLine(subject placementSubject, reason string) planned {
	return planned{Line: Line{
		Op:          OpSkip,
		ModuleID:    subject.moduleID,
		PlacementID: subject.placementID,
		Target:      subject.target,
		Reason:      reason,
	}}
}

func skipPtr(subject placementSubject, reason string) *Line {
	line := skipLine(subject, reason).Line
	return &line
}

func fileLine(desired desiredPlacement) planned {
	subject := subjectForDesired(desired)
	return planned{
		Line: Line{
			Op:          OpFile,
			ModuleID:    subject.moduleID,
			PlacementID: subject.placementID,
			Target:      subject.target,
		},
		source: desired.source,
	}
}

func linkLine(desired desiredPlacement) planned {
	subject := subjectForDesired(desired)
	return planned{
		Line: Line{
			Op:          OpLink,
			ModuleID:    subject.moduleID,
			PlacementID: subject.placementID,
			Target:      subject.target,
		},
		dest:     desired.destination,
		targetID: subject.targetID,
	}
}

func recordLine(desired desiredPlacement) planned {
	subject := subjectForDesired(desired)
	return planned{
		Line: Line{
			Op:          OpRecord,
			ModuleID:    subject.moduleID,
			PlacementID: subject.placementID,
			Target:      subject.target,
		},
		dest:     desired.destination,
		targetID: subject.targetID,
	}
}

func replaceLine(desired desiredPlacement, record state.LinkRecord) planned {
	subject := subjectForDesired(desired)
	return planned{
		Line: Line{
			Op:          OpReplace,
			ModuleID:    subject.moduleID,
			PlacementID: subject.placementID,
			Target:      subject.target,
		},
		dest:       desired.destination,
		beforeDest: record.Dest,
		targetID:   subject.targetID,
	}
}

func removeLine(subject placementSubject, record state.LinkRecord) planned {
	return planned{
		Line: Line{
			Op:          OpRemove,
			ModuleID:    subject.moduleID,
			PlacementID: subject.placementID,
			Target:      subject.target,
		},
		beforeDest: record.Dest,
		targetID:   subject.targetID,
	}
}

func forgetLine(subject placementSubject, record state.LinkRecord, reason string) planned {
	return planned{
		Line: Line{
			Op:          OpForget,
			ModuleID:    subject.moduleID,
			PlacementID: subject.placementID,
			Target:      subject.target,
			Reason:      reason,
		},
		beforeDest: record.Dest,
		targetID:   subject.targetID,
	}
}

func recordForDesired(desired desiredPlacement) state.LinkRecord {
	return state.LinkRecord{
		Target: desired.target.Relative(),
		Dest:   desired.destination,
	}
}

func sortPlanned(lines []planned) {
	slices.SortStableFunc(lines, func(left, right planned) int {
		if byPhase := linePhase(left.Op) - linePhase(right.Op); byPhase != 0 {
			return byPhase
		}
		if byModule := strings.Compare(left.ModuleID, right.ModuleID); byModule != 0 {
			return byModule
		}
		if byControl := strings.Compare(left.Control, right.Control); byControl != 0 {
			return byControl
		}
		if byPlacement := strings.Compare(left.PlacementID, right.PlacementID); byPlacement != 0 {
			return byPlacement
		}
		if byTarget := strings.Compare(left.Target, right.Target); byTarget != 0 {
			return byTarget
		}
		return strings.Compare(string(left.Op), string(right.Op))
	})
}

func linePhase(op Op) int {
	switch op {
	case OpSkip:
		return 0
	case OpChmod:
		return 1
	case OpFile, OpLink:
		return 2
	case OpReplace:
		return 3
	case OpRemove:
		return 4
	case OpRecord, OpForget:
		return 5
	default:
		return 6
	}
}

func stateKeys(snapshot state.Snapshot) []state.Key {
	keys := make([]state.Key, 0, len(snapshot.Links))
	for key := range snapshot.Links {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareStateKey)
	return keys
}

func compareStateKey(left, right state.Key) int {
	if byModule := strings.Compare(left.ModuleID, right.ModuleID); byModule != 0 {
		return byModule
	}
	return strings.Compare(left.PlacementID, right.PlacementID)
}

func placementLabel(moduleID, placementID string) string {
	return moduleID + "/" + placementID
}

func publicLines(lines []planned) []Line {
	result := make([]Line, len(lines))
	for index, line := range lines {
		result[index] = line.Line
	}
	return result
}
