package add

import (
	"errors"
	"fmt"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
)

// executeScaffoldItem 发布一次性蓝本，但始终把 HOME target 当作用户产物而不修改。
func executeScaffoldItem(
	control paths.ControlPlanePaths,
	item ItemPlan,
	operations publicationOperations,
) (linkItemResult, error) {
	result := linkItemResult{item: item, seal: successfulLinkExecutionSeal}
	if !item.Valid() || item.Kind() != manifest.FileKindScaffold {
		return linkItemResult{}, fmt.Errorf("add scaffold execution requires a validated scaffold item")
	}
	if err := validatePublicationOperations(operations); err != nil {
		return linkItemResult{}, err
	}
	evidence, err := validateAddTarget(control, item, nil, operations)
	if err != nil {
		return result, fmt.Errorf("validate add scaffold target before source publication: %w", err)
	}

	publication, err := publishSource(item, operations)
	if publication.Valid() {
		result.sourcePublished = true
	}
	if err != nil {
		if publication.Valid() && publication.Created() {
			err = errors.Join(err, cleanupSourcePublication(publication, operations))
		}
		return result, err
	}
	failBeforeState := func(primary error) (linkItemResult, error) {
		return result, errors.Join(primary, cleanupSourcePublication(publication, operations))
	}
	if err := validateSourceAncestors(item, operations); err != nil {
		return failBeforeState(fmt.Errorf("revalidate add scaffold source topology: %w", err))
	}
	if _, err := validateRegularFile(
		item.sourcePath,
		publication.info,
		item.snapshot.content,
		item.snapshot.mode,
		operations,
	); err != nil {
		return failBeforeState(fmt.Errorf("revalidate add scaffold source: %w", err))
	}
	if _, err := validateAddTarget(control, item, &evidence, operations); err != nil {
		return failBeforeState(fmt.Errorf("revalidate add scaffold target before state: %w", err))
	}
	result.publication = publication
	result.targetEvidence = evidence
	result.stateReady = true
	return result, nil
}

func revalidateScaffoldStatePrecondition(
	control paths.ControlPlanePaths,
	item ItemPlan,
	result linkItemResult,
	operations publicationOperations,
) error {
	if !result.Valid() || !sameItemPlan(item, result.item) || item.Kind() != manifest.FileKindScaffold ||
		!result.stateReady || result.targetCommitted {
		return fmt.Errorf("add scaffold state precondition requires a valid state-ready result")
	}
	if err := validateSourceAncestors(item, operations); err != nil {
		return fmt.Errorf("revalidate add scaffold source topology before state commit: %w", err)
	}
	if _, err := validateRegularFile(
		item.sourcePath,
		result.publication.info,
		item.snapshot.content,
		item.snapshot.mode,
		operations,
	); err != nil {
		return fmt.Errorf("revalidate add scaffold source before state commit: %w", err)
	}
	if _, err := validateAddTarget(control, item, &result.targetEvidence, operations); err != nil {
		return fmt.Errorf("revalidate add scaffold target before state commit: %w", err)
	}
	return nil
}

func cleanupUncommittedScaffold(result linkItemResult, operations publicationOperations) error {
	if !result.Valid() || result.item.Kind() != manifest.FileKindScaffold || !result.stateReady {
		return fmt.Errorf("cleanup scaffold requires a valid state-ready result")
	}
	return cleanupSourcePublication(result.publication, operations)
}
