package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mianm12/dotfiles/internal/manifest"
)

const developmentNotice = "development build skipped the requires version comparison"

// ManifestOptions 是 manifest-only 静态检查无需 machine config 即可确定的输入。
type ManifestOptions struct {
	Repository string
	Version    string
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

	_, loadErr := manifest.Load(options.Repository)
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

	tracked, err := trackedLocalFiles(ctx, options.Repository)
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

func appendError(findings []Finding, check string, err error) []Finding {
	return append(findings, Finding{
		Severity: SeverityError,
		Check:    check,
		Message:  err.Error(),
	})
}

func trackedLocalFiles(ctx context.Context, repository string) ([]string, error) {
	command := exec.CommandContext(ctx, "git", "-C", repository, "ls-files", "-z", "--", "*.local")
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
		paths = append(paths, string(part))
	}
	return paths, nil
}
