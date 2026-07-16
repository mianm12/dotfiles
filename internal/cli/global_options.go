package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// 全局 flag 名称集中定义，使公开 CLI 词汇和 Cobra 的名称查询保持一致。
const (
	repoFlagName         = "repo"
	homeFlagName         = "home"
	profileFlagName      = "profile"
	verboseFlagName      = "verbose"
	verboseFlagShorthand = "v"
	noColorFlagName      = "no-color"
)

type globalOptions struct {
	repo    string
	home    string
	profile string
	verbose bool
	noColor bool
}

// bind 只注册全局 flag，不解析路径或读取机器配置等外部状态。
func (o *globalOptions) bind(root *cobra.Command) error {
	flags := root.PersistentFlags()
	flags.StringVar(&o.repo, repoFlagName, "", "override the repository path")
	flags.StringVar(&o.home, homeFlagName, "", "override the effective home")
	flags.StringVar(&o.profile, profileFlagName, "", "override the configured profile")
	flags.BoolVarP(&o.verbose, verboseFlagName, verboseFlagShorthand, false, "enable verbose output")
	flags.BoolVar(&o.noColor, noColorFlagName, false, "disable colored output")

	// --home 是测试专用的隔离入口，不在常规帮助中展示。
	return flags.MarkHidden(homeFlagName)
}

func (o globalOptions) validate(command *cobra.Command) error {
	if command.Flags().Changed(profileFlagName) && o.profile == "" {
		return errors.New("--profile must not be empty")
	}
	return nil
}
