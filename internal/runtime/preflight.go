// Package runtime 建立可信运行上下文，并按命令语义组合 lock、manifest 与 state 加载。
package runtime

import (
	"errors"
	"fmt"

	"github.com/mianm12/dotfiles/internal/config"
	"github.com/mianm12/dotfiles/internal/paths"
)

// Options 保存本次进程的路径和 profile 覆盖，以及解析平台默认值所需的只读来源。
// *Set 字段区分未提供与显式空值；调用方不得把环境变量预先折叠进 flag 值。
type Options struct {
	Home       string
	HomeSet    bool
	Repo       string
	RepoSet    bool
	Profile    string
	ProfileSet bool

	LookupEnv   func(string) (string, bool)
	UserHomeDir func() (string, error)
}

// ControlContext 是已经完成路径解析和控制面隔离校验的只读运行前置。
type ControlContext struct {
	Home         string
	Config       string
	Repository   string
	ControlPaths paths.ControlPlanePaths
	ControlPlane paths.ControlPlane
}

// Context 是普通 profile/data 消费者的完整 preflight 结果。
type Context struct {
	ControlContext
	Profile string
	Data    map[string]string
}

// InitContext 额外保留配置是否缺失，供未来 init 将初次读取结果作为提交 Precond。
type InitContext struct {
	Context
	ConfigMissing bool
}

// Preflight 要求严格、完整的机器配置，并解析本次 profile/data 和控制面。
func Preflight(options Options) (Context, error) {
	if err := validateProfileOverride(options); err != nil {
		return Context{}, err
	}
	loaded, err := load(options)
	if err != nil {
		return Context{}, err
	}
	if !loaded.configExists {
		return Context{}, fmt.Errorf("machine config %q is missing; run dot init", loaded.control.Config)
	}
	return contextFromLoaded(loaded, options), nil
}

// PreflightInit 允许机器配置缺失，并只在此结果中保留 missing 状态。
func PreflightInit(options Options) (InitContext, error) {
	if err := validateProfileOverride(options); err != nil {
		return InitContext{}, err
	}
	loaded, err := load(options)
	if err != nil {
		return InitContext{}, err
	}
	return InitContext{
		Context:       contextFromLoaded(loaded, options),
		ConfigMissing: !loaded.configExists,
	}, nil
}

// PreflightRepository 为 version 等不消费 profile/data 的只读入口解析 repo 和控制面。
// 配置缺失时继续使用环境或默认 repo，但已有配置仍须完整严格校验。
func PreflightRepository(options Options) (ControlContext, error) {
	loaded, err := load(options)
	if err != nil {
		return ControlContext{}, err
	}
	return loaded.control, nil
}

type loadedContext struct {
	control      ControlContext
	machine      config.Machine
	configExists bool
}

func load(options Options) (loadedContext, error) {
	home, err := paths.EffectiveHome(options.Home, options.HomeSet, options.UserHomeDir)
	if err != nil {
		return loadedContext{}, err
	}
	configPath, err := paths.Config(home, options.LookupEnv)
	if err != nil {
		return loadedContext{}, err
	}
	machine, exists, err := config.Load(configPath)
	if err != nil {
		return loadedContext{}, err
	}
	// 覆盖只决定本次 effective repo，不能掩盖已经持久化的非法配置。
	if exists && machine.Repo != nil {
		if _, err := paths.ResolveControlPath(*machine.Repo, home); err != nil {
			return loadedContext{}, fmt.Errorf("machine config repo: %w", err)
		}
	}
	repository, err := paths.Repository(
		home,
		options.Repo,
		options.RepoSet,
		options.LookupEnv,
		machine.Repo,
	)
	if err != nil {
		return loadedContext{}, err
	}
	controlPaths, err := paths.ResolveControlPlanePaths(home, repository, configPath)
	if err != nil {
		return loadedContext{}, err
	}
	controlPlane, err := paths.ValidateControlPlane(controlPaths)
	if err != nil {
		return loadedContext{}, err
	}
	return loadedContext{
		control: ControlContext{
			Home:         home,
			Config:       configPath,
			Repository:   repository,
			ControlPaths: controlPaths,
			ControlPlane: controlPlane,
		},
		machine:      machine,
		configExists: exists,
	}, nil
}

func contextFromLoaded(loaded loadedContext, options Options) Context {
	profile := loaded.machine.Profile
	if options.ProfileSet {
		profile = options.Profile
	}
	data := make(map[string]string, len(loaded.machine.Data))
	for key, value := range loaded.machine.Data {
		data[key] = value
	}
	return Context{
		ControlContext: loaded.control,
		Profile:        profile,
		Data:           data,
	}
}

func validateProfileOverride(options Options) error {
	if options.ProfileSet && options.Profile == "" {
		return errors.New("--profile must not be empty")
	}
	return nil
}
