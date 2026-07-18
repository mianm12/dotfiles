//go:build linux && (amd64 || arm64)

package paths

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	// ext2、ext3 与 ext4 共用这个 statfs magic；只有 ext4 可启用 per-directory casefold。
	extFilesystemMagic int64 = 0xef53
	// Btrfs 的目录项查找按名称长度和原始 bytes 比较。
	btrfsFilesystemMagic int64 = 0x9123683e
	fsCasefoldFlag             = uintptr(0x40000000)

	linuxIOCRead      = uintptr(2)
	linuxIOCDirShift  = uintptr(30)
	linuxIOCSizeShift = uintptr(16)
	linuxIOCTypeShift = uintptr(8)
	// Linux amd64/arm64 的 FS_IOC_GETFLAGS 定义为 _IOR('f', 1, long)。
	fsIOCGetFlags = (linuxIOCRead << linuxIOCDirShift) |
		(uintptr(unsafe.Sizeof(uintptr(0))) << linuxIOCSizeShift) |
		(uintptr('f') << linuxIOCTypeShift) |
		uintptr(1)
)

type linuxNameSemanticsQuery func(*os.File) (int64, uintptr, error)

func missingNameKey(parent, name string) (string, error) {
	return missingNameKeyWithQuery(parent, name, queryLinuxNameSemantics)
}

func missingNameKeyWithQuery(
	parent, name string,
	query linuxNameSemanticsQuery,
) (string, error) {
	directory, err := os.Open(parent)
	if err != nil {
		return "", fmt.Errorf("open parent directory %q: %w", parent, err)
	}
	defer func() {
		_ = directory.Close()
	}()

	filesystemType, flags, err := query(directory)
	if err != nil {
		return "", fmt.Errorf("%w: query missing-name semantics for %q: %w", ErrIdentityUnavailable, parent, err)
	}
	return classifyLinuxMissingName(filesystemType, flags, name)
}

func queryLinuxNameSemantics(directory *os.File) (int64, uintptr, error) {
	var filesystem syscall.Statfs_t
	if err := syscall.Fstatfs(int(directory.Fd()), &filesystem); err != nil {
		return 0, 0, fmt.Errorf("inspect parent filesystem: %w", err)
	}

	var flags uintptr
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		directory.Fd(),
		fsIOCGetFlags,
		uintptr(unsafe.Pointer(&flags)),
	)
	if errno != 0 {
		return 0, 0, fmt.Errorf("read parent inode flags: %w", errno)
	}
	return filesystem.Type, flags, nil
}

func classifyLinuxMissingName(filesystemType int64, flags uintptr, name string) (string, error) {
	if filesystemType != extFilesystemMagic && filesystemType != btrfsFilesystemMagic {
		return "", fmt.Errorf(
			"%w: filesystem type %#x does not expose supported missing-name rules",
			ErrIdentityUnavailable,
			filesystemType,
		)
	}
	if flags&fsCasefoldFlag != 0 {
		return "", fmt.Errorf(
			"%w: parent directory uses case-insensitive name lookup",
			ErrIdentityUnavailable,
		)
	}
	return name, nil
}
