package manifest

import (
	"fmt"
	"os"
)

// openManifest 只打开最终解析为普通文件的 manifest；指向普通文件的 symlink 有意允许。
func openManifest(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect manifest %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("manifest %q is not a regular file", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest %q: %w", path, err)
	}
	return file, nil
}
