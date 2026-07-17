// Package config 负责机器本地配置的严格解码和上下文无关校验。
package config

import (
	"fmt"
	"os"

	"github.com/mianm12/dotfiles/internal/datakey"
	"github.com/mianm12/dotfiles/internal/paths"
	"github.com/pelletier/go-toml/v2"
)

// Machine 表示机器本地配置。Repo 为 nil 表示字段缺失；非 nil 空字符串会被 Load 拒绝。
// Repo 保留配置原值；依赖 effective HOME 的路径解析由调用方完成。
type Machine struct {
	Profile string            `toml:"profile"`
	Repo    *string           `toml:"repo"`
	Data    map[string]string `toml:"data"`
}

// Load 读取并严格解码机器本地配置；文件不存在表示尚未初始化，是合法的空状态。
// err == nil 时，第二个返回值表示配置文件是否存在。
// 依赖运行上下文的路径校验不属于 Load 的职责。
func Load(path string) (Machine, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if paths.IsMissing(path, err) {
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
		if !datakey.Valid(key) {
			return Machine{}, false, fmt.Errorf("machine config %q: invalid data key %q", path, key)
		}
	}

	return machine, true, nil
}
