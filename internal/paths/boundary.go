package paths

import (
	"errors"
	"fmt"
)

// ErrTargetControlOverlap 表示 labeled target 与控制面成员相等或互为祖先。
var ErrTargetControlOverlap = errors.New("target overlaps control plane")

// PathBoundaries 是控制面、完整 target set 及其 cross-product 全部通过后的只读结果。
type PathBoundaries struct {
	control ControlPlane
	targets TargetSet
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
