package planner

import (
	"fmt"
	"slices"
	"strings"
)

// PruneOptions 封闭 M1 prune 的显式开关与 scope。Full 和 Modules 互斥。
type PruneOptions struct {
	Enabled bool
	Full    bool
	Modules []string
}

// PruneConfirmationTarget 保存 whole-module 确认所需的稳定摘要。
type PruneConfirmationTarget struct {
	Target            string
	WouldDeleteTarget bool
}

// PruneConfirmationGroup 表示 complete desired 中已整体消失的 module。
type PruneConfirmationGroup struct {
	Module  string
	Targets []PruneConfirmationTarget
}

// PrunePlan 是 scope 内 orphan 的纯值计划，不提供执行或文件系统能力。
type PrunePlan struct {
	actions []PruneAction
	groups  []PruneConfirmationGroup
}

// Actions 返回不共享 observation bytes 的动作副本。
func (plan PrunePlan) Actions() []PruneAction {
	actions := append([]PruneAction(nil), plan.actions...)
	for index := range actions {
		actions[index] = actions[index].Clone()
	}
	return actions
}

// ConfirmationGroups 返回不共享 target slices 的确认组副本。
func (plan PrunePlan) ConfirmationGroups() []PruneConfirmationGroup {
	groups := append([]PruneConfirmationGroup(nil), plan.groups...)
	for index := range groups {
		groups[index].Targets = append([]PruneConfirmationTarget(nil), groups[index].Targets...)
	}
	return groups
}

// PlanPrune 按完整 observation、file decision 与显式 scope 形成 P1/P2/P3 计划。只要 file
// decision 含 conflict，全部候选都会显式 deferred，避免在 unresolved 决策旁提交 prune。
func PlanPrune(profile ObservedProfile, fileActions []Action, options PruneOptions) (PrunePlan, error) {
	if !options.Enabled {
		return PrunePlan{}, nil
	}

	modules, err := pruneScope(options)
	if err != nil {
		return PrunePlan{}, err
	}
	orphans := profile.Orphans()
	selected := orphans[:0]
	for _, orphan := range orphans {
		if options.Full || modules[orphan.State.Module] {
			selected = append(selected, orphan)
		}
	}
	slices.SortFunc(selected, func(left, right OrphanTarget) int {
		return strings.Compare(left.State.Key, right.State.Key)
	})

	deferred := slices.ContainsFunc(fileActions, func(action Action) bool {
		return action.Verb == ActionConflict
	})
	actions := make([]PruneAction, 0, len(selected))
	for _, orphan := range selected {
		action, planErr := planOrphanPrune(orphan, deferred)
		if planErr != nil {
			return PrunePlan{}, planErr
		}
		actions = append(actions, action)
	}

	desiredModules := make(map[string]bool)
	for _, target := range profile.Targets() {
		desiredModules[target.Desired.Module] = true
	}
	grouped := make(map[string][]PruneConfirmationTarget)
	for _, action := range actions {
		if desiredModules[action.Module] {
			continue
		}
		grouped[action.Module] = append(grouped[action.Module], PruneConfirmationTarget{
			Target:            action.Target,
			WouldDeleteTarget: action.WouldDeleteTarget(),
		})
	}
	groupModules := make([]string, 0, len(grouped))
	for module := range grouped {
		groupModules = append(groupModules, module)
	}
	slices.Sort(groupModules)
	groups := make([]PruneConfirmationGroup, 0, len(groupModules))
	for _, module := range groupModules {
		targets := grouped[module]
		slices.SortFunc(targets, func(left, right PruneConfirmationTarget) int {
			return strings.Compare(left.Target, right.Target)
		})
		groups = append(groups, PruneConfirmationGroup{Module: module, Targets: targets})
	}

	return PrunePlan{actions: actions, groups: groups}, nil
}

func pruneScope(options PruneOptions) (map[string]bool, error) {
	if options.Full {
		if len(options.Modules) != 0 {
			return nil, fmt.Errorf("%w: full prune cannot include module scope", ErrUnsupportedPruneInput)
		}
		return nil, nil
	}
	if len(options.Modules) == 0 {
		return nil, fmt.Errorf("%w: partial prune requires module scope", ErrUnsupportedPruneInput)
	}
	modules := make(map[string]bool, len(options.Modules))
	for _, module := range options.Modules {
		if module == "" {
			return nil, fmt.Errorf("%w: prune module cannot be empty", ErrUnsupportedPruneInput)
		}
		modules[module] = true
	}
	return modules, nil
}
