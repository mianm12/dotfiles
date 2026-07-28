package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"github.com/mianm12/dotfiles/internal/core/config"
)

type fileReader func(string) ([]byte, error)

func detectPlatform(goos, goarch string, readFile fileReader) config.Platform {
	platform := config.Platform{
		OS: config.UnknownPlatformField(
			fmt.Sprintf("runtime GOOS %q is unsupported", goos),
		),
		Distro: config.UnknownPlatformField(
			"distribution is only detected on Linux",
		),
		Arch: config.UnknownPlatformField("runtime GOARCH is empty"),
	}
	if goos == "" {
		platform.OS = config.UnknownPlatformField("runtime GOOS is empty")
	}
	if goarch != "" {
		platform.Arch = config.KnownPlatformField(
			normalizeArchitecture(goarch),
		)
	}
	switch goos {
	case "darwin":
		platform.OS = config.KnownPlatformField("macos")
	case "linux":
		platform.OS = config.KnownPlatformField("linux")
		if readFile == nil {
			platform.Distro = config.UnknownPlatformField(
				"/etc/os-release reader is unavailable",
			)
			break
		}
		data, err := readFile("/etc/os-release")
		if err != nil {
			platform.Distro = config.UnknownPlatformField(
				fmt.Sprintf("read /etc/os-release: %v", err),
			)
			break
		}
		distro, err := osReleaseID(data)
		if err != nil {
			platform.Distro = config.UnknownPlatformField(
				fmt.Sprintf("parse /etc/os-release: %v", err),
			)
			break
		}
		platform.Distro = config.KnownPlatformField(distro)
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

func osReleaseID(data []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var distro string
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "ID=") {
			continue
		}
		if distro != "" {
			return "", fmt.Errorf("ID is declared more than once")
		}
		parsed, err := parseOSReleaseID(strings.TrimPrefix(line, "ID="))
		if err != nil {
			return "", err
		}
		distro = parsed
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan: %w", err)
	}
	if distro == "" {
		return "", fmt.Errorf("ID is missing")
	}
	return distro, nil
}

func parseOSReleaseID(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("ID is empty")
	}
	if value[0] == '"' || value[0] == '\'' {
		quote := value[0]
		if len(value) < 2 || value[len(value)-1] != quote {
			return "", fmt.Errorf("ID has an unmatched quote")
		}
		value = value[1 : len(value)-1]
	} else if strings.ContainsAny(value, "\"' \t") {
		return "", fmt.Errorf("ID contains unsupported quoting or whitespace")
	}
	if value == "" {
		return "", fmt.Errorf("ID is empty")
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' ||
			character == '.' ||
			character == '_' ||
			character == '-' {
			continue
		}
		return "", fmt.Errorf("ID %q must be a lowercase token", value)
	}
	return value, nil
}
