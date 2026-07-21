package add

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
)

const (
	targetTemporaryDirectoryPrefix = ".dot-add-link-"
	targetTemporaryLinkName        = "link"
)

type linkOperations struct {
	publication publicationOperations
	mkdirTemp   func(string, string) (string, error)
	symlink     func(string, string) error
	readlink    func(string) (string, error)
	rename      func(string, string) error
	remove      func(string) error
}

type linkExecutionSeal struct{}

var successfulLinkExecutionSeal = &linkExecutionSeal{}

type linkItemResult struct {
	item            ItemPlan
	sourcePublished bool
	targetCommitted bool
	seal            *linkExecutionSeal
}

func (result linkItemResult) Valid() bool {
	return result.seal == successfulLinkExecutionSeal && result.item.Valid() &&
		(!result.targetCommitted || result.sourcePublished)
}

func defaultLinkOperations() linkOperations {
	return linkOperations{
		publication: defaultPublicationOperations(),
		mkdirTemp:   os.MkdirTemp,
		symlink:     os.Symlink,
		readlink:    os.Readlink,
		rename:      os.Rename,
		remove:      os.Remove,
	}
}

type addTargetEvidence struct {
	info       fs.FileInfo
	resolution paths.TargetResolution
}

type targetTemporaryOwnership struct {
	directory     string
	directoryInfo fs.FileInfo
	link          string
	linkInfo      fs.FileInfo
	linkText      string
}

func executeLinkItem(
	control paths.ControlPlanePaths,
	item ItemPlan,
	operations linkOperations,
) (linkItemResult, error) {
	result := linkItemResult{item: item, seal: successfulLinkExecutionSeal}
	if !item.Valid() || item.Kind() != manifest.FileKindLink {
		return linkItemResult{}, fmt.Errorf("add link execution requires a validated link item")
	}
	if err := validateLinkOperations(operations); err != nil {
		return linkItemResult{}, err
	}
	evidence, err := validateAddTarget(control, item, nil, operations.publication)
	if err != nil {
		return result, fmt.Errorf("validate add target before source publication: %w", err)
	}

	publication, err := publishSource(item, operations.publication)
	if publication.Valid() {
		result.sourcePublished = true
	}
	if err != nil {
		if publication.Valid() && publication.Created() {
			err = errors.Join(err, cleanupSourcePublication(publication, operations.publication))
		}
		return result, err
	}
	failBeforeCommit := func(primary error, temporary targetTemporaryOwnership) (linkItemResult, error) {
		return result, errors.Join(
			primary,
			cleanupTargetTemporary(temporary, operations),
			cleanupSourcePublication(publication, operations.publication),
		)
	}

	temporaryDirectory, err := operations.mkdirTemp(filepath.Dir(item.targetPath), targetTemporaryDirectoryPrefix)
	if err != nil {
		return failBeforeCommit(fmt.Errorf("create add target temporary directory: %w", err), targetTemporaryOwnership{})
	}
	temporary := targetTemporaryOwnership{directory: temporaryDirectory}
	directoryInfo, err := operations.publication.lstat(temporaryDirectory)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&fs.ModeSymlink != 0 {
		if err == nil {
			err = fmt.Errorf("created temporary path is not a real directory")
		}
		return failBeforeCommit(fmt.Errorf("inspect add target temporary directory: %w", err), temporary)
	}
	temporary.directoryInfo = directoryInfo
	temporaryLink := filepath.Join(temporaryDirectory, targetTemporaryLinkName)
	temporary.link = temporaryLink
	temporary.linkText = item.sourcePath
	if err := operations.symlink(item.sourcePath, temporaryLink); err != nil {
		return failBeforeCommit(fmt.Errorf("prepare add target symlink: %w", err), temporary)
	}
	temporary.linkInfo, err = operations.publication.lstat(temporaryLink)
	if err != nil || temporary.linkInfo.Mode()&fs.ModeSymlink == 0 {
		if err == nil {
			err = fmt.Errorf("prepared target temporary path is not a symlink")
		}
		return failBeforeCommit(fmt.Errorf("inspect prepared add target symlink: %w", err), temporary)
	}
	linkText, err := operations.readlink(temporaryLink)
	if err != nil || linkText != item.sourcePath {
		if err == nil {
			err = fmt.Errorf("prepared link text %q does not equal source %q", linkText, item.sourcePath)
		}
		return failBeforeCommit(fmt.Errorf("validate prepared add target symlink: %w", err), temporary)
	}

	if err := validateSourceAncestors(item, operations.publication); err != nil {
		return failBeforeCommit(fmt.Errorf("revalidate add link source topology: %w", err), temporary)
	}
	if _, err := validateRegularFile(
		item.sourcePath,
		publication.info,
		item.snapshot.content,
		item.snapshot.mode,
		operations.publication,
	); err != nil {
		return failBeforeCommit(fmt.Errorf("revalidate add link source: %w", err), temporary)
	}
	if _, err := validateAddTarget(control, item, &evidence, operations.publication); err != nil {
		return failBeforeCommit(fmt.Errorf("revalidate add target before commit: %w", err), temporary)
	}
	if err := operations.rename(temporaryLink, item.targetPath); err != nil {
		return failBeforeCommit(fmt.Errorf("commit add target symlink: %w", err), temporary)
	}
	result.targetCommitted = true
	if err := cleanupTargetTemporary(temporary, operations); err != nil {
		return result, fmt.Errorf("cleanup committed add target temporary directory: %w", err)
	}
	return result, nil
}

