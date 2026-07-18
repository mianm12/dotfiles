package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mianm12/dotfiles/internal/manifest"
	"github.com/mianm12/dotfiles/internal/paths"
)

const developmentNotice = "development build skipped the requires version comparison"

// ManifestOptions 是 manifest-only 静态检查无需 machine config 即可确定的输入。
type ManifestOptions struct {
	Repository string
	Version    string
	Home       string
	Config     string
	GOOS       string
	Profile    string
}

// CheckManifest 执行不依赖 machine config 或 state 的只读 manifest 诊断。
func CheckManifest(ctx context.Context, options ManifestOptions) Result {
	findings := make([]Finding, 0)
	notices := make([]string, 0)

	requirement, requirementErr := manifest.ReadRequirement(options.Repository)
	if requirementErr == nil {
		satisfied, developmentBuild, err := manifest.Satisfies(options.Version, requirement)
		switch {
		case err != nil:
			findings = appendError(findings, "manifest.requires", err)
		case developmentBuild:
			notices = append(notices, developmentNotice)
		case !satisfied:
			findings = appendError(findings, "manifest.requires", fmt.Errorf(
				"CLI %s does not satisfy %s",
				options.Version,
				requirement,
			))
		}
	} else if errors.Is(requirementErr, manifest.ErrInvalidRequirement) {
		findings = appendError(findings, "manifest.requires", requirementErr)
	}

	loaded, loadErr := manifest.Load(options.Repository)
	if loadErr != nil {
		requirementAlreadyReported := requirementErr != nil &&
			errors.Is(requirementErr, manifest.ErrInvalidRequirement) &&
			errors.Is(loadErr, manifest.ErrInvalidRequirement)
		if !requirementAlreadyReported {
			findings = appendError(findings, "manifest.load", loadErr)
		}
	} else if requirementErr != nil {
		// 两次只读检查通常会同样成功或失败；若外部状态在两者之间变化，仍保留预读错误。
		findings = appendError(findings, "manifest.load", requirementErr)
	}
	if loadErr == nil {
		findings = checkRepository(findings, loaded, options)
	}

	tracked, err := trackedLocalFiles(ctx, options.Repository, options.Home)
	if err != nil {
		findings = appendError(findings, "git.index", err)
	} else {
		for _, path := range tracked {
			findings = append(findings, Finding{
				Severity: SeverityError,
				Check:    "git.tracked-local",
				Message:  fmt.Sprintf("tracked machine-local file %q", path),
			})
		}
	}

	return newResult(findings, notices)
}

func checkRepository(
	findings []Finding,
	repository manifest.Repository,
	options ManifestOptions,
) []Finding {
	if err := repository.ValidateTemplates(); err != nil {
		findings = appendError(findings, "manifest.templates", err)
	}
	if err := repository.ValidateModuleRules(options.GOOS); err != nil {
		findings = appendError(findings, "manifest.modules", err)
	}

	controlPaths, controlErr := paths.ResolveControlPlanePaths(
		options.Home,
		options.Repository,
		options.Config,
	)
	if controlErr != nil {
		findings = appendError(findings, "paths.control", controlErr)
	} else if _, err := paths.ValidatePathBoundaries(controlPaths, nil); err != nil {
		controlErr = err
		findings = appendError(findings, "paths.control", err)
	}

	profiles := repository.ProfileNames()
	if options.Profile != "" {
		profiles = []string{options.Profile}
	}
	for _, name := range profiles {
		resolved, err := repository.Resolve(name, options.GOOS)
		if err != nil {
			findings = appendError(findings, "manifest.profile", fmt.Errorf("profile %q resolve: %w", name, err))
			continue
		}
		if controlErr != nil {
			continue
		}
		if _, err := resolved.ValidatePathBoundaries(controlPaths); err != nil {
			findings = appendError(findings, "manifest.profile", err)
		}
	}
	return findings
}

func appendError(findings []Finding, check string, err error) []Finding {
	return append(findings, Finding{
		Severity: SeverityError,
		Check:    check,
		Message:  err.Error(),
	})
}

func trackedLocalFiles(ctx context.Context, repository, home string) ([]string, error) {
	command := exec.CommandContext(ctx, "git", "-C", repository, "ls-files", "-z")
	// GIT_* 可以覆盖 repository、worktree、index 与 pathspec 解释；process HOME/XDG
	// 还会让测试专用 --home 意外读取主力机器配置。Git 只查询显式 repo，并使用
	// 本次 effective HOME 发现用户级配置。
	command.Env = isolatedGitEnvironment(os.Environ(), home)
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			detail := strings.TrimSpace(string(exitErr.Stderr))
			if detail != "" {
				return nil, fmt.Errorf("query Git index for tracked *.local: %w: %s", err, detail)
			}
		}
		return nil, fmt.Errorf("query Git index for tracked *.local: %w", err)
	}
	if len(output) == 0 {
		return nil, nil
	}
	if output[len(output)-1] != 0 {
		return nil, errors.New("query Git index for tracked *.local: malformed NUL-delimited output")
	}

	parts := bytes.Split(output[:len(output)-1], []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		path := string(part)
		if strings.HasSuffix(path, ".local") {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func isolatedGitEnvironment(environment []string, home string) []string {
	isolated := make([]string, 0, len(environment)+4)
	for _, variable := range environment {
		name, _, _ := strings.Cut(variable, "=")
		if strings.HasPrefix(name, "GIT_") || isGitHomeVariable(name) {
			continue
		}
		isolated = append(isolated, variable)
	}
	isolated = append(isolated,
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
	)
	return isolated
}

func isGitHomeVariable(name string) bool {
	switch name {
	case "HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME":
		return true
	default:
		return false
	}
}
