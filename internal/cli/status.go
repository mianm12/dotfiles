package cli

import (
	"slices"

	"github.com/mianm12/dotfiles/internal/core/config"
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
	analysis, err := analyzeStatus(context, machine, moduleID)
	if err != nil {
		return err
	}
	return printStatusAnalysis(command, analysis)
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
