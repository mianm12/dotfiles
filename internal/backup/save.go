package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mianm12/dotfiles/internal/storage"
)

const sha256Prefix = "sha256:"

// ErrEvidenceMismatch 表示 backup source 已明确不再满足调用方提供的计划证据。
// open/copy/chmod/sync/cleanup 等 IO 错误不属于此分类。
var ErrEvidenceMismatch = errors.New("backup source evidence mismatch")

// IsPureEvidenceMismatch 只在整个错误树均由明确 evidence 失配构成时返回 true。
func IsPureEvidenceMismatch(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !IsPureEvidenceMismatch(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return IsPureEvidenceMismatch(wrapped.Unwrap())
	}
	return errors.Is(err, ErrEvidenceMismatch)
}

type directorySyncer interface {
	Sync() error
	Close() error
}

type directorySyncOpener func(string) (directorySyncer, error)

var openDirectoryForSync directorySyncOpener = func(path string) (directorySyncer, error) {
	return os.Open(path)
}

// SaveRegular 保存 source 的普通文件字节与九个权限位，并核对计划摘要。
func (batch *Batch) SaveRegular(
	source string,
	relative string,
	expectedHash string,
	expectedMode fs.FileMode,
) (string, error) {
	if err := validateRegularEvidence(expectedHash, expectedMode); err != nil {
		return "", err
	}
	destination, err := batch.prepareDestination(relative)
	if err != nil {
		return "", err
	}

	sourceInfo, err := inspectAbsoluteSource(source)
	if err != nil {
		return "", err
	}
	if !sourceInfo.Mode().IsRegular() {
		return "", fmt.Errorf("%w: backup source %q is not a regular file", ErrEvidenceMismatch, filepath.Clean(source))
	}
	if sourceInfo.Mode().Perm() != expectedMode {
		return "", fmt.Errorf(
			"%w: backup source %q permissions changed: got %04o, want %04o",
			ErrEvidenceMismatch,
			filepath.Clean(source), sourceInfo.Mode().Perm(), expectedMode,
		)
	}

	sourceFile, err := os.Open(filepath.Clean(source))
	if err != nil {
		return "", fmt.Errorf("open backup source %q: %w", filepath.Clean(source), err)
	}
	openedInfo, err := sourceFile.Stat()
	if err != nil {
		_ = sourceFile.Close()
		return "", fmt.Errorf("inspect opened backup source %q: %w", filepath.Clean(source), err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(sourceInfo, openedInfo) {
		_ = sourceFile.Close()
		return "", fmt.Errorf("%w: backup source %q changed before copy", ErrEvidenceMismatch, filepath.Clean(source))
	}

	destinationFile, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = sourceFile.Close()
		return "", fmt.Errorf("create backup file %q without overwrite: %w", destination, err)
	}

	digest := sha256.New()
	if _, err := io.Copy(io.MultiWriter(destinationFile, digest), sourceFile); err != nil {
		_ = sourceFile.Close()
		_ = destinationFile.Close()
		return "", cleanupFailedBackup(destination, fmt.Errorf("copy backup file %q: %w", destination, err))
	}
	if err := sourceFile.Close(); err != nil {
		_ = destinationFile.Close()
		return "", cleanupFailedBackup(destination, fmt.Errorf("close backup source %q: %w", filepath.Clean(source), err))
	}
	if err := validateSourceAfterCopy(source, sourceInfo, expectedMode); err != nil {
		_ = destinationFile.Close()
		return "", cleanupFailedBackup(destination, err)
	}
	actualHash := sha256Prefix + hex.EncodeToString(digest.Sum(nil))
	if actualHash != expectedHash {
		_ = destinationFile.Close()
		return "", cleanupFailedBackup(destination, fmt.Errorf(
			"%w: backup source %q digest changed: got %s, want %s",
			ErrEvidenceMismatch,
			filepath.Clean(source), actualHash, expectedHash,
		))
	}
	if err := destinationFile.Chmod(expectedMode); err != nil {
		_ = destinationFile.Close()
		return "", cleanupFailedBackup(destination, fmt.Errorf("set backup file permissions %q: %w", destination, err))
	}
	if err := destinationFile.Sync(); err != nil {
		_ = destinationFile.Close()
		return "", cleanupFailedBackup(destination, fmt.Errorf("sync backup file %q: %w", destination, err))
	}
	if err := destinationFile.Close(); err != nil {
		return "", cleanupFailedBackup(destination, fmt.Errorf("close backup file %q: %w", destination, err))
	}
	if err := batch.syncParents(filepath.Dir(destination)); err != nil {
		return "", cleanupFailedBackup(destination, err)
	}
	return destination, nil
}

// SaveSymlink 保存 source 的原始 link text，不解析或跟随链接目标。
func (batch *Batch) SaveSymlink(source string, relative string, expectedLinkText string) (string, error) {
	destination, err := batch.prepareDestination(relative)
	if err != nil {
		return "", err
	}
	sourceInfo, err := inspectAbsoluteSource(source)
	if err != nil {
		return "", err
	}
	if sourceInfo.Mode()&fs.ModeSymlink == 0 {
		return "", fmt.Errorf("%w: backup source %q is not a symlink", ErrEvidenceMismatch, filepath.Clean(source))
	}
	linkText, err := os.Readlink(filepath.Clean(source))
	if err != nil {
		return "", fmt.Errorf("read backup source symlink %q: %w", filepath.Clean(source), err)
	}
	if linkText != expectedLinkText {
		return "", fmt.Errorf("%w: backup source %q link text changed", ErrEvidenceMismatch, filepath.Clean(source))
	}
	currentInfo, err := os.Lstat(filepath.Clean(source))
	if err != nil {
		return "", fmt.Errorf("inspect backup source %q before copy: %w", filepath.Clean(source), err)
	}
	if !os.SameFile(sourceInfo, currentInfo) {
		return "", fmt.Errorf("%w: backup source %q changed before copy", ErrEvidenceMismatch, filepath.Clean(source))
	}
	currentText, err := os.Readlink(filepath.Clean(source))
	if err != nil {
		return "", fmt.Errorf("read backup source symlink %q before copy: %w", filepath.Clean(source), err)
	}
	if currentText != expectedLinkText {
		return "", fmt.Errorf("%w: backup source %q link text changed before copy", ErrEvidenceMismatch, filepath.Clean(source))
	}
	if err := os.Symlink(expectedLinkText, destination); err != nil {
		return "", fmt.Errorf("create backup symlink %q without overwrite: %w", destination, err)
	}
	if err := batch.syncParents(filepath.Dir(destination)); err != nil {
		return "", cleanupFailedBackup(destination, err)
	}
	return destination, nil
}

func (batch *Batch) prepareDestination(relative string) (string, error) {
	if batch == nil || batch.path == "" || !filepath.IsAbs(batch.path) {
		return "", fmt.Errorf("backup batch is not initialized")
	}
	if relative == "" || relative == "." || filepath.IsAbs(relative) || filepath.Clean(relative) != relative {
		return "", fmt.Errorf("backup relative path %q must be non-empty, clean, and relative", relative)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("backup relative path %q escapes its batch", relative)
	}
	destination := filepath.Join(batch.path, relative)
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, storage.PrivateDirectoryMode); err != nil {
		return "", fmt.Errorf("create backup parent %q: %w", parent, err)
	}
	for path := parent; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			return "", fmt.Errorf("inspect backup parent %q: %w", path, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("backup parent %q is not a directory", path)
		}
		if err := os.Chmod(path, storage.PrivateDirectoryMode); err != nil {
			return "", fmt.Errorf("set backup parent permissions %q: %w", path, err)
		}
		if path == batch.path {
			break
		}
	}
	return destination, nil
}

