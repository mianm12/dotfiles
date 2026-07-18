package state

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mianm12/dotfiles/internal/paths"
)

var (
	// ErrPathValidation 表示合法 state 在当前文件系统拓扑下尚不能安全消费。
	// 它不等同于持久文档损坏；错误链会保留具体 paths cause。
	ErrPathValidation = errors.New("state target path validation failed")
	// ErrTargetIdentityConflict 表示两个不同 state key 当前解析到同一 target identity。
	ErrTargetIdentityConflict = errors.New("state target identities conflict")
)

type targetIdentityResolver func(string) (paths.TargetIdentity, error)

type resolvedStateTarget struct {
	key      string
	identity paths.TargetIdentity
}

// ValidateTargetIdentities 在一个稳定、只读的当前拓扑快照中校验 state key 身份唯一。
// 当前祖先阻断、identity capability 或 IO 错误返回 ErrPathValidation，不会改写为 ErrCorrupt。
func ValidateTargetIdentities(snapshot Snapshot, home string) error {
	return validateTargetIdentities(snapshot, home, paths.ResolveTargetIdentity)
}

func validateTargetIdentities(
	snapshot Snapshot,
	home string,
	resolve targetIdentityResolver,
) error {
	if !snapshot.valid {
		return fmt.Errorf("%w: invalid zero-value snapshot", ErrPathValidation)
	}
	if home == "" || !filepath.IsAbs(home) {
		return fmt.Errorf("%w: effective HOME %q must be a non-empty absolute path", ErrPathValidation, home)
	}
	cleanHome := filepath.Clean(home)
	resolved := make([]resolvedStateTarget, 0, len(snapshot.entries))
	for _, key := range snapshot.EntryKeys() {
		relative := strings.TrimPrefix(key, "~/")
		path := filepath.Join(cleanHome, filepath.FromSlash(relative))
		identity, err := resolve(path)
		if err != nil {
			return fmt.Errorf("%w: resolve state target %q: %w", ErrPathValidation, key, err)
		}
		for _, previous := range resolved {
			if identity.Equal(previous.identity) {
				return fmt.Errorf(
					"%w: %w: state targets %q and %q resolve to the same identity",
					ErrCorrupt,
					ErrTargetIdentityConflict,
					previous.key,
					key,
				)
			}
		}
		resolved = append(resolved, resolvedStateTarget{key: key, identity: identity})
	}
	return nil
}
