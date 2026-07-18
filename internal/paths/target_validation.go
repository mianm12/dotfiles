package paths

import (
	"errors"
	"fmt"
)

// ErrTargetOverlap 表示两个 labeled target 的文件系统身份相等或互为祖先。
var ErrTargetOverlap = errors.New("target paths overlap")

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
// 任一解析或 pair relation 失败都返回零值，调用方不会获得部分可信集合。
func ValidateTargetSet(inputs []LabeledTarget) (TargetSet, error) {
	resolver := newTargetResolver()
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
			return TargetSet{}, fmt.Errorf(
				"%w: %s path %q and %s path %q have relation %s",
				ErrTargetOverlap,
				left.input.Label,
				left.input.Path,
				right.input.Label,
				right.input.Path,
				relation,
			)
		}
	}

	return TargetSet{targets: resolved}, nil
}
