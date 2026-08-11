package lock

import (
	"github.com/gofrs/flock"
	"github.com/mianm12/dotfiles/internal/storage"
)

type backend interface {
	TryLock() (bool, error)
	Unlock() error
}

func newBackend(path string) backend {
	return flock.New(path, flock.SetPermissions(storage.PrivateFileMode))
}
