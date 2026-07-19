package paths

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrTargetControlOverlap 表示 labeled target 与控制面成员相等或互为祖先。
var ErrTargetControlOverlap = errors.New("target overlaps control plane")

// PathBoundaries 是控制面、完整 target set 及其 cross-product 全部通过后的只读结果。
type PathBoundaries struct {
	control ControlPlane
	targets TargetSet
}

// ValidateLexicalTargetControlBoundaries 只按已经清理的绝对展示路径校验 target 与控制面。
// 它不读取文件系统；调用方可先用它区分永久词法错误，再用 ValidatePathBoundaries 校验当前别名。
func ValidateLexicalTargetControlBoundaries(
	controlPaths ControlPlanePaths,
	targets []LabeledTarget,
) error {
	for index, target := range targets {
		if target.Label == "" {
			return fmt.Errorf("target %d has an empty provenance label", index)
		}
		cleanTarget, err := cleanTargetPath(target.Path)
		if err != nil {
			return fmt.Errorf("target %s: %w", target.Label, err)
		}
		for _, member := range controlPaths.members {
			cleanControl, err := cleanAbsolutePath(member.path)
			if err != nil {
				return fmt.Errorf("control %s/%s: %w", member.family, member.role, err)
			}
			relation, err := lexicalPathRelation(cleanControl, cleanTarget)
			if err != nil {
				return fmt.Errorf("compare control %q and target %q: %w", cleanControl, cleanTarget, err)
			}
			if relation == pathRelationNone {
				continue
			}
			return fmt.Errorf(
				"%w: %s/%s path %q and %s path %q have lexical relation %s",
				ErrTargetControlOverlap,
				member.family,
				member.role,
				cleanControl,
				target.Label,
				cleanTarget,
				relation,
			)
		}
	}
	return nil
}

func lexicalPathRelation(left, right string) (pathRelation, error) {
	var relation pathRelation
	leftToRight, err := filepath.Rel(left, right)
	if err != nil {
		return pathRelationNone, err
	}
	switch {
	case leftToRight == ".":
		relation |= pathRelationEqual
	case lexicalDescendant(leftToRight):
		relation |= pathRelationLeftAncestor
	}
	rightToLeft, err := filepath.Rel(right, left)
	if err != nil {
		return pathRelationNone, err
	}
	if lexicalDescendant(rightToLeft) {
		relation |= pathRelationRightAncestor
	}
	return relation, nil
}

func lexicalDescendant(relative string) bool {
	return relative != "." && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}

// ValidatePathBoundaries 在同一个只读 identity snapshot 内依次校验控制面、完整 target set，
// 以及每个 target 与每个控制面成员。任一步失败都返回零值结果。
func ValidatePathBoundaries(controlPaths ControlPlanePaths, targets []LabeledTarget) (PathBoundaries, error) {
	resolver := newTargetResolver()
	control, err := validateControlPlane(controlPaths, resolver)
	if err != nil {
		return PathBoundaries{}, err
	}
	validatedTargets, err := validateTargetSet(targets, resolver)
	if err != nil {
		return PathBoundaries{}, err
	}

	for _, target := range validatedTargets.targets {
		for _, member := range control.members {
			relation := controlTargetRelation(member.resolution, target.resolution)
			if relation == pathRelationNone {
				continue
			}
			return PathBoundaries{}, fmt.Errorf(
				"%w: %s/%s path %q and %s path %q have relation %s",
				ErrTargetControlOverlap,
				member.definition.family,
				member.definition.role,
				member.definition.path,
				target.input.Label,
				target.input.Path,
				relation,
			)
		}
	}

	return PathBoundaries{control: control, targets: validatedTargets}, nil
}
