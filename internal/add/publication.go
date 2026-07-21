package add

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const sourceTemporaryPattern = ".dot-add-source-*.swp"

type publicationFile interface {
	Name() string
	Write([]byte) (int, error)
	Chmod(fs.FileMode) error
	Sync() error
	Close() error
}

type directorySyncer interface {
	Sync() error
	Close() error
}

type publicationOperations struct {
	createTemp    func(string, string) (publicationFile, error)
	mkdir         func(string, fs.FileMode) error
	lstat         func(string) (fs.FileInfo, error)
	readFile      func(string) ([]byte, error)
	publish       func(string, string) error
	remove        func(string) error
	syncDirectory func(string) error
}

type createdSourceDirectory struct {
	path string
	info fs.FileInfo
}

type sourcePublication struct {
	source      string
	content     []byte
	mode        fs.FileMode
	info        fs.FileInfo
	created     bool
	createdDirs []createdSourceDirectory
	validation  *validationSeal
}

func (publication sourcePublication) Valid() bool {
	return publication.validation == successfulPreflightSeal && publication.source != "" &&
		publication.info != nil && publication.info.Mode().IsRegular() &&
		publication.mode&^fs.ModePerm == 0
}

func (publication sourcePublication) Created() bool {
	return publication.Valid() && publication.created
}

func defaultPublicationOperations() publicationOperations {
	return publicationOperations{
		createTemp: func(directory, pattern string) (publicationFile, error) {
			return os.CreateTemp(directory, pattern)
		},
		mkdir:    os.Mkdir,
		lstat:    os.Lstat,
		readFile: os.ReadFile,
		publish:  os.Link,
		remove:   os.Remove,
		syncDirectory: func(path string) error {
			directory, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open directory %q for sync: %w", path, err)
			}
			if err := directory.Sync(); err != nil {
				_ = directory.Close()
				return fmt.Errorf("sync directory %q: %w", path, err)
			}
			if err := directory.Close(); err != nil {
				return fmt.Errorf("close directory %q after sync: %w", path, err)
			}
			return nil
		},
	}
}

func publishSource(item ItemPlan, operations publicationOperations) (sourcePublication, error) {
	if !item.Valid() || item.Kind() != "link" {
		return sourcePublication{}, fmt.Errorf("add source publication requires a validated link item")
	}
	if err := validatePublicationOperations(operations); err != nil {
		return sourcePublication{}, err
	}
	content := item.snapshot.content
	mode := item.snapshot.mode.Perm()
	if item.sourceExists {
		info, err := validateRegularFile(item.sourcePath, nil, content, mode, operations)
		if err != nil {
			return sourcePublication{}, fmt.Errorf("revalidate equivalent add source: %w", err)
		}
		return sourcePublication{
			source: item.sourcePath, content: append([]byte(nil), content...), mode: mode,
			info: info, validation: successfulPreflightSeal,
		}, nil
	}

	createdDirectories, err := ensureSourceParent(item, operations)
	if err != nil {
		return sourcePublication{}, err
	}
	failBeforePublish := func(primary error, temporary string) (sourcePublication, error) {
		cleanupErr := cleanupTemporaryAndDirectories(temporary, createdDirectories, operations)
		return sourcePublication{}, errors.Join(primary, cleanupErr)
	}

	file, err := operations.createTemp(filepath.Dir(item.sourcePath), sourceTemporaryPattern)
	if err != nil {
		return failBeforePublish(fmt.Errorf("create add source temporary file: %w", err), "")
	}
	temporary := file.Name()
	closed := false
	failFile := func(primary error) (sourcePublication, error) {
		if !closed {
			if closeErr := file.Close(); closeErr != nil {
				primary = errors.Join(primary, fmt.Errorf("close failed add source temporary file: %w", closeErr))
			}
			closed = true
		}
		return failBeforePublish(primary, temporary)
	}
	written, err := file.Write(content)
	if err != nil {
		return failFile(fmt.Errorf("write add source temporary file: %w", err))
	}
	if written != len(content) {
		return failFile(fmt.Errorf("write add source temporary file: %w", io.ErrShortWrite))
	}
	if err := file.Chmod(mode); err != nil {
		return failFile(fmt.Errorf("set add source temporary file mode: %w", err))
	}
	if err := file.Sync(); err != nil {
		return failFile(fmt.Errorf("sync add source temporary file: %w", err))
	}
	closed = true
	if err := file.Close(); err != nil {
		return failBeforePublish(fmt.Errorf("close add source temporary file: %w", err), temporary)
	}
	temporaryInfo, err := validateRegularFile(temporary, nil, content, mode, operations)
	if err != nil {
		return failBeforePublish(fmt.Errorf("validate prepared add source: %w", err), temporary)
	}
	if err := operations.publish(temporary, item.sourcePath); err != nil {
		return failBeforePublish(fmt.Errorf("publish add source without clobber: %w", err), temporary)
	}

	publication := sourcePublication{
		source: item.sourcePath, content: append([]byte(nil), content...), mode: mode,
		info: temporaryInfo, created: true, createdDirs: createdDirectories,
		validation: successfulPreflightSeal,
	}
	if _, err := validateRegularFile(item.sourcePath, temporaryInfo, content, mode, operations); err != nil {
		return publication, fmt.Errorf("validate published add source: %w", err)
	}
	if err := operations.remove(temporary); err != nil {
		return publication, fmt.Errorf("remove published add source temporary file: %w", err)
	}
	if err := operations.syncDirectory(filepath.Dir(item.sourcePath)); err != nil {
		return publication, fmt.Errorf("persist published add source directory: %w", err)
	}
	return publication, nil
}

