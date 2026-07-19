package planner

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/state"
)

// ErrHookPlan 表示 hook planner 无法形成完整、可信的只读计划。
var ErrHookPlan = errors.New("hook plan failed")

// HookVerb 描述一个 run_once 候选的纯计划结果。
type HookVerb string

const (
	// HookSkip 表示相同指纹已经成功执行，state 保持不变。
	HookSkip HookVerb = "skip"
	// HookRun 表示 hook 缺少成功记录或指纹已经变化。
	HookRun HookVerb = "run-hook"
)

// HookExecutionMode 是进入指纹和 future executor invocation 的封闭执行分类。
type HookExecutionMode string

const (
	// HookExecutionDirect 表示脚本任一 executable bit 存在，直接 exec。
	HookExecutionDirect HookExecutionMode = "direct"
	// HookExecutionShell 表示脚本不可执行，以 sh <script> 调起。
	HookExecutionShell HookExecutionMode = "sh"
)

// HookInvocation 保存 future executor 的程序与参数；它不提供执行能力。
type HookInvocation struct {
	Mode      HookExecutionMode
	Program   string
	Arguments []string
}

// HookEnvironment 保存规范要求覆盖继承环境的完整封闭集合。
type HookEnvironment struct {
	Home          string
	XDGConfigHome string
	XDGStateHome  string
	XDGDataHome   string
	DotModule     string
	DotOS         string
	DotProfile    string
	DotRepo       string
	DotTarget     string
}

// HookStateEffectKind 描述一个 hook 结果分支的 run_once state 处置。
type HookStateEffectKind string

const (
	// HookStatePreserve 保留原 run_once 记录；skip 与执行失败均使用它。
	HookStatePreserve HookStateEffectKind = "preserve"
	// HookStateUpsert 在 hook 成功后写入新指纹；executed_at 由 executor 在成功时填入。
	HookStateUpsert HookStateEffectKind = "upsert"
)

// HookStateEffect 保存 run_once key 与成功指纹，不写文件 entry state。
type HookStateEffect struct {
	Kind        HookStateEffectKind
	Key         string
	Fingerprint string
}

// HookAction 是单个 M1 run_once 候选的自包含纯计划。脚本内容按规范是 executor 读取仓库的
// 明确例外；Fingerprint 固定 plan-time bytes 与执行分类，future executor 必须重新验证。
type HookAction struct {
	Verb           HookVerb
	StateKey       string
	Module         string
	Script         string
	ScriptPath     string
	WorkingDir     string
	TargetRoot     string
	TargetRootPath string
	Profile        string
	GOOS           string
	Repository     string
	Invocation     HookInvocation
	Environment    HookEnvironment
	Fingerprint    string
	OnSuccess      HookStateEffect
	OnFailure      HookStateEffect
}

// HookPlan 是按 manifest scope 顺序形成的不可变 run_once 计划。
type HookPlan struct {
	actions []HookAction
}

// Actions 返回不共享 invocation 参数 backing array 的动作副本。
func (plan HookPlan) Actions() []HookAction {
	if len(plan.actions) == 0 {
		return nil
	}
	actions := make([]HookAction, len(plan.actions))
	for index := range plan.actions {
		actions[index] = plan.actions[index].Clone()
	}
	return actions
}

// Clone 返回不共享 invocation 参数 backing array 的副本。
func (action HookAction) Clone() HookAction {
	action.Invocation.Arguments = append([]string(nil), action.Invocation.Arguments...)
	return action
}

type hookRuntime struct {
	Profile    string
	GOOS       string
	Home       string
	Repository string
}

// PlanHooks 只读取 scoped manifest、严格 state 与 hook script，形成完整计划；任一候选失败时
// 返回零计划，不暴露已形成的部分动作。
func PlanHooks(profile manifest.ScopedProfile, loaded state.Loaded, repository string) (HookPlan, error) {
	runtime := hookRuntime{
		Profile:    profile.Name(),
		GOOS:       profile.GOOS(),
		Home:       profile.Home(),
		Repository: repository,
	}
	if err := validateHookRuntime(runtime); err != nil {
		return HookPlan{}, err
	}
	runtime.Repository = filepath.Clean(runtime.Repository)

	history, err := hookHistory(loaded)
	if err != nil {
		return HookPlan{}, err
	}
	hooks := profile.Hooks()
	actions := make([]HookAction, 0, len(hooks))
	for _, descriptor := range hooks {
		key := descriptor.Module + "/" + descriptor.Script
		fingerprint, exists := history[key]
		action, err := planHook(descriptor, runtime, fingerprint, exists)
		if err != nil {
			return HookPlan{}, err
		}
		actions = append(actions, action)
	}
	return HookPlan{actions: actions}, nil
}

func hookHistory(loaded state.Loaded) (map[string]string, error) {
	if loaded.Missing() {
		return nil, nil
	}
	snapshot, ok := loaded.Snapshot()
	if !ok {
		return nil, fmt.Errorf("%w: state load result is invalid", ErrHookPlan)
	}
	history := make(map[string]string, len(snapshot.RunOnceKeys()))
	for _, key := range snapshot.RunOnceKeys() {
		record, ok := snapshot.RunOnce(key)
		if !ok {
			return nil, fmt.Errorf("%w: state run_once key %q has no record", ErrHookPlan, key)
		}
		history[key] = record.Hash()
	}
	return history, nil
}

