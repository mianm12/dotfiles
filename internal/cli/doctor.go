package cli

import (
	"errors"

	"github.com/mianm12/dotfiles/internal/doctor"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/spf13/cobra"
)

type doctorOptions struct {
	manifestOnly bool
	home         string
	homeSet      bool
	repo         string
	repoSet      bool
	profile      string
}

func newDoctorCommand(env environment, global *globalOptions) *cobra.Command {
	var manifestOnly bool
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check dot configuration health",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runDoctor(command, doctorOptions{
				manifestOnly: manifestOnly,
				home:         global.home,
				homeSet:      command.Flags().Changed(homeFlagName),
				repo:         global.repo,
				repoSet:      command.Flags().Changed(repoFlagName),
				profile:      global.profile,
			}, env)
		},
	}
	command.Flags().BoolVar(
		&manifestOnly,
		"manifest-only",
		false,
		"check repository manifests without reading machine configuration or state",
	)
	return command
}

func runDoctor(command *cobra.Command, options doctorOptions, env environment) error {
	if !options.manifestOnly {
		return errors.New("full doctor requires M2; use dot doctor --manifest-only for the M1 static check")
	}

	home, err := paths.EffectiveHome(options.home, options.homeSet, env.userHomeDir)
	if err != nil {
		return err
	}
	configPath, err := paths.Config(home, env.lookupEnv)
	if err != nil {
		return err
	}
	repository, err := paths.Repository(home, options.repo, options.repoSet, env.lookupEnv, nil)
	if err != nil {
		return err
	}

	result := doctor.CheckManifest(command.Context(), doctor.ManifestOptions{
		Repository: repository,
		Version:    env.build.Version,
		Home:       home,
		Config:     configPath,
		GOOS:       env.goos,
		Profile:    options.profile,
	})
	findings := result.Findings()
	for _, finding := range findings {
		command.PrintErrf("%s [%s]: %s\n", finding.Severity, finding.Check, finding.Message)
	}
	for _, notice := range result.Notices() {
		command.PrintErrf("notice: %s\n", notice)
	}
	if len(findings) == 0 {
		command.Println("Manifest check passed.")
	}
	return commandExit(result.ExitCode())
}
