package config

import (
	"fmt"
	"os"
	"regexp"

	"github.com/pelletier/go-toml/v2"
)

var dataKeyPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*$`)

// Machine is the strictly decoded machine-local configuration.
type Machine struct {
	Profile string            `toml:"profile"`
	Repo    *string           `toml:"repo"`
	Data    map[string]string `toml:"data"`
}

// Load reads a machine configuration. A missing file is a valid empty state.
func Load(path string) (Machine, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Machine{}, false, nil
		}
		return Machine{}, false, fmt.Errorf("open machine config %q: %w", path, err)
	}
	defer file.Close()

	var machine Machine
	decoder := toml.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&machine); err != nil {
		return Machine{}, false, fmt.Errorf("decode machine config %q: %w", path, err)
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
