// Package config 负责机器本地配置的严格解码和上下文无关校验。
package config

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"

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

// Precondition 密封一次机器配置读取所依据的对象证据。
// kind 使用叶子 Lstat 类型，mode 与 bytes 来自实际严格解码的打开对象。
type Precondition struct {
	valid  bool
	exists bool
	kind   fs.FileMode
	mode   fs.FileMode
	bytes  []byte
}

// Exists 报告准备时配置对象是否存在。
func (precondition Precondition) Exists() bool { return precondition.valid && precondition.exists }

// Kind 返回准备时配置叶子的文件类型位。
func (precondition Precondition) Kind() fs.FileMode { return precondition.kind }

// Mode 返回参与配置决策的普通权限位。
func (precondition Precondition) Mode() fs.FileMode { return precondition.mode }

// Bytes 返回准备时严格解码的配置字节副本。
func (precondition Precondition) Bytes() []byte { return append([]byte(nil), precondition.bytes...) }

// Snapshot 是 strict machine config 及其提交前提的不可变快照。
type Snapshot struct {
	valid        bool
	exists       bool
	profile      string
	repo         string
	repoSet      bool
	data         map[string]string
	precondition Precondition
}

// Exists 报告配置是否存在。
func (snapshot Snapshot) Exists() bool { return snapshot.valid && snapshot.exists }

// Profile 返回已有 profile；配置缺失时为空。
func (snapshot Snapshot) Profile() string { return snapshot.profile }

// Repo 返回已有 repo；字段省略时 ok 为 false。
func (snapshot Snapshot) Repo() (value string, ok bool) { return snapshot.repo, snapshot.repoSet }

// Data 返回已有 machine data 的独立副本。
func (snapshot Snapshot) Data() map[string]string { return cloneData(snapshot.data) }

// Precondition 返回本快照密封的提交前提。
func (snapshot Snapshot) Precondition() Precondition { return clonePrecondition(snapshot.precondition) }

// Machine 返回兼容旧调用方的完整独立副本。
func (snapshot Snapshot) Machine() Machine { return snapshot.machine() }

// Load 读取并严格解码机器本地配置；文件不存在表示尚未初始化，是合法的空状态。
// err == nil 时，第二个返回值表示配置文件是否存在。
// 依赖运行上下文的路径校验不属于 Load 的职责。
func Load(path string) (Machine, bool, error) {
	snapshot, err := LoadSnapshot(path)
	if err != nil {
		return Machine{}, false, err
	}
	if !snapshot.Exists() {
		return Machine{}, false, nil
	}
	return snapshot.machine(), true, nil
}

// LoadSnapshot 严格读取机器配置，并保留同一次读取的对象 kind、bytes 与 mode 证据。
// 文件缺失形成有效 missing Precondition；悬空 symlink 等已有叶子读取失败仍返回错误。
func LoadSnapshot(path string) (Snapshot, error) {
	leafBefore, err := os.Lstat(path)
	if err != nil {
		if paths.IsMissing(path, err) {
			return Snapshot{
				valid:        true,
				data:         map[string]string{},
				precondition: Precondition{valid: true},
			}, nil
		}
		return Snapshot{}, fmt.Errorf("inspect machine config %q: %w", path, err)
	}

	file, err := os.Open(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open machine config %q: %w", path, err)
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return Snapshot{}, fmt.Errorf("inspect opened machine config %q: %w", path, statErr)
	}
	raw, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return Snapshot{}, fmt.Errorf("read machine config %q: %w", path, readErr)
	}
	if closeErr != nil {
		return Snapshot{}, fmt.Errorf("close machine config %q after reading: %w", path, closeErr)
	}
	leafAfter, err := os.Lstat(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("reinspect machine config %q: %w", path, err)
	}
	if leafBefore.Mode().Type() != leafAfter.Mode().Type() {
		return Snapshot{}, fmt.Errorf("machine config %q changed kind while reading", path)
	}

	var machine Machine
	decoder := toml.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&machine); err != nil {
		return Snapshot{}, fmt.Errorf("decode machine config %q: %w", path, err)
	}
	if err := validateMachine(machine); err != nil {
		return Snapshot{}, fmt.Errorf("machine config %q: %w", path, err)
	}

	snapshot := Snapshot{
		valid:   true,
		exists:  true,
		profile: machine.Profile,
		data:    cloneData(machine.Data),
		precondition: Precondition{
			valid:  true,
			exists: true,
			kind:   leafAfter.Mode().Type(),
			mode:   openedInfo.Mode().Perm(),
			bytes:  append([]byte(nil), raw...),
		},
	}
	if machine.Repo != nil {
		snapshot.repo = *machine.Repo
		snapshot.repoSet = true
	}
	return snapshot, nil
}

func (snapshot Snapshot) machine() Machine {
	machine := Machine{Profile: snapshot.profile, Data: cloneData(snapshot.data)}
	if snapshot.repoSet {
		repo := snapshot.repo
		machine.Repo = &repo
	}
	return machine
}

func cloneData(data map[string]string) map[string]string {
	cloned := make(map[string]string, len(data))
	for key, value := range data {
		cloned[key] = value
	}
	return cloned
}

func clonePrecondition(precondition Precondition) Precondition {
	precondition.bytes = append([]byte(nil), precondition.bytes...)
	return precondition
}
