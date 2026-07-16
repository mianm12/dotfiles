// Package cli 使用 Cobra 组装 dot 命令，并将执行结果映射为进程退出码。
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ghstlnx/dotfiles/internal/buildinfo"
	"github.com/spf13/cobra"
)

// environment 集中保存 CLI 的外部依赖，便于测试替换 I/O、环境变量、HOME 和构建元数据。
type environment struct {
	stdout      io.Writer
	stderr      io.Writer
	lookupEnv   func(string) (string, bool)
	userHomeDir func() (string, error)
	build       buildinfo.Info
}

type globalOptions struct {
	home    string
	profile string
	repo    string
	verbose bool
	noColor bool
}

// Run 使用不含程序名的 args 执行 dot，将命令输出写入 stdout 和 stderr，
// 并返回进程退出码。
func Run(args []string, stdout, stderr io.Writer) int {
	return run(args, environment{
		stdout:      stdout,
		stderr:      stderr,
		lookupEnv:   os.LookupEnv,
		userHomeDir: os.UserHomeDir,
		build:       buildinfo.Current(),
	})
}

func run(args []string, env environment) int {
	root, err := newRootCommand(env)
	if err != nil {
		_, _ = fmt.Fprintf(env.stderr, "error: initialize CLI: %v\n", err)
		return 1
	}
	root.SetArgs(args)
	root.SetOut(env.stdout)
	root.SetErr(env.stderr)

	if err := root.Execute(); err != nil {
		root.PrintErrf("error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCommand(env environment) (*cobra.Command, error) {
	var options globalOptions
	// 禁用 Cobra 自动错误和 usage 输出，由命令与 run 按统一格式处理。
	root := &cobra.Command{
		Use:           "dot",
		Short:         "Manage a personal dotfiles repository",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := command.Help(); err != nil {
				return err
			}
			return errors.New("a command is required")
		},
		PersistentPreRunE: func(command *cobra.Command, _ []string) error {
			if command.Flags().Changed("profile") && options.profile == "" {
				return errors.New("--profile must not be empty")
			}
			return nil
		},
	}
	// completion 尚未进入公开命令规范，因此禁用 Cobra 自动生成的子命令。
	root.CompletionOptions.DisableDefaultCmd = true

	flags := root.PersistentFlags()
	flags.StringVar(&options.repo, "repo", "", "override the repository path")
	flags.StringVar(&options.home, "home", "", "override the effective home")
	flags.StringVar(&options.profile, "profile", "", "override the configured profile")
	flags.BoolVarP(&options.verbose, "verbose", "v", false, "enable verbose output")
	flags.BoolVar(&options.noColor, "no-color", false, "disable colored output")
	// --home 是测试专用的隔离入口，不在常规帮助中展示。
	if err := flags.MarkHidden("home"); err != nil {
		return nil, err
	}

	root.AddCommand(newVersionCommand(env, &options))
	return root, nil
}
