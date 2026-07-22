package executor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mianm12/dotfiles/internal/planner"
)

var (
	// ErrUnsupportedHookAction 表示动作不属于当前 executor 的封闭 hook 能力。
	ErrUnsupportedHookAction = errors.New("unsupported hook action")
	// ErrHookPrecondition 表示 script 的 execute-time 观测不再等于 canonical plan。
	ErrHookPrecondition = errors.New("hook action precondition failed")
)

// HookStreams 是 hook 子进程直接使用的调用方 stdio。
type HookStreams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// HookResult 保存单个 hook 执行分支选择的 run_once state effect。
type HookResult struct {
	StateEffect planner.HookStateEffect
}

// ExecuteHook 在启动子进程前重新观测 script 的 regular-file、bytes 与 executable class，
// 再按 canonical invocation、工作目录、环境覆盖和调用方 stdio 执行 HookRun。
func ExecuteHook(action planner.HookAction, streams HookStreams) (HookResult, error) {
	failure := HookResult{StateEffect: action.OnFailure}
	if err := ValidateHookAction(action); err != nil {
		return failure, err
	}
	if action.Verb != planner.HookRun {
		return failure, fmt.Errorf("%w: verb %q is not executable", ErrUnsupportedHookAction, action.Verb)
	}
	if streams.Stdin == nil || streams.Stdout == nil || streams.Stderr == nil {
		return failure, fmt.Errorf("%w: hook streams must all be present", ErrUnsupportedHookAction)
	}
	if err := revalidateHookScript(action); err != nil {
		return failure, err
	}

	command := exec.Command(action.Invocation.Program, action.Invocation.Arguments...)
	command.Dir = action.WorkingDir
	command.Env = mergeHookEnvironment(os.Environ(), action.Environment)
	command.Stdin = streams.Stdin
	command.Stdout = streams.Stdout
	command.Stderr = streams.Stderr
	if err := command.Run(); err != nil {
		return failure, fmt.Errorf("run hook %q: %w", action.StateKey, err)
	}
	return HookResult{StateEffect: action.OnSuccess}, nil
}

// ValidateHookAction 不读取文件系统，检查 HookRun 或 HookSkip 是否内部一致；执行函数另行拒绝
// 不需要启动子进程的 HookSkip。
func ValidateHookAction(action planner.HookAction) error {
	if action.Verb != planner.HookRun && action.Verb != planner.HookSkip {
		return fmt.Errorf("%w: verb %q is unsupported", ErrUnsupportedHookAction, action.Verb)
	}
	if action.Module == "" || action.Script == "" || action.TargetRoot == "" ||
		action.Profile == "" || (action.GOOS != "darwin" && action.GOOS != "linux") {
		return fmt.Errorf("%w: hook identity or runtime is incomplete", ErrUnsupportedHookAction)
	}
	if action.StateKey != action.Module+"/"+action.Script {
		return fmt.Errorf("%w: hook state key is inconsistent", ErrUnsupportedHookAction)
	}
	for name, value := range map[string]string{
		"script":      action.ScriptPath,
		"working dir": action.WorkingDir,
		"target":      action.TargetRootPath,
		"repository":  action.Repository,
		"HOME":        action.Environment.Home,
	} {
		if value == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("%w: hook %s path %q is not absolute", ErrUnsupportedHookAction, name, value)
		}
	}
	if filepath.Clean(filepath.Join(action.WorkingDir, filepath.FromSlash(action.Script))) != action.ScriptPath {
		return fmt.Errorf("%w: hook script path is inconsistent", ErrUnsupportedHookAction)
	}
	wantEnvironment := planner.HookEnvironment{
		Home:          action.Environment.Home,
		XDGConfigHome: filepath.Join(action.Environment.Home, ".config"),
		XDGStateHome:  filepath.Join(action.Environment.Home, ".local", "state"),
		XDGDataHome:   filepath.Join(action.Environment.Home, ".local", "share"),
		DotModule:     action.Module,
		DotOS:         action.GOOS,
		DotProfile:    action.Profile,
		DotRepo:       action.Repository,
		DotTarget:     action.TargetRootPath,
	}
	if action.Environment != wantEnvironment {
		return fmt.Errorf("%w: hook environment is inconsistent", ErrUnsupportedHookAction)
	}
	switch action.Invocation.Mode {
	case planner.HookExecutionDirect:
		if action.Invocation.Program != action.ScriptPath || len(action.Invocation.Arguments) != 0 {
			return fmt.Errorf("%w: direct invocation is inconsistent", ErrUnsupportedHookAction)
		}
	case planner.HookExecutionShell:
		if action.Invocation.Program != "sh" ||
			!slices.Equal(action.Invocation.Arguments, []string{action.ScriptPath}) {
			return fmt.Errorf("%w: shell invocation is inconsistent", ErrUnsupportedHookAction)
		}
	default:
		return fmt.Errorf("%w: execution mode %q is unsupported", ErrUnsupportedHookAction, action.Invocation.Mode)
	}
	if !strings.HasPrefix(action.Fingerprint, "sha256:") || len(action.Fingerprint) != len("sha256:")+64 {
		return fmt.Errorf("%w: hook fingerprint is unsupported", ErrUnsupportedHookAction)
	}
	if action.OnFailure != (planner.HookStateEffect{Kind: planner.HookStatePreserve}) {
		return fmt.Errorf("%w: hook state effects are inconsistent", ErrUnsupportedHookAction)
	}
	switch action.Verb {
	case planner.HookRun:
		if action.OnSuccess.Kind != planner.HookStateUpsert ||
			action.OnSuccess.Key != action.StateKey ||
			action.OnSuccess.Fingerprint != action.Fingerprint {
			return fmt.Errorf("%w: run hook success effect is inconsistent", ErrUnsupportedHookAction)
		}
	case planner.HookSkip:
		if action.OnSuccess != (planner.HookStateEffect{Kind: planner.HookStatePreserve}) {
			return fmt.Errorf("%w: skipped hook effect is inconsistent", ErrUnsupportedHookAction)
		}
	}
	return nil
}

