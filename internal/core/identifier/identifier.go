// Package identifier owns the shared syntax of repository and state IDs.
package identifier

// Valid reports whether value matches the ASCII grammar "[a-z0-9][a-z0-9_-]*".
func Valid(value string) bool {
	if value == "" || !isLowerOrDigit(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if !isLowerOrDigit(character) && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func isLowerOrDigit(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= '0' && character <= '9'
}
