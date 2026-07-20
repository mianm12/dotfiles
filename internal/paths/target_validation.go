package paths

import (
	"errors"
	"fmt"
	"strings"
)

// ErrTargetOverlap 表示一个 labeled target 穿过自身 leaf，或两个 target 的文件系统身份相等/
// 互为祖先。
var ErrTargetOverlap = errors.New("target paths overlap")

// TargetRelation 描述冲突中 left/right target 的文件系统 identity 关系；
// symlink traversal 可以让两个严格祖先方向同时成立。
// left/right 始终对应 TargetConflictError 返回的原始输入顺序。
type TargetRelation uint8

// TargetRelationNone 表示零值或无冲突关系；有效 TargetConflictError 不返回它。
const TargetRelationNone TargetRelation = 0

const (
	// TargetRelationEqual 表示双方解析到同一 target identity。
	TargetRelationEqual TargetRelation = 1 << iota
	// TargetRelationLeftAncestor 表示 left 是 right 的严格祖先。
	TargetRelationLeftAncestor
	// TargetRelationRightAncestor 表示 right 是 left 的严格祖先。
	TargetRelationRightAncestor
)

// String 返回稳定的诊断名称。
func (relation TargetRelation) String() string {
	parts := make([]string, 0, 3)
	if relation&TargetRelationEqual != 0 {
		parts = append(parts, "equal")
	}
	if relation&TargetRelationLeftAncestor != 0 {
		parts = append(parts, "left-ancestor")
	}
	if relation&TargetRelationRightAncestor != 0 {
		parts = append(parts, "right-ancestor")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}

// Has 报告 relation 是否包含指定的非空关系事实。
func (relation TargetRelation) Has(candidate TargetRelation) bool {
	return candidate != TargetRelationNone && relation&candidate == candidate
}

// TargetConflictError 保存 target-set 冲突的双方 provenance 和 identity relation。
// 访问器返回不可变值副本；errors.Is(error, ErrTargetOverlap) 保持成立。
type TargetConflictError struct {
	left     LabeledTarget
	right    LabeledTarget
	relation TargetRelation
}

// Error 返回同时包含双方展示路径和 relation 的诊断。
func (conflict *TargetConflictError) Error() string {
	return fmt.Sprintf(
		"%v: %s path %q and %s path %q have relation %s",
		ErrTargetOverlap,
		conflict.left.Label,
		conflict.left.Path,
		conflict.right.Label,
		conflict.right.Path,
		conflict.relation,
	)
}

// Unwrap 保留 ErrTargetOverlap sentinel。
func (conflict *TargetConflictError) Unwrap() error { return ErrTargetOverlap }

// Left 返回冲突中先出现的 target。
func (conflict *TargetConflictError) Left() LabeledTarget { return conflict.left }

// Right 返回冲突中后出现的 target。
func (conflict *TargetConflictError) Right() LabeledTarget { return conflict.right }

// Relation 返回双方在同一次 identity snapshot 中的关系。
func (conflict *TargetConflictError) Relation() TargetRelation { return conflict.relation }

// TargetSelfTraversalError 保存展示路径在到达 leaf 前穿过自身 leaf identity 的 target provenance。
// 访问器返回不可变值副本；errors.Is(error, ErrTargetOverlap) 保持成立。
type TargetSelfTraversalError struct {
	target LabeledTarget
}

// Error 返回包含 target label、展示路径和 unary topology 原因的诊断。
func (traversal *TargetSelfTraversalError) Error() string {
	return fmt.Sprintf(
		"%v: %s path %q traverses its own leaf identity",
		ErrTargetOverlap,
		traversal.target.Label,
		traversal.target.Path,
	)
}

// Unwrap 保留 ErrTargetOverlap sentinel。
func (traversal *TargetSelfTraversalError) Unwrap() error { return ErrTargetOverlap }

// Target 返回发生 self traversal 的 target。
func (traversal *TargetSelfTraversalError) Target() LabeledTarget { return traversal.target }

// LabeledTarget 是共享 target-set validator 的最小输入。
// Label 由调用方提供诊断 provenance；Path 必须是非 root 绝对 target 展示路径。
type LabeledTarget struct {
	Label string
	Path  string
}

type resolvedLabeledTarget struct {
	input      LabeledTarget
	resolution TargetResolution
}

// TargetSet 是全部成员 identity/topology 校验通过后的只读 target 集合。
type TargetSet struct {
	targets []resolvedLabeledTarget
}

// ValidateTargetSet 在同一个只读 identity snapshot 内解析并校验全部 target。
// 任一解析、self traversal 或 pair relation 失败都返回零值，调用方不会获得部分可信集合。
func ValidateTargetSet(inputs []LabeledTarget) (TargetSet, error) {
	return validateTargetSet(inputs, newTargetResolver())
}

func validateTargetSet(inputs []LabeledTarget, resolver *targetResolver) (TargetSet, error) {
	resolved := make([]resolvedLabeledTarget, len(inputs))
	for index, input := range inputs {
		if input.Label == "" {
			return TargetSet{}, fmt.Errorf("target %d has an empty provenance label", index)
		}
		resolution, err := resolver.resolve(input.Path)
		if err != nil {
			return TargetSet{}, fmt.Errorf("resolve target %s path %q: %w", input.Label, input.Path, err)
		}
		resolved[index] = resolvedLabeledTarget{input: input, resolution: resolution}
	}

	for leftIndex := range resolved {
		for rightIndex := leftIndex + 1; rightIndex < len(resolved); rightIndex++ {
			left := resolved[leftIndex]
			right := resolved[rightIndex]
			relation := targetResolutionRelation(left.resolution, right.resolution)
			if relation == pathRelationNone {
				continue
			}
			return TargetSet{}, &TargetConflictError{
				left:     left.input,
				right:    right.input,
				relation: exportedTargetRelation(relation),
			}
		}
	}
	// pair relation 携带双方 provenance 与更具体的冲突事实；仅在集合没有 pair conflict 时报告
	// unary self traversal，避免遮蔽 equal/ancestor 关系。
	for _, target := range resolved {
		if target.resolution.Traverses(target.resolution) {
			return TargetSet{}, &TargetSelfTraversalError{target: target.input}
		}
	}

	return TargetSet{targets: resolved}, nil
}

func exportedTargetRelation(relation pathRelation) TargetRelation {
	var result TargetRelation
	if relation&pathRelationEqual != 0 {
		result |= TargetRelationEqual
	}
	if relation&pathRelationLeftAncestor != 0 {
		result |= TargetRelationLeftAncestor
	}
	if relation&pathRelationRightAncestor != 0 {
		result |= TargetRelationRightAncestor
	}
	return result
}
