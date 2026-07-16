// Package config 读取并严格校验机器本地配置。
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"

	"github.com/pelletier/go-toml/v2"
)

// dataKeyPattern 约束 [data] key 必须以小写字母开头且只含 ASCII 字母、数字和下划线。
var dataKeyPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*$`)

// Machine 表示机器本地配置。Repo 为 nil 表示字段缺失；非 nil 空字符串会被 Load 拒绝。
type Machine struct {
	Profile string            `toml:"profile"`
	Repo    *string           `toml:"repo"`
	Data    map[string]string `toml:"data"`
}

// Load 读取机器本地配置；文件不存在表示尚未初始化，是合法的空状态。
// 第二个返回值表示配置文件是否存在。
func Load(path string) (Machine, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Machine{}, false, nil
		}
		return Machine{}, false, fmt.Errorf("open machine config %q: %w", path, err)
	}

	var machine Machine
	decoder := toml.NewDecoder(file)
	decoder.DisallowUnknownFields()
	// 不使用 defer，以便报告 Close 错误；Decode 与 Close 均失败时优先返回 Decode 错误。
	decodeErr := decoder.Decode(&machine)
	closeErr := file.Close()
	if decodeErr != nil {
		return Machine{}, false, fmt.Errorf("decode machine config %q: %w", path, decodeErr)
	}
	if closeErr != nil {
		return Machine{}, false, fmt.Errorf("close machine config %q after reading: %w", path, closeErr)
	}
	if machine.Profile == "" {
		return Machine{}, false, fmt.Errorf("machine config %q: profile must be a non-empty string", path)
	}
	if machine.Repo != nil && *machine.Repo == "" {
		return Machine{}, false, fmt.Errorf("machine config %q: repo must be a non-empty string", path)
	}
	for key := range machine.Data {
		if !dataKeyPattern.MatchString(key) {
			return Machine{}, false, fmt.Errorf("machine config %q: invalid data key %q", path, key)
		}
	}

	return machine, true, nil
}
