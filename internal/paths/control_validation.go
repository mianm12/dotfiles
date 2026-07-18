package paths

import (
	"errors"
	"fmt"
)

// ErrControlPlaneOverlap 表示两个控制面成员的文件系统身份相等或互为祖先。
var ErrControlPlaneOverlap = errors.New("control-plane paths overlap")

type resolvedControlMember struct {
	definition controlPathMember
	resolution ControlPathResolution
}

// ControlPlane 是已完成成员 identity 解析和家族隔离校验的只读控制面。
type ControlPlane struct {
	paths   ControlPlanePaths
	members [controlMemberCount]resolvedControlMember
}

// ValidateControlPlane 解析全部固定控制面成员并校验不同家族两两隔离。
// state root 到预定 child 的正向包含是唯一例外；相等、反向包含和 sibling overlap 仍拒绝。
func ValidateControlPlane(paths ControlPlanePaths) (ControlPlane, error) {
	resolver := newTargetResolver()
	var resolved [controlMemberCount]resolvedControlMember
	for index, member := range paths.members {
		if member.role != controlMemberRole(index) {
			return ControlPlane{}, fmt.Errorf("invalid control-plane member table at index %d", index)
		}
		resolution, err := resolver.resolveControlPathIdentity(member.path)
		if err != nil {
			return ControlPlane{}, fmt.Errorf(
				"resolve %s control path %q: %w",
				member.role,
				member.path,
				err,
			)
		}
		resolved[index] = resolvedControlMember{definition: member, resolution: resolution}
	}

	for leftIndex := range resolved {
		for rightIndex := leftIndex + 1; rightIndex < len(resolved); rightIndex++ {
			left := resolved[leftIndex]
			right := resolved[rightIndex]
			relation := controlPathRelation(left.resolution, right.resolution)
			leftIsParent, planned := plannedControlContainment(left.definition, right.definition)
			if planned {
				if plannedContainmentValid(relation, leftIsParent) {
					continue
				}
				return ControlPlane{}, controlPlaneConflict(left.definition, right.definition, relation)
			}
			if relation != pathRelationNone {
				return ControlPlane{}, controlPlaneConflict(left.definition, right.definition, relation)
			}
		}
	}

	return ControlPlane{paths: paths, members: resolved}, nil
}

// Paths 返回已校验控制面的原始绝对展示路径值。
func (plane ControlPlane) Paths() ControlPlanePaths {
	return plane.paths
}

func plannedControlContainment(left, right controlPathMember) (bool, bool) {
	if left.family != controlFamilyState || right.family != controlFamilyState {
		return false, false
	}
	if right.hasParent && right.parent == left.role {
		return true, true
	}
	if left.hasParent && left.parent == right.role {
		return false, true
	}
	return false, false
}

func plannedContainmentValid(relation pathRelation, leftIsParent bool) bool {
	if leftIsParent {
		return relation&pathRelationLeftAncestor != 0 &&
			relation&(pathRelationEqual|pathRelationRightAncestor) == 0
	}
	return relation&pathRelationRightAncestor != 0 &&
		relation&(pathRelationEqual|pathRelationLeftAncestor) == 0
}

func controlPlaneConflict(left, right controlPathMember, relation pathRelation) error {
	return fmt.Errorf(
		"%w: %s/%s %q and %s/%s %q have relation %s",
		ErrControlPlaneOverlap,
		left.family,
		left.role,
		left.path,
		right.family,
		right.role,
		right.path,
		relation,
	)
}

func (family controlFamily) String() string {
	switch family {
	case controlFamilyRepository:
		return "repository"
	case controlFamilyConfig:
		return "config"
	case controlFamilyState:
		return "state"
	case controlFamilyBinary:
		return "binary"
	default:
		return fmt.Sprintf("control family %d", family)
	}
}

func (role controlMemberRole) String() string {
	switch role {
	case controlMemberRepository:
		return "repository"
	case controlMemberConfig:
		return "machine config"
	case controlMemberStateRoot:
		return "state root"
	case controlMemberStateFile:
		return "state file"
	case controlMemberStateLock:
		return "state lock"
	case controlMemberBackupRoot:
		return "backup root"
	case controlMemberInstalledBinary:
		return "installed binary"
	default:
		return fmt.Sprintf("control member %d", role)
	}
}
