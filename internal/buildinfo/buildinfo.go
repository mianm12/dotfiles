package buildinfo

// These values are replaced by release builds through -ldflags -X.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Info identifies the running dot binary.
type Info struct {
	Version   string
	Commit    string
	BuildTime string
}

// Current returns normalized build metadata.
func Current() Info {
	return Info{
		Version:   valueOr(Version, "dev"),
		Commit:    valueOr(Commit, "unknown"),
		BuildTime: valueOr(BuildTime, "unknown"),
	}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