func revalidateHookScript(action planner.HookAction) error {
	info, err := os.Lstat(action.ScriptPath)
	if err != nil {
		return fmt.Errorf("%w: inspect hook %q: %w", ErrHookPrecondition, action.ScriptPath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: hook %q is not a regular file", ErrHookPrecondition, action.ScriptPath)
	}
	content, err := os.ReadFile(action.ScriptPath)
	if err != nil {
		return fmt.Errorf("%w: read hook %q: %w", ErrHookPrecondition, action.ScriptPath, err)
	}
	mode := planner.HookExecutionShell
	if info.Mode().Perm()&0o111 != 0 {
		mode = planner.HookExecutionDirect
	}
	if mode != action.Invocation.Mode || planner.HookFingerprint(mode, content) != action.Fingerprint {
		return fmt.Errorf("%w: hook %q observation changed", ErrHookPrecondition, action.ScriptPath)
	}
	return nil
}

var hookEnvironmentKeys = []string{
	"HOME",
	"XDG_CONFIG_HOME",
	"XDG_STATE_HOME",
	"XDG_DATA_HOME",
	"DOT_MODULE",
	"DOT_OS",
	"DOT_PROFILE",
	"DOT_REPO",
	"DOT_TARGET",
}

func mergeHookEnvironment(parent []string, environment planner.HookEnvironment) []string {
	overrides := make(map[string]struct{}, len(hookEnvironmentKeys))
	for _, key := range hookEnvironmentKeys {
		overrides[key] = struct{}{}
	}
	merged := make([]string, 0, len(parent)+len(hookEnvironmentKeys))
	for _, entry := range parent {
		key, _, found := strings.Cut(entry, "=")
		if _, overridden := overrides[key]; found && overridden {
			continue
		}
		merged = append(merged, entry)
	}
	return append(merged,
		"HOME="+environment.Home,
		"XDG_CONFIG_HOME="+environment.XDGConfigHome,
		"XDG_STATE_HOME="+environment.XDGStateHome,
		"XDG_DATA_HOME="+environment.XDGDataHome,
		"DOT_MODULE="+environment.DotModule,
		"DOT_OS="+environment.DotOS,
		"DOT_PROFILE="+environment.DotProfile,
		"DOT_REPO="+environment.DotRepo,
		"DOT_TARGET="+environment.DotTarget,
	)
}
