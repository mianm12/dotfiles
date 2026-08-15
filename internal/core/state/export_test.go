package state

// Decode exposes the pure decoder to black-box and fuzz tests without adding a
// test-only entry point to the production package.
func Decode(data []byte, expectedHome string) (Snapshot, error) {
	return decode(data, expectedHome)
}
