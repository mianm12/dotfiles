package cli

import (
	"fmt"
	"slices"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/core/planner"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/spf13/cobra"
)

func newStatusCommand(env environment) *cobra.Command {
	return &cobra.Command{
		Use:   "status [MODULE]",
		Short: "Inspect module convergence without mutation",
		Args:  maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			var moduleID *string
			if len(args) == 1 {
				moduleID = &args[0]
			}
			return runStatus(command, moduleID, env)
		},
	}
}

func runStatus(command *cobra.Command, moduleID *string, env environment) error {
	context, err := resolveContext(env)
	if err != nil {
		return err
	}
	machine, err := loadRequiredMachine(context)
	if err != nil {
		return err
	}
	repository, err := config.OpenRepository(machine.Repository)
	if err != nil {
		return err
	}
	profileModules, err := repository.ProfileModules(machine.Profiles)
	if err != nil {
		return err
	}
	loaded, err := state.Load(context.statePath, context.home)
	if err != nil {
		return err
	}
	if moduleID != nil {
		requested := *moduleID
		_, stateKnown := loaded.Snapshot.Modules[requested]
		_, exists, _, inspectErr := repository.InspectModule(requested, context.platform)
		if inspectErr != nil {
			return inspectErr
		}
		selectionKnown := slices.Contains(profileModules, requested) ||
			slices.Contains(machine.ExtraModules, requested)
		if !exists && !stateKnown && !selectionKnown {
			return fmt.Errorf("unknown module %q", requested)
		}
	}

	resolution, err := repository.Resolve(machine.Scope(), context.platform)
	if err != nil {
		return err
	}
	var scope []string
	if moduleID != nil {
		scope = []string{*moduleID}
	}
	plan, err := planner.Build(planner.Request{
		Home:     context.home,
		Controls: context.controls(machine.Repository),
		Modules:  resolution.Modules,
		Scope:    scope,
		State:    loaded.Snapshot,
	})
	if err != nil {
		return err
	}

	effective := make(map[string]bool)
	for _, id := range profileModules {
		effective[id] = true
	}
	for _, id := range machine.ExtraModules {
		effective[id] = true
	}
	notApplicable := make(map[string]bool)
	for _, id := range resolution.NotApplicable {
		notApplicable[id] = true
	}
	variants := make(map[string]string)
	for _, module := range resolution.Modules {
		variants[module.ID] = module.Variant
	}

	ids := statusModuleIDs(moduleID, repository, machine, loaded.Snapshot)
	statuses := make([]moduleStatus, 0, len(ids))
	for _, id := range ids {
		statuses = append(statuses, statusForModule(
			id,
			effective,
			notApplicable,
			variants,
			loaded.Snapshot,
			plan,
		))
	}
	return printStatus(command, statuses, appendWarning(
		loaded.Warning,
		plan.Warnings,
	))
}

func statusModuleIDs(
	moduleID *string,
	repository config.Repository,
	machine config.Machine,
	snapshot state.Snapshot,
) []string {
	if moduleID != nil {
		return []string{*moduleID}
	}
	set := make(map[string]bool)
	for _, id := range repository.ModuleIDs() {
		set[id] = true
	}
	for _, id := range machine.ExtraModules {
		set[id] = true
	}
	for id := range snapshot.Modules {
		set[id] = true
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
