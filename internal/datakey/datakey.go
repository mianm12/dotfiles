// Package datakey 校验 manifest 与机器配置共享的用户 data key 词法规则。
package datakey

import "regexp"

var pattern = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*$`)

// Valid 报告 key 是否以 ASCII 小写字母开头，且只包含 ASCII 字母、数字和下划线。
func Valid(key string) bool {
	return pattern.MatchString(key)
}