func validatePublicationOperations(operations publicationOperations) error {
	if operations.createTemp == nil || operations.mkdir == nil || operations.lstat == nil ||
		operations.readFile == nil || operations.publish == nil || operations.remove == nil ||
		operations.syncDirectory == nil {
		return fmt.Errorf("add source publication operations are incomplete")
	}
	return nil
}

func ensureSourceParent(item ItemPlan, operations publicationOperations) ([]createdSourceDirectory, error) {
	relative := filepath.Clean(filepath.FromSlash(item.source))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("add source %q is not module-relative", item.source)
	}
	moduleRoot := item.sourcePath
	for range strings.Split(relative, string(filepath.Separator)) {
		moduleRoot = filepath.Dir(moduleRoot)
	}
	if filepath.Join(moduleRoot, relative) != filepath.Clean(item.sourcePath) {
		return nil, fmt.Errorf("add source path does not match its module-relative source")
	}
	rootInfo, err := operations.lstat(moduleRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect add module root %q: %w", moduleRoot, err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("add module root %q is not a real directory", moduleRoot)
	}

	parentRelative, err := filepath.Rel(moduleRoot, filepath.Dir(item.sourcePath))
	if err != nil {
		return nil, fmt.Errorf("locate add source parent: %w", err)
	}
	created := make([]createdSourceDirectory, 0)
	current := moduleRoot
	if parentRelative == "." {
		return created, nil
	}
	for _, component := range strings.Split(parentRelative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, inspectErr := operations.lstat(current)
		switch {
		case inspectErr == nil:
			if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
				return nil, errors.Join(
					fmt.Errorf("add source ancestor %q is not a real directory", current),
					cleanupCreatedDirectories(created, operations),
				)
			}
		case errors.Is(inspectErr, fs.ErrNotExist):
			if err := operations.mkdir(current, 0o755); err != nil {
				return nil, errors.Join(
					fmt.Errorf("create add source ancestor %q: %w", current, err),
					cleanupCreatedDirectories(created, operations),
				)
			}
			createdInfo, err := operations.lstat(current)
			if err != nil || !createdInfo.IsDir() || createdInfo.Mode()&fs.ModeSymlink != 0 {
				return nil, errors.Join(
					fmt.Errorf("verify created add source ancestor %q: %w", current, err),
					cleanupCreatedDirectories(created, operations),
				)
			}
			created = append(created, createdSourceDirectory{path: current, info: createdInfo})
		default:
			return nil, errors.Join(
				fmt.Errorf("inspect add source ancestor %q: %w", current, inspectErr),
				cleanupCreatedDirectories(created, operations),
			)
		}
	}
	return created, nil
}

func cleanupSourcePublication(publication sourcePublication, operations publicationOperations) error {
	if !publication.Valid() || !publication.created {
		return nil
	}
	_, err := validateRegularFile(
		publication.source,
		publication.info,
		publication.content,
		publication.mode,
		operations,
	)
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return fmt.Errorf("refuse to clean changed add source %q: %w", publication.source, err)
	}
	if _, statErr := operations.lstat(publication.source); statErr == nil {
		if removeErr := operations.remove(publication.source); removeErr != nil {
			return fmt.Errorf("remove uncommitted add source %q: %w", publication.source, removeErr)
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("inspect uncommitted add source %q: %w", publication.source, statErr)
	}
	var cleanupErr error
	if err := operations.syncDirectory(filepath.Dir(publication.source)); err != nil {
		cleanupErr = fmt.Errorf("persist cleanup of add source %q: %w", publication.source, err)
	}
	return errors.Join(cleanupErr, cleanupCreatedDirectories(publication.createdDirs, operations))
}

func cleanupTemporaryAndDirectories(
	temporary string,
	directories []createdSourceDirectory,
	operations publicationOperations,
) error {
	var cleanupErr error
	if temporary != "" {
		if err := operations.remove(temporary); err != nil && !errors.Is(err, fs.ErrNotExist) {
			cleanupErr = fmt.Errorf("remove incomplete add source temporary file: %w", err)
		}
	}
	return errors.Join(cleanupErr, cleanupCreatedDirectories(directories, operations))
}

func cleanupCreatedDirectories(directories []createdSourceDirectory, operations publicationOperations) error {
	var cleanupErrors []error
	for index := len(directories) - 1; index >= 0; index-- {
		directory := directories[index]
		info, err := operations.lstat(directory.path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect created add source directory %q: %w", directory.path, err))
			continue
		}
		if !info.IsDir() || !os.SameFile(directory.info, info) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("refuse to clean changed add source directory %q", directory.path))
			continue
		}
		if err := operations.remove(directory.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove created add source directory %q: %w", directory.path, err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func validateRegularFile(
	path string,
	expectedInfo fs.FileInfo,
	content []byte,
	mode fs.FileMode,
	operations publicationOperations,
) (fs.FileInfo, error) {
	info, err := operations.lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != mode {
		return nil, fmt.Errorf("%q is not the expected regular file mode %04o", path, mode)
	}
	if expectedInfo != nil && !os.SameFile(expectedInfo, info) {
		return nil, fmt.Errorf("%q file identity changed", path)
	}
	actual, err := operations.readFile(path)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(actual, content) {
		return nil, fmt.Errorf("%q bytes changed", path)
	}
	after, err := operations.lstat(path)
	if err != nil {
		return nil, err
	}
	if !after.Mode().IsRegular() || after.Mode().Perm() != mode || !os.SameFile(info, after) {
		return nil, fmt.Errorf("%q changed while validating", path)
	}
	return after, nil
}