func validateRegularEvidence(expectedHash string, expectedMode fs.FileMode) error {
	digestText, ok := strings.CutPrefix(expectedHash, sha256Prefix)
	decoded, err := hex.DecodeString(digestText)
	if !ok || err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != digestText {
		return fmt.Errorf("planned backup digest %q must use canonical sha256 format", expectedHash)
	}
	if expectedMode&^fs.ModePerm != 0 {
		return fmt.Errorf("planned backup mode %v must contain only nine permission bits", expectedMode)
	}
	return nil
}

func inspectAbsoluteSource(source string) (fs.FileInfo, error) {
	if source == "" || !filepath.IsAbs(source) {
		return nil, fmt.Errorf("backup source must be a non-empty absolute path")
	}
	info, err := os.Lstat(filepath.Clean(source))
	if err != nil {
		return nil, fmt.Errorf("inspect backup source %q: %w", filepath.Clean(source), err)
	}
	return info, nil
}

func validateSourceAfterCopy(source string, before fs.FileInfo, expectedMode fs.FileMode) error {
	after, err := os.Lstat(filepath.Clean(source))
	if err != nil {
		return fmt.Errorf("inspect backup source %q after copy: %w", filepath.Clean(source), err)
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) || after.Mode().Perm() != expectedMode {
		return fmt.Errorf("%w: backup source %q changed during copy", ErrEvidenceMismatch, filepath.Clean(source))
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := openDirectoryForSync(path)
	if err != nil {
		return fmt.Errorf("open backup directory %q for sync: %w", path, err)
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return fmt.Errorf("sync backup directory %q: %w", path, err)
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("close backup directory %q after sync: %w", path, err)
	}
	return nil
}

func syncDirectoryChain(start string, stop string) error {
	for path := start; ; path = filepath.Dir(path) {
		if err := syncDirectory(path); err != nil {
			return err
		}
		if path == stop {
			return nil
		}
		if filepath.Dir(path) == path {
			return fmt.Errorf("backup sync stop %q is not an ancestor of %q", stop, start)
		}
	}
}

func (batch *Batch) syncParents(parent string) error {
	return syncDirectoryChain(parent, batch.path)
}

func cleanupFailedBackup(path string, cause error) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.Join(cause, fmt.Errorf("remove incomplete backup %q: %w", path, err))
	}
	return cause
}
