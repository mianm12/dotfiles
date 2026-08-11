package config_test

import (
	"errors"
	"path/filepath"
	"testing"

	coreconfig "github.com/mianm12/dotfiles/internal/core/config"
)

func TestMarshalMachineRejectsInvalidUTF8Repository(t *testing.T) {
	repository := filepath.Join(
		string(filepath.Separator),
		"repository-"+string([]byte{0xff}),
	)
	data, err := coreconfig.MarshalMachine(coreconfig.Machine{
		Version:    1,
		Repository: repository,
	})
	if data != nil || !errors.Is(err, coreconfig.ErrInvalidConfiguration) {
		t.Fatalf(
			"MarshalMachine(invalid UTF-8) = (%q, %v), want ErrInvalidConfiguration",
			data,
			err,
		)
	}
}
