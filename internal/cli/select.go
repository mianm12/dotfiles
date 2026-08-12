package cli

import (
	"fmt"

	"github.com/mianm12/dotfiles/internal/core/converge"
	"github.com/spf13/cobra"
)

func newSelectCommand(env environment) *cobra.Command {
	command := &cobra.Command{
		Use:   "select",
		Short: "Change direct module selection without converging targets",
		Args:  noArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return usagef("dot select requires a subcommand")
		},
	}
	command.AddCommand(
		newSelectAddCommand(env),
		newSelectRemoveCommand(env),
	)
	return command
}

func newSelectAddCommand(env environment) *cobra.Command {
	return &cobra.Command{
		Use:   "add MODULE",
		Short: "Add a direct module selection without converging targets",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runSelectAdd(command, args[0], env)
		},
	}
}

func newSelectRemoveCommand(env environment) *cobra.Command {
	return &cobra.Command{
		Use:   "remove MODULE",
		Short: "Remove a direct module selection without converging targets",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runSelectRemove(command, args[0], env)
		},
	}
}

func runSelectAdd(command *cobra.Command, moduleID string, env environment) error {
	context, err := resolveContext(env)
	if err != nil {
		return err
	}
	result, runErr := converge.SelectAdd(context.environment(), moduleID)
	if runErr != nil {
		return runErr
	}
	return printSelectionResult(
		command,
		fmt.Sprintf("module %s is selected directly; run dot apply", moduleID),
		result.Changed,
		"dot select add "+moduleID,
	)
}

func runSelectRemove(command *cobra.Command, moduleID string, env environment) error {
	control, err := resolveControlContext(env)
	if err != nil {
		return err
	}
	result, runErr := converge.SelectRemove(control.environment(), moduleID)
	if runErr != nil {
		return runErr
	}
	message := fmt.Sprintf("direct selection for module %s is absent; run dot apply", moduleID)
	if result.ProfileSelected {
		message = fmt.Sprintf(
			"direct selection for module %s is absent; module remains selected by an active profile; run dot apply",
			moduleID,
		)
	}
	return printSelectionResult(
		command,
		message,
		result.Changed,
		"dot select remove "+moduleID,
	)
}

func printSelectionResult(
	command *cobra.Command,
	message string,
	changed bool,
	rerun string,
) error {
	_, err := fmt.Fprintf(
		command.OutOrStdout(),
		"%s selection_changed=%t\n",
		message,
		changed,
	)
	if err == nil || !changed {
		return err
	}
	return fmt.Errorf(
		"selection may already be updated after result output failed: %w; rerun %s",
		err,
		rerun,
	)
}
