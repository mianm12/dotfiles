package state

import (
	"errors"
)

var (
	// ErrPathValidation 表示合法 state 在当前文件系统拓扑下尚不能安全消费。
	// 它不等同于持久文档损坏；错误链会保留具体 paths cause。
	ErrPathValidation = errors.New("state target path validation failed")
	// ErrTargetIdentityConflict 表示两个不同 state key 当前解析到同一 target identity。
	ErrTargetIdentityConflict = errors.New("state target identities conflict")
)
