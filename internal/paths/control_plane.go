package paths

import (
	"fmt"
	"path/filepath"
)

type controlFamily uint8

const (
	controlFamilyRepository controlFamily = iota + 1
	controlFamilyConfig
	controlFamilyState
	controlFamilyBinary
)

type controlMemberRole uint8

const (
	controlMemberRepository controlMemberRole = iota
	controlMemberConfig
	controlMemberStateRoot
	controlMemberStateFile
	controlMemberStateLock
	controlMemberBackupRoot
	controlMemberInstalledBinary
	controlMemberCount
)

type controlPathMember struct {
	role      controlMemberRole
	family    controlFamily
	path      string
	parent    controlMemberRole
	hasParent bool
}

// ControlPlanePaths 保存一次运行的 effective HOME，以及 repo、config、state 家族与已安装
// binary 的绝对展示路径。字段与 state 预定父子关系保持不透明，由本类型作为唯一成员定义。
type ControlPlanePaths struct {
	home    string
	members [controlMemberCount]controlPathMember
}

// ResolveControlPlanePaths 从已经选定的 effective HOME、repo 与 config 构造控制面路径。
// 该函数只做绝对路径校验和词法清理，不读取或修改文件系统。
func ResolveControlPlanePaths(home, repo, config string) (ControlPlanePaths, error) {
	cleanHome, err := cleanControlPlaneInput("effective HOME", home)
	if err != nil {
		return ControlPlanePaths{}, err
	}
	cleanRepo, err := cleanControlPlaneInput("repository", repo)
	if err != nil {
		return ControlPlanePaths{}, err
	}
	cleanConfig, err := cleanControlPlaneInput("machine config", config)
	if err != nil {
		return ControlPlanePaths{}, err
	}

	stateRoot := filepath.Join(cleanHome, ".local", "state", "dot")
	return ControlPlanePaths{home: cleanHome, members: [controlMemberCount]controlPathMember{
		controlMemberRepository: {
			role:   controlMemberRepository,
			family: controlFamilyRepository,
			path:   cleanRepo,
		},
		controlMemberConfig: {
			role:   controlMemberConfig,
			family: controlFamilyConfig,
			path:   cleanConfig,
		},
		controlMemberStateRoot: {
			role:   controlMemberStateRoot,
			family: controlFamilyState,
			path:   stateRoot,
		},
		controlMemberStateFile: newStateChild(
			controlMemberStateFile,
			filepath.Join(stateRoot, "state.json"),
		),
		controlMemberStateLock: newStateChild(
			controlMemberStateLock,
			filepath.Join(stateRoot, "lock"),
		),
		controlMemberBackupRoot: newStateChild(
			controlMemberBackupRoot,
			filepath.Join(stateRoot, "backup"),
		),
		controlMemberInstalledBinary: {
			role:   controlMemberInstalledBinary,
			family: controlFamilyBinary,
			path:   filepath.Join(cleanHome, ".local", "bin", "dot"),
		},
	}}, nil
}

// EffectiveHome 返回构造整组控制面路径时使用的有效 HOME。
func (paths ControlPlanePaths) EffectiveHome() string {
	return paths.home
}

func cleanControlPlaneInput(name, path string) (string, error) {
	cleanPath, err := cleanAbsolutePath(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return cleanPath, nil
}

func newStateChild(role controlMemberRole, path string) controlPathMember {
	return controlPathMember{
		role:      role,
		family:    controlFamilyState,
		path:      path,
		parent:    controlMemberStateRoot,
		hasParent: true,
	}
}

// Repository 返回本次运行的有效 repo tree 展示路径。
func (paths ControlPlanePaths) Repository() string {
	return paths.members[controlMemberRepository].path
}

// Config 返回本次运行的机器配置文件展示路径。
func (paths ControlPlanePaths) Config() string {
	return paths.members[controlMemberConfig].path
}

// StateRoot 返回 state 家族根目录展示路径。
func (paths ControlPlanePaths) StateRoot() string {
	return paths.members[controlMemberStateRoot].path
}

// StateFile 返回 state.json 展示路径。
func (paths ControlPlanePaths) StateFile() string {
	return paths.members[controlMemberStateFile].path
}

// StateLock 返回单实例 lock 文件展示路径。
func (paths ControlPlanePaths) StateLock() string {
	return paths.members[controlMemberStateLock].path
}

// BackupRoot 返回 backup 家族根目录展示路径。
func (paths ControlPlanePaths) BackupRoot() string {
	return paths.members[controlMemberBackupRoot].path
}

// InstalledBinary 返回规范安装位置中的 dot binary 展示路径。
func (paths ControlPlanePaths) InstalledBinary() string {
	return paths.members[controlMemberInstalledBinary].path
}
