package cli

import (
	"slices"

	"github.com/mianm12/dotfiles/internal/core/config"
	"github.com/mianm12/dotfiles/internal/core/state"
	"github.com/spf13/cobra"
)

func newStatusCommand(env environment) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Inspect module convergence without mutation",
		Args:  noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runStatus(command, env)
		},
	}
}

func runStatus(command *cobra.Command, env environment) error {
	context, err := resolveContext(env)
	if err != nil {
		return err
	}
	machine, err := loadRequiredMachine(context)
	if err != nil {
		return err
	}
	analysis, err := analyzeStatus(context, machine)
	if err != nil {
		return err
	}
	return printStatusAnalysis(command, analysis)
}

func statusModuleIDs(
	repository config.Repository,
	machine config.Machine,
	snapshot state.Snapshot,
) []string {
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
