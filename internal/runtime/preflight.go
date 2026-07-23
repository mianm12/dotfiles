// Package runtime 建立可信运行上下文，并按命令语义组合 lock、manifest 与 state 加载。
package runtime

import (
	"errors"
	"fmt"
	"os"

	"github.com/mianm12/dotfiles/internal/config"
	"github.com/mianm12/dotfiles/internal/paths"
)

// Override 保存一个可选的字符串覆盖。Set 区分未提供与显式空值。
type Override struct {
	Value string
	Set   bool
}

// Overrides 只保存本次调用的路径和 profile 覆盖，不混入环境或平台来源。
type Overrides struct {
	Home       Override
	Repository Override
	Profile    Override
}

// Resolver 保存 preflight 解析所需的窄系统来源。
// 它是 CLI 测试与 production 共享真实解析逻辑的具体接缝，不承载策略。
type Resolver struct {
	lookupEnv   func(string) (string, bool)
	userHomeDir func() (string, error)
}

// NewResolver 使用明确来源建立 preflight resolver。nil 来源会在消费前返回错误，不会 panic。
func NewResolver(
	lookupEnv func(string) (string, bool),
	userHomeDir func() (string, error),
) Resolver {
	return Resolver{lookupEnv: lookupEnv, userHomeDir: userHomeDir}
}

// ControlContext 是已经完成路径解析和控制面隔离校验的只读运行前置。
// 所有路径均由同一个 opaque ControlPlanePaths 派生，避免重复真相漂移。
type ControlContext struct {
	paths paths.ControlPlanePaths
}

// Paths 返回本次运行已校验的整组控制面路径。
func (context ControlContext) Paths() paths.ControlPlanePaths { return context.paths }

// Home 返回本次运行的 effective HOME。
func (context ControlContext) Home() string { return context.paths.EffectiveHome() }

// ConfigPath 返回本次运行的机器配置文件路径。
func (context ControlContext) ConfigPath() string { return context.paths.Config() }

// RepositoryPath 返回本次运行的仓库路径。
func (context ControlContext) RepositoryPath() string { return context.paths.Repository() }

// MachineContext 是严格机器配置形成的不可变 profile/repo 快照。
type MachineContext struct {
	profile string
	repo    string
	repoSet bool
}

// Profile 返回该机器上下文的 profile。
func (context MachineContext) Profile() string { return context.profile }

// Repo 返回机器配置中持久化的 repo；字段缺失时 ok 为 false。
func (context MachineContext) Repo() (value string, ok bool) { return context.repo, context.repoSet }

// RunContext 是普通 profile 消费者的完整 preflight 结果。
type RunContext struct {
	control ControlContext
	machine MachineContext
}

// Control 返回已校验的控制面上下文。
func (context RunContext) Control() ControlContext { return context.control }

// Profile 返回本次运行的 effective profile。
func (context RunContext) Profile() string { return context.machine.Profile() }

// InitContext 保存 init 配置选择所需的控制面、已有机器配置和显式 profile 覆盖。
// 配置缺失时不会向调用方暴露伪造的普通 RunContext。
type InitContext struct {
	control          ControlContext
	existing         MachineContext
	valid            bool
	configExists     bool
	profileOverride  Override
	repositorySource paths.RepositorySource
	configSnapshot   config.Snapshot
}

// Control 返回 init 已校验的控制面上下文。
func (context InitContext) Control() ControlContext { return context.control }

// ConfigMissing 报告初次严格读取时机器配置是否确认缺失。
func (context InitContext) ConfigMissing() bool { return context.valid && !context.configExists }

// ExistingMachine 返回已有机器配置的不可变快照；配置缺失时 ok 为 false。
func (context InitContext) ExistingMachine() (machine MachineContext, ok bool) {
	if !context.valid || !context.configExists {
		return MachineContext{}, false
	}
	return context.existing, true
}

// ProfileOverride 返回 init 调用显式提供的 profile；未提供时 ok 为 false。
func (context InitContext) ProfileOverride() (profile string, ok bool) {
	if !context.valid {
		return "", false
	}
	return context.profileOverride.Value, context.profileOverride.Set
}

// RepositorySource 返回 effective repo 的决策来源。
func (context InitContext) RepositorySource() paths.RepositorySource {
	if !context.valid {
		return ""
	}
	return context.repositorySource
}

// ConfigSnapshot 返回初次 strict config 读取的不可变快照。
func (context InitContext) ConfigSnapshot() config.Snapshot { return context.configSnapshot }

// Preflight 使用 production 系统来源完成严格、完整的机器运行前置。
func Preflight(overrides Overrides) (RunContext, error) {
	return systemResolver().Preflight(overrides)
}

// PreflightInit 使用 production 系统来源完成允许 config missing 的 init 前置。
func PreflightInit(overrides Overrides) (InitContext, error) {
	return systemResolver().PreflightInit(overrides)
}

