package cli

import (
	"errors"
	"fmt"

	"github.com/mianm12/dotfiles/internal/manifest"
	runtimecontext "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/spf13/cobra"
)

// versionOptions 同时保留值和 flag 是否显式出现，以区分“未提供”与“显式空值”。
type versionOptions struct {
	home    string
	homeSet bool
	repo    string
	repoSet bool
}

func newVersionCommand(env environment, global *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show build and repository compatibility information",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runVersion(command, versionOptions{
				home:    global.home,
				homeSet: command.Flags().Changed(homeFlagName),
				repo:    global.repo,
				repoSet: command.Flags().Changed(repoFlagName),
			}, env)
		},
	}
}

func runVersion(command *cobra.Command, options versionOptions, env environment) error {
	// 先输出构建信息，使后续配置或 compatibility 检查失败时仍能识别当前二进制。
	command.Printf("version=%s\n", env.build.Version)
	command.Printf("commit=%s\n", env.build.Commit)
	command.Printf("build_time=%s\n", env.build.BuildTime)

	context, err := runtimecontext.PreflightRepository(runtimecontext.Options{
		Home:        options.home,
		HomeSet:     options.homeSet,
		Repo:        options.repo,
		RepoSet:     options.repoSet,
		LookupEnv:   env.lookupEnv,
		UserHomeDir: env.userHomeDir,
	})
	if err != nil {
		return reportVersionError(command, err)
	}
	requirement, err := manifest.ReadRequirement(context.Repository)
	if errors.Is(err, manifest.ErrRepositoryUnavailable) {
		// 尚未安装仓库时仍允许 version 成功，并明确报告 requires 不可用。
		command.Println("requires=unavailable")
		return nil
	}
	if err != nil {
		return reportVersionError(command, err)
	}

	command.Printf("requires=%s\n", requirement)
	satisfied, developmentBuild, err := manifest.Satisfies(env.build.Version, requirement)
	if err != nil {
		command.Println("satisfied=error")
		return err
	}
	command.Printf("satisfied=%t\n", satisfied)
	if developmentBuild {
		command.Println("compatibility=development-build")
		command.PrintErrln("notice: development build skipped the requires version comparison")
		return nil
	}
	if !satisfied {
		return fmt.Errorf("CLI %s does not satisfy %s; run dot self-update", env.build.Version, requirement)
	}
	return nil
}

func reportVersionError(command *cobra.Command, err error) error {
	command.Println("requires=error")
	return err
}
