package cli

import (
	"errors"
	"fmt"

	"github.com/mianm12/dotfiles/internal/planner"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/spf13/cobra"
)

type statusFinding struct {
	target      string
	description string
}

type statusProjection struct {
	summary    string
	drift      []statusFinding
	pending    []statusFinding
	orphans    []statusFinding
	unassigned []statusFinding
	notices    []string
	clean      bool
	exitCode   int
}

func newStatusCommand(env environment, global *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Inspect the current dotfiles health",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			plan, err := planner.PlanApply(planner.ApplyOptions{
				Runtime: dotruntime.Overrides{
					Home: dotruntime.Override{
						Value: global.home,
						Set:   command.Flags().Changed(homeFlagName),
					},
					Repository: dotruntime.Override{
						Value: global.repo,
						Set:   command.Flags().Changed(repoFlagName),
					},
					Profile: dotruntime.Override{
						Value: global.profile,
						Set:   command.Flags().Changed(profileFlagName),
					},
				},
				CLIVersion: env.build.Version,
			})
			if err != nil {
				return err
			}
			projection, err := projectStatus(plan)
			if err != nil {
				return err
			}
			printStatusProjection(command, projection)
			return commandExit(projection.exitCode)
		},
	}
}

func projectStatus(plan planner.ApplyPlan) (statusProjection, error) {
	if !plan.Valid() {
		return statusProjection{}, errors.New("cannot present an invalid apply plan as status")
	}
	context := plan.Context()
	observed := plan.Observed()
	projection := statusProjection{
		summary: fmt.Sprintf(
			"Profile: %s (%d modules, %d files managed)",
			context.Profile,
			len(context.Modules),
			len(observed.Targets()),
		),
	}
	if context.DevelopmentBuild {
		projection.notices = append(projection.notices, "development build skipped the requires version comparison")
	}

	for _, action := range plan.FileActions() {
		if action.Verb == planner.ActionSkip {
			continue
		}
		description, err := statusFileDescription(action)
		if err != nil {
			return statusProjection{}, err
		}
		finding := statusFinding{target: action.Target, description: description}
		if action.Reason == planner.ReasonLinkDrift {
			projection.drift = append(projection.drift, finding)
		} else {
			projection.pending = append(projection.pending, finding)
		}
	}

	for _, action := range plan.Hooks().Actions() {
		switch action.Verb {
		case planner.HookSkip:
			continue
		case planner.HookRun:
			projection.pending = append(projection.pending, statusFinding{
				target:      action.StateKey,
				description: "run_once pending execution",
			})
		default:
			return statusProjection{}, fmt.Errorf("unsupported status hook verb %q", action.Verb)
		}
	}

	for _, action := range plan.Prune().Actions() {
		description, err := statusOrphanDescription(action)
		if err != nil {
			return statusProjection{}, err
		}
		projection.orphans = append(projection.orphans, statusFinding{
			target:      action.Target,
			description: description,
		})
	}

	for _, module := range context.UnassignedModules {
		projection.unassigned = append(projection.unassigned, statusFinding{
			target:      module,
			description: "not referenced by any profile",
		})
	}

	if len(projection.drift)+len(projection.pending)+len(projection.orphans) == 0 {
		projection.clean = true
		projection.exitCode = exitOK
	} else {
		projection.exitCode = exitActionable
	}
	return projection, nil
}

func statusFileDescription(action planner.Action) (string, error) {
	switch action.Reason {
	case planner.ReasonTargetMissing:
		switch action.Desired.Kind {
		case planner.DesiredLink:
			return "desired symlink missing", nil
		case planner.DesiredScaffold:
			return "scaffold not yet created", nil
		default:
			return "", fmt.Errorf("unsupported status desired kind %q", action.Desired.Kind)
		}
	case planner.ReasonStateMetadata:
		return "state metadata needs refresh", nil
	case planner.ReasonOwnedLinkStale:
		return "owned symlink points to previous source", nil
	case planner.ReasonLinkDrift:
		return "symlink re-pointed elsewhere", nil
	case planner.ReasonUnownedLink:
		return "unowned symlink blocks desired link", nil
	case planner.ReasonRegularConflict:
		return "regular file blocks desired link", nil
	case planner.ReasonDirectoryConflict:
		return "directory blocks desired link", nil
	case planner.ReasonSpecialConflict:
		return "special file blocks desired link", nil
	case planner.ReasonScaffoldPresent:
		return "scaffold lifecycle not recorded", nil
	case planner.ReasonScaffoldRebuild:
		return "scaffold rebuild pending", nil
	case planner.ReasonOwnedLinkToScaffold:
		return "owned symlink pending scaffold migration", nil
	case planner.ReasonReleaseOwnershipToScaffold:
		return "scaffold ownership release pending", nil
	case planner.ReasonExpectedLink, planner.ReasonScaffoldDeleted:
		return "", fmt.Errorf("status received non-actionable reason %q for verb %q", action.Reason, action.Verb)
	default:
		return "", fmt.Errorf("unsupported status file reason %q", action.Reason)
	}
}

func statusOrphanDescription(action planner.PruneAction) (string, error) {
	var description string
	switch action.Reason {
	case planner.PruneReasonScaffold:
		description = "scaffold orphan pending state cleanup"
	case planner.PruneReasonOwned:
		description = "owned orphan from previous profile"
	case planner.PruneReasonUnowned:
		description = "unowned orphan pending state cleanup"
	default:
		return "", fmt.Errorf("unsupported status prune reason %q", action.Reason)
	}
	if action.Deferred {
		description += "; prune deferred by file conflict"
	}
	return description, nil
}

func printStatusProjection(command *cobra.Command, projection statusProjection) {
	command.Println(projection.summary)
	printStatusSection(command, "DRIFT", projection.drift)
	printStatusSection(command, "PENDING", projection.pending)
	printStatusSection(command, "ORPHAN / PENDING PRUNE", projection.orphans)
	printStatusSection(command, "UNASSIGNED MODULES", projection.unassigned)
	if projection.clean {
		command.Println()
		command.Println("Clean.")
	}
	for _, notice := range projection.notices {
		command.PrintErrf("notice: %s\n", notice)
	}
}

func printStatusSection(command *cobra.Command, title string, findings []statusFinding) {
	if len(findings) == 0 {
		return
	}
	command.Println()
	command.Printf("%s (%d)\n", title, len(findings))
	for _, finding := range findings {
		command.Printf("  %-30s  %s\n", finding.target, finding.description)
	}
}
