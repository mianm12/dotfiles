package config

import (
	"fmt"

	"github.com/mianm12/dotfiles/internal/datakey"
	"github.com/pelletier/go-toml/v2"
)

// Candidate 是通过完整 machine 校验并绑定初次 Precondition 的配置提交对象。
type Candidate struct {
	valid        bool
	machine      Machine
	bytes        []byte
	precondition Precondition
}

// NewCandidate 合并后的完整 Machine 校验、确定性编码并绑定 preparation snapshot。
func NewCandidate(snapshot Snapshot, machine Machine) (Candidate, error) {
	if !snapshot.valid || !snapshot.precondition.valid {
		return Candidate{}, fmt.Errorf("machine config snapshot is invalid")
	}
	if err := validateMachine(machine); err != nil {
		return Candidate{}, fmt.Errorf("validate machine config candidate: %w", err)
	}
	machine = cloneMachine(machine)
	encoded, err := toml.Marshal(machine)
	if err != nil {
		return Candidate{}, fmt.Errorf("encode machine config candidate: %w", err)
	}
	return Candidate{
		valid:        true,
		machine:      machine,
		bytes:        append([]byte(nil), encoded...),
		precondition: clonePrecondition(snapshot.precondition),
	}, nil
}

// Machine 返回 candidate 的完整独立副本。
func (candidate Candidate) Machine() Machine { return cloneMachine(candidate.machine) }

// Bytes 返回 candidate 的确定性 TOML 字节副本。
func (candidate Candidate) Bytes() []byte { return append([]byte(nil), candidate.bytes...) }

func validateMachine(machine Machine) error {
	if machine.Profile == "" {
		return fmt.Errorf("profile must be a non-empty string")
	}
	if machine.Repo != nil && *machine.Repo == "" {
		return fmt.Errorf("repo must be a non-empty string")
	}
	for key := range machine.Data {
		if !datakey.Valid(key) {
			return fmt.Errorf("invalid data key %q", key)
		}
	}
	return nil
}

func cloneMachine(machine Machine) Machine {
	cloned := Machine{Profile: machine.Profile, Data: cloneData(machine.Data)}
	if machine.Repo != nil {
		repo := *machine.Repo
		cloned.Repo = &repo
	}
	return cloned
}
