package cli

import (
	"bufio"
	"bytes"
	"strings"

	"github.com/mianm12/dotfiles/internal/core/config"
)

type fileReader func(string) ([]byte, error)

func detectPlatform(goos, goarch string, readFile fileReader) config.Platform {
	platform := config.Platform{Arch: normalizeArchitecture(goarch)}
	switch goos {
	case "darwin":
		platform.OS = "macos"
	case "linux":
		platform.OS = "linux"
		if readFile != nil {
			if data, err := readFile("/etc/os-release"); err == nil {
				platform.Distro = osReleaseID(data)
			}
		}
	}
	return platform
}

func normalizeArchitecture(architecture string) string {
	switch architecture {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return strings.ToLower(architecture)
	}
}

func osReleaseID(data []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		key, value, found := strings.Cut(line, "=")
		if !found || key != "ID" {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if value == strings.ToLower(value) {
			return value
		}
		return ""
	}
	return ""
}
