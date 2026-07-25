package state_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	corestate "github.com/mianm12/dotfiles/internal/core/state"
)

func FuzzDecode(f *testing.F) {
	home := filepath.Join(f.TempDir(), "home")
	f.Add([]byte(fmt.Sprintf(`{"version":2,"home":%q,"modules":{}}`, home)))
	f.Add([]byte(`{"version":1,"entries":{}}`))
	f.Add([]byte(`{"version":2`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := corestate.Decode(data, home)
		if err != nil {
			return
		}
		encoded, err := corestate.Marshal(decoded)
		if err != nil {
			t.Fatalf("Marshal(successful Decode()) error = %v", err)
		}
		roundTrip, err := corestate.Decode(encoded, home)
		if err != nil {
			t.Fatalf("Decode(Marshal(snapshot)) error = %v", err)
		}
		if !reflect.DeepEqual(roundTrip, decoded) {
			t.Fatalf("round trip = %#v, want %#v", roundTrip, decoded)
		}
	})
}
