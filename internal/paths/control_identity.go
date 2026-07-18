package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ControlPathResolution 保存 control path 展示 leaf 与实际消费位置的只读身份。
// leaf symlink 的展示位置和跟随后的最终位置都受保护；零值无效。
type ControlPathResolution struct {
	entry    TargetResolution
	consumed TargetResolution
}

// ResolveControlPathIdentity 解析绝对 control path 的展示位置与实际消费位置。
// 与 ResolveTarget 不同，它接受 filesystem root，并跟随 leaf symlink；解析保持只读。
func ResolveControlPathIdentity(path string) (ControlPathResolution, error) {
	cleanPath, err := cleanAbsolutePath(path)
	if err != nil {
		return ControlPathResolution{}, fmt.Errorf("control %w", err)
	}
	return newTargetResolver().resolveControlPathIdentity(cleanPath)
}

func (resolver *targetResolver) resolveControlPathIdentity(cleanPath string) (ControlPathResolution, error) {
	if filepath.Dir(cleanPath) == cleanPath {
		resolved, resolveErr := resolver.resolveExistingPath(cleanPath)
		if resolveErr != nil {
			return ControlPathResolution{}, fmt.Errorf("resolve control root %q: %w", cleanPath, resolveErr)
		}
		return ControlPathResolution{entry: resolved.resolution, consumed: resolved.resolution}, nil
	}

	entry, err := resolver.resolveCleanTarget(cleanPath)
	if err != nil {
		return ControlPathResolution{}, fmt.Errorf("resolve control entry %q: %w", cleanPath, err)
	}
	info, err := os.Lstat(cleanPath)
	if errors.Is(err, fs.ErrNotExist) && IsMissing(cleanPath, err) {
		return ControlPathResolution{entry: entry, consumed: entry}, nil
	}
	if err != nil {
		return ControlPathResolution{}, fmt.Errorf("inspect control entry %q: %w", cleanPath, err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return ControlPathResolution{entry: entry, consumed: entry}, nil
	}

	consumed, err := resolver.resolveExistingPath(cleanPath)
	if err != nil {
		return ControlPathResolution{}, fmt.Errorf("resolve control symlink %q: %w", cleanPath, err)
	}
	return ControlPathResolution{entry: entry, consumed: consumed.resolution}, nil
}

// OverlapsTarget 报告 control 的展示位置或实际消费位置是否与 target 相等或互为祖先。
func (resolution ControlPathResolution) OverlapsTarget(target TargetResolution) bool {
	return controlTargetRelation(resolution, target) != pathRelationNone
}

// Overlaps 报告两个 control 的展示位置或实际消费位置是否相等或互为祖先。
func (resolution ControlPathResolution) Overlaps(other ControlPathResolution) bool {
	return controlPathRelation(resolution, other) != pathRelationNone
}

type pathRelation uint8

const (
	pathRelationNone  pathRelation = 0
	pathRelationEqual pathRelation = 1 << iota
	pathRelationLeftAncestor
	pathRelationRightAncestor
)

func controlPathRelation(leftControl, rightControl ControlPathResolution) pathRelation {
	var relation pathRelation
	left := [...]TargetResolution{leftControl.entry, leftControl.consumed}
	right := [...]TargetResolution{rightControl.entry, rightControl.consumed}
	for _, leftPosition := range left {
		for _, rightPosition := range right {
			relation |= targetResolutionRelation(leftPosition, rightPosition)
		}
	}
	return relation
}

func controlTargetRelation(control ControlPathResolution, target TargetResolution) pathRelation {
	return targetResolutionRelation(control.entry, target) |
		targetResolutionRelation(control.consumed, target)
}

func targetResolutionRelation(left, right TargetResolution) pathRelation {
	var relation pathRelation
	if left.Equal(right) {
		relation |= pathRelationEqual
	}
	if left.IsAncestorOf(right) {
		relation |= pathRelationLeftAncestor
	}
	if right.IsAncestorOf(left) {
		relation |= pathRelationRightAncestor
	}
	return relation
}

func (relation pathRelation) String() string {
	parts := make([]string, 0, 3)
	if relation&pathRelationEqual != 0 {
		parts = append(parts, "equal")
	}
	if relation&pathRelationLeftAncestor != 0 {
		parts = append(parts, "left-ancestor")
	}
	if relation&pathRelationRightAncestor != 0 {
		parts = append(parts, "right-ancestor")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}