func planHook(
	descriptor manifest.HookDescriptor,
	runtime hookRuntime,
	historicalFingerprint string,
	hasHistorical bool,
) (HookAction, error) {
	if err := validateHookInputs(descriptor, runtime); err != nil {
		return HookAction{}, err
	}
	info, err := os.Lstat(descriptor.ScriptPath)
	if err != nil {
		return HookAction{}, fmt.Errorf("%w: inspect hook %q: %w", ErrHookPlan, descriptor.ScriptPath, err)
	}
	if !info.Mode().IsRegular() {
		return HookAction{}, fmt.Errorf(
			"%w: hook %q must be a regular file",
			ErrHookPlan,
			descriptor.ScriptPath,
		)
	}
	content, err := os.ReadFile(descriptor.ScriptPath)
	if err != nil {
		return HookAction{}, fmt.Errorf("%w: read hook %q: %w", ErrHookPlan, descriptor.ScriptPath, err)
	}

	mode := HookExecutionShell
	invocation := HookInvocation{
		Mode:      HookExecutionShell,
		Program:   "sh",
		Arguments: []string{descriptor.ScriptPath},
	}
	if info.Mode().Perm()&0o111 != 0 {
		mode = HookExecutionDirect
		invocation = HookInvocation{Mode: HookExecutionDirect, Program: descriptor.ScriptPath}
	}
	fingerprint := hookFingerprint(mode, content)
	key := descriptor.Module + "/" + descriptor.Script
	action := HookAction{
		Verb:           HookRun,
		StateKey:       key,
		Module:         descriptor.Module,
		Script:         descriptor.Script,
		ScriptPath:     descriptor.ScriptPath,
		WorkingDir:     descriptor.ModulePath,
		TargetRoot:     descriptor.TargetRoot,
		TargetRootPath: descriptor.TargetRootPath,
		Profile:        runtime.Profile,
		GOOS:           runtime.GOOS,
		Repository:     runtime.Repository,
		Invocation:     invocation,
		Environment: HookEnvironment{
			Home:          runtime.Home,
			XDGConfigHome: filepath.Join(runtime.Home, ".config"),
			XDGStateHome:  filepath.Join(runtime.Home, ".local", "state"),
			XDGDataHome:   filepath.Join(runtime.Home, ".local", "share"),
			DotModule:     descriptor.Module,
			DotOS:         runtime.GOOS,
			DotProfile:    runtime.Profile,
			DotRepo:       runtime.Repository,
			DotTarget:     descriptor.TargetRootPath,
		},
		Fingerprint: fingerprint,
		OnSuccess: HookStateEffect{
			Kind:        HookStateUpsert,
			Key:         key,
			Fingerprint: fingerprint,
		},
		OnFailure: HookStateEffect{Kind: HookStatePreserve},
	}
	if hasHistorical && historicalFingerprint == fingerprint {
		action.Verb = HookSkip
		action.OnSuccess = HookStateEffect{Kind: HookStatePreserve}
	}
	return action, nil
}

func validateHookInputs(descriptor manifest.HookDescriptor, runtime hookRuntime) error {
	if descriptor.Module == "" || descriptor.Script == "" || descriptor.TargetRoot == "" {
		return fmt.Errorf("%w: hook descriptor has empty module, script, or target root", ErrHookPlan)
	}
	paths := []struct {
		name  string
		value string
	}{
		{name: "module path", value: descriptor.ModulePath},
		{name: "script path", value: descriptor.ScriptPath},
		{name: "target root path", value: descriptor.TargetRootPath},
	}
	for _, candidate := range paths {
		if candidate.value == "" || !filepath.IsAbs(candidate.value) {
			return fmt.Errorf(
				"%w: %s %q must be a non-empty absolute path",
				ErrHookPlan,
				candidate.name,
				candidate.value,
			)
		}
	}
	return validateHookRuntime(runtime)
}

func validateHookRuntime(runtime hookRuntime) error {
	paths := []struct {
		name  string
		value string
	}{
		{name: "HOME", value: runtime.Home},
		{name: "repository", value: runtime.Repository},
	}
	for _, candidate := range paths {
		if candidate.value == "" || !filepath.IsAbs(candidate.value) {
			return fmt.Errorf(
				"%w: %s %q must be a non-empty absolute path",
				ErrHookPlan,
				candidate.name,
				candidate.value,
			)
		}
	}
	if runtime.Profile == "" || (runtime.GOOS != "darwin" && runtime.GOOS != "linux") {
		return fmt.Errorf("%w: hook runtime has invalid profile or OS", ErrHookPlan)
	}
	return nil
}

func hookFingerprint(mode HookExecutionMode, script []byte) string {
	digest := sha256.New()
	writeFingerprintField(digest, "format", []byte("dot-run-once-v1"))
	writeFingerprintField(digest, "execution", []byte(mode))
	writeFingerprintField(digest, "script", script)
	return fmt.Sprintf("sha256:%x", digest.Sum(nil))
}

func writeFingerprintField(destination hash.Hash, label string, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(label)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write([]byte(label))
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}
