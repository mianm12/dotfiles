// Package doctor 聚合只读诊断，并把 findings 映射为稳定退出码。
package doctor

import (
	"cmp"
	"slices"
)

// Severity 表示 finding 对命令结果的影响。
type Severity string

const (
	// SeverityError 表示静态配置无法被信任。
	SeverityError Severity = "error"
	// SeverityWarning 表示检查完成但存在需关注的问题。
	SeverityWarning Severity = "warning"
)

// Finding 是一个可稳定排序的诊断结果。
type Finding struct {
	Severity Severity
	Check    string
	Message  string
}

// Result 保存已排序的 findings 与不影响退出码的 notices。
type Result struct {
	findings []Finding
	notices  []string
}

func newResult(findings []Finding, notices []string) Result {
	findings = append([]Finding(nil), findings...)
	slices.SortFunc(findings, func(left, right Finding) int {
		if order := severityOrder(left.Severity) - severityOrder(right.Severity); order != 0 {
			return order
		}
		if order := cmp.Compare(left.Check, right.Check); order != 0 {
			return order
		}
		return cmp.Compare(left.Message, right.Message)
	})
	notices = append([]string(nil), notices...)
	slices.Sort(notices)
	return Result{findings: findings, notices: notices}
}

func severityOrder(severity Severity) int {
	if severity == SeverityError {
		return 0
	}
	return 1
}

// Findings 返回按 severity、check 和 message 排序的副本。
func (r Result) Findings() []Finding {
	return append([]Finding(nil), r.findings...)
}

// Notices 返回不影响退出码的稳定排序副本。
func (r Result) Notices() []string {
	return append([]string(nil), r.notices...)
}

// ExitCode 按 error、warning、clean 的优先级返回 1、2、0。
func (r Result) ExitCode() int {
	hasWarning := false
	for _, finding := range r.findings {
		switch finding.Severity {
		case SeverityError:
			return 1
		case SeverityWarning:
			hasWarning = true
		}
	}
	if hasWarning {
		return 2
	}
	return 0
}
