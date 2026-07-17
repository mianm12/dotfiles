package manifest

import (
	"fmt"
	"slices"
	"strings"
)

// profileExpander 使用带记忆的 DFS 展开全部 profile：零值状态表示未访问，visiting
// 用于检测环，complete 复用已经稳定排序的结果；stack 只保存当前引用链。
type profileExpander struct {
	declared    map[string][]string
	modules     map[string]loadedModule
	moduleNames []string
	states      map[string]profileState
	expanded    map[string][]string
	stack       []string
}

type profileState uint8

const (
	profileVisiting profileState = iota + 1
	profileComplete
)

func expandProfiles(
	declared map[string][]string,
	modules map[string]loadedModule,
	moduleNames []string,
) (map[string][]string, []string, []string, error) {
	profileNames := make([]string, 0, len(declared))
	for name := range declared {
		profileNames = append(profileNames, name)
	}
	slices.Sort(profileNames)
	for _, name := range profileNames {
		if !manifestNamePattern.MatchString(name) {
			return nil, nil, nil, fmt.Errorf("invalid profile name %q", name)
		}
	}

	expander := profileExpander{
		declared:    declared,
		modules:     modules,
		moduleNames: moduleNames,
		states:      make(map[string]profileState, len(declared)),
		expanded:    make(map[string][]string, len(declared)),
	}
	// unassigned 是仓库级概念，因此必须汇总所有已声明 profile，而不是只看调用方将选择的一个。
	assigned := make(map[string]struct{}, len(modules))
	for _, name := range profileNames {
		members, err := expander.expand(name)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, module := range members {
			assigned[module] = struct{}{}
		}
	}

	unassigned := make([]string, 0, len(moduleNames))
	for _, name := range moduleNames {
		if _, exists := assigned[name]; !exists {
			unassigned = append(unassigned, name)
		}
	}
	return expander.expanded, profileNames, unassigned, nil
}

func (e *profileExpander) expand(name string) ([]string, error) {
	switch e.states[name] {
	case profileComplete:
		return e.expanded[name], nil
	case profileVisiting:
		return nil, e.cycleError(name)
	}

	members, exists := e.declared[name]
	if !exists {
		return nil, fmt.Errorf("profile %q references unknown profile %q", e.currentProfile(), name)
	}
	e.states[name] = profileVisiting
	e.stack = append(e.stack, name)
	defer func() { e.stack = e.stack[:len(e.stack)-1] }()

	seen := make(map[string]struct{})
	result := make([]string, 0, len(members))
	add := func(module string) {
		if _, exists := seen[module]; exists {
			return
		}
		seen[module] = struct{}{}
		result = append(result, module)
	}

	for _, member := range members {
		if strings.HasPrefix(member, "@") {
			reference := strings.TrimPrefix(member, "@")
			if !manifestNamePattern.MatchString(reference) {
				return nil, fmt.Errorf("profile %q contains invalid profile reference %q", name, member)
			}
			expanded, err := e.expand(reference)
			if err != nil {
				return nil, err
			}
			for _, module := range expanded {
				add(module)
			}
			continue
		}
		if !manifestNamePattern.MatchString(member) {
			return nil, fmt.Errorf("profile %q contains invalid module name %q", name, member)
		}
		if _, exists := e.modules[member]; !exists {
			return nil, e.missingModuleError(name, member)
		}
		add(member)
	}

	// profile 语义是模块集合；排序得到与声明展开路径无关的规范结果。
	slices.Sort(result)
	e.states[name] = profileComplete
	e.expanded[name] = result
	return result, nil
}

func (e *profileExpander) currentProfile() string {
	if len(e.stack) == 0 {
		return ""
	}
	return e.stack[len(e.stack)-1]
}

func (e *profileExpander) cycleError(name string) error {
	start := slices.Index(e.stack, name)
	cycle := append([]string(nil), e.stack[start:]...)
	cycle = append(cycle, name)
	return fmt.Errorf("profile reference cycle: %s", strings.Join(cycle, " -> "))
}

func (e *profileExpander) missingModuleError(profile, requested string) error {
	for _, discovered := range e.moduleNames {
		if strings.EqualFold(discovered, requested) {
			return fmt.Errorf(
				"profile %q references module %q, which does not exactly match discovered module %q",
				profile,
				requested,
				discovered,
			)
		}
	}
	return fmt.Errorf("profile %q references missing module %q", profile, requested)
}