// PreflightRepository 使用 production 系统来源解析 repo 和控制面。
func PreflightRepository(overrides Overrides) (ControlContext, error) {
	return systemResolver().PreflightRepository(overrides)
}

// Preflight 要求严格、完整的机器配置，并解析本次 profile/data 和控制面。
func (resolver Resolver) Preflight(overrides Overrides) (RunContext, error) {
	if err := validateProfileOverride(overrides); err != nil {
		return RunContext{}, err
	}
	loaded, err := resolver.load(overrides)
	if err != nil {
		return RunContext{}, err
	}
	if !loaded.configExists {
		return RunContext{}, fmt.Errorf("machine config %q is missing; run dot init", loaded.control.ConfigPath())
	}
	return runContextFromLoaded(loaded, overrides), nil
}

// PreflightInit 允许机器配置缺失，并只在该结果中保留 missing 提交前提。
func (resolver Resolver) PreflightInit(overrides Overrides) (InitContext, error) {
	if err := validateProfileOverride(overrides); err != nil {
		return InitContext{}, err
	}
	loaded, err := resolver.load(overrides)
	if err != nil {
		return InitContext{}, err
	}
	return InitContext{
		control:          loaded.control,
		existing:         machineContext(loaded.machine),
		valid:            true,
		configExists:     loaded.configExists,
		profileOverride:  overrides.Profile,
		repositorySource: loaded.repositorySource,
		configSnapshot:   loaded.configSnapshot,
	}, nil
}

// PreflightRepository 为 version 等不消费 profile/data 的入口解析 repo 和控制面。
// 配置缺失时继续使用环境或默认 repo，但已有配置仍须完整严格校验。
func (resolver Resolver) PreflightRepository(overrides Overrides) (ControlContext, error) {
	loaded, err := resolver.load(overrides)
	if err != nil {
		return ControlContext{}, err
	}
	return loaded.control, nil
}

type loadedContext struct {
	control          ControlContext
	machine          config.Machine
	configExists     bool
	repositorySource paths.RepositorySource
	configSnapshot   config.Snapshot
}

func (resolver Resolver) load(overrides Overrides) (loadedContext, error) {
	if err := resolver.validate(); err != nil {
		return loadedContext{}, err
	}
	home, err := paths.EffectiveHome(
		overrides.Home.Value,
		overrides.Home.Set,
		resolver.userHomeDir,
	)
	if err != nil {
		return loadedContext{}, err
	}
	configPath, err := paths.Config(home, resolver.lookupEnv)
	if err != nil {
		return loadedContext{}, err
	}
	configSnapshot, err := config.LoadSnapshot(configPath)
	if err != nil {
		return loadedContext{}, err
	}
	machine := configSnapshot.Machine()
	exists := configSnapshot.Exists()
	// 覆盖只决定本次 effective repo，不能掩盖已经持久化的非法配置。
	if exists && machine.Repo != nil {
		if _, err := paths.ResolveControlPath(*machine.Repo, home); err != nil {
			return loadedContext{}, fmt.Errorf("machine config repo: %w", err)
		}
	}
	repository, err := paths.ResolveRepository(
		home,
		overrides.Repository.Value,
		overrides.Repository.Set,
		resolver.lookupEnv,
		machine.Repo,
	)
	if err != nil {
		return loadedContext{}, err
	}
	controlPaths, err := paths.ResolveControlPlanePaths(home, repository.Path(), configPath)
	if err != nil {
		return loadedContext{}, err
	}
	if _, err := paths.ValidateControlPlane(controlPaths); err != nil {
		return loadedContext{}, err
	}
	return loadedContext{
		control:          ControlContext{paths: controlPaths},
		machine:          machine,
		configExists:     exists,
		repositorySource: repository.Source(),
		configSnapshot:   configSnapshot,
	}, nil
}

func (resolver Resolver) validate() error {
	if resolver.lookupEnv == nil {
		return errors.New("runtime environment lookup source is nil")
	}
	if resolver.userHomeDir == nil {
		return errors.New("runtime user HOME source is nil")
	}
	return nil
}

func systemResolver() Resolver {
	return NewResolver(os.LookupEnv, os.UserHomeDir)
}

func runContextFromLoaded(loaded loadedContext, overrides Overrides) RunContext {
	machine := machineContext(loaded.machine)
	if overrides.Profile.Set {
		machine.profile = overrides.Profile.Value
	}
	return RunContext{control: loaded.control, machine: machine}
}

func machineContext(machine config.Machine) MachineContext {
	context := MachineContext{profile: machine.Profile}
	if machine.Repo != nil {
		context.repo = *machine.Repo
		context.repoSet = true
	}
	return context
}

func validateProfileOverride(overrides Overrides) error {
	if overrides.Profile.Set && overrides.Profile.Value == "" {
		return errors.New("--profile must not be empty")
	}
	return nil
}