func validateLinkOperations(operations linkOperations) error {
	if err := validatePublicationOperations(operations.publication); err != nil {
		return err
	}
	if operations.mkdirTemp == nil || operations.symlink == nil || operations.readlink == nil ||
		operations.rename == nil || operations.remove == nil {
		return fmt.Errorf("add link execution operations are incomplete")
	}
	return nil
}

func validateAddTarget(
	control paths.ControlPlanePaths,
	item ItemPlan,
	expected *addTargetEvidence,
	operations publicationOperations,
) (addTargetEvidence, error) {
	if _, err := paths.ValidatePathBoundaries(control, []paths.LabeledTarget{{
		Label: "add link target " + item.target,
		Path:  item.targetPath,
	}}); err != nil {
		return addTargetEvidence{}, fmt.Errorf("validate target/control boundary: %w", err)
	}
	resolution, err := paths.ResolveTarget(item.targetPath)
	if err != nil {
		return addTargetEvidence{}, fmt.Errorf("resolve target topology: %w", err)
	}
	if !item.snapshot.MatchesTargetIdentity(resolution.Identity()) {
		return addTargetEvidence{}, fmt.Errorf("target identity no longer matches add snapshot")
	}
	if expected != nil && !expected.resolution.SameTopology(resolution) {
		return addTargetEvidence{}, fmt.Errorf("target ancestor topology changed")
	}
	info, err := operations.lstat(item.targetPath)
	if err != nil {
		return addTargetEvidence{}, fmt.Errorf("inspect target: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != item.snapshot.mode {
		return addTargetEvidence{}, fmt.Errorf("target is not the planned ordinary file mode")
	}
	if expected != nil && !os.SameFile(expected.info, info) {
		return addTargetEvidence{}, fmt.Errorf("target file identity changed")
	}
	content, err := operations.readFile(item.targetPath)
	if err != nil {
		return addTargetEvidence{}, fmt.Errorf("read target: %w", err)
	}
	if !bytes.Equal(content, item.snapshot.content) {
		return addTargetEvidence{}, fmt.Errorf("target bytes changed")
	}
	after, err := operations.lstat(item.targetPath)
	if err != nil {
		return addTargetEvidence{}, fmt.Errorf("reinspect target: %w", err)
	}
	if !after.Mode().IsRegular() || after.Mode().Perm() != item.snapshot.mode || !os.SameFile(info, after) {
		return addTargetEvidence{}, fmt.Errorf("target changed while validating")
	}
	return addTargetEvidence{info: after, resolution: resolution}, nil
}

func cleanupTargetTemporary(temporary targetTemporaryOwnership, operations linkOperations) error {
	var cleanupErrors []error
	if temporary.link != "" {
		info, err := operations.publication.lstat(temporary.link)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect add target temporary symlink: %w", err))
		case temporary.linkInfo == nil || info.Mode()&fs.ModeSymlink == 0 || !os.SameFile(temporary.linkInfo, info):
			cleanupErrors = append(cleanupErrors, fmt.Errorf("refuse to clean changed add target temporary symlink %q", temporary.link))
		default:
			linkText, readErr := operations.readlink(temporary.link)
			if readErr != nil || linkText != temporary.linkText {
				if readErr == nil {
					readErr = fmt.Errorf("link text changed")
				}
				cleanupErrors = append(cleanupErrors, fmt.Errorf("refuse to clean changed add target temporary symlink %q: %w", temporary.link, readErr))
			} else if removeErr := operations.remove(temporary.link); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove add target temporary symlink: %w", removeErr))
			}
		}
	}
	if temporary.directory != "" {
		info, err := operations.publication.lstat(temporary.directory)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect add target temporary directory: %w", err))
		case temporary.directoryInfo == nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 ||
			!os.SameFile(temporary.directoryInfo, info) || info.Mode().Perm() != temporary.directoryInfo.Mode().Perm():
			cleanupErrors = append(cleanupErrors, fmt.Errorf("refuse to clean changed add target temporary directory %q", temporary.directory))
		case err == nil:
			if removeErr := operations.remove(temporary.directory); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove add target temporary directory: %w", removeErr))
			}
		}
	}
	return errors.Join(cleanupErrors...)
}
