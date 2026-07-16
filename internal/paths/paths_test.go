package paths

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestEffectiveHome(t *testing.T) {
	tests := []struct {
		name        string
		override    string
		overrideSet bool
		userHome    string
		userErr     error
		want        string
		wantErr     bool
	}{
		{name: "explicit absolute", override: "/tmp/home/../dot-home", overrideSet: true, want: "/tmp/dot-home"},
		{name: "platform home", userHome: "/Users/example", want: "/Users/example"},
		{name: "empty override", overrideSet: true, wantErr: true},
		{name: "relative override", override: "relative", overrideSet: true, wantErr: true},
		{name: "tilde override", override: "~/test", overrideSet: true, wantErr: true},
		{name: "platform error", userErr: errors.New("lookup failed"), wantErr: true},
		{name: "relative platform home", userHome: "relative", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EffectiveHome(tt.override, tt.overrideSet, func() (string, error) {
				return tt.userHome, tt.userErr
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("EffectiveHome() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("EffectiveHome() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveControlPath(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "Users", "example")
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "absolute", value: "/tmp/repo/../dot", want: "/tmp/dot"},
		{name: "home", value: "~", want: home},
		{name: "home child", value: "~/.dot/../repo", want: filepath.Join(home, "repo")},
		{name: "empty", wantErr: true},
		{name: "relative", value: "repo", wantErr: true},
		{name: "other tilde", value: "~someone/repo", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveControlPath(tt.value, home)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveControlPath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ResolveControlPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRepositoryPriority(t *testing.T) {
	home := "/home/example"
	configured := "~/configured"

	tests := []struct {
		name      string
		flagValue string
		flagSet   bool
		envValue  string
		envSet    bool
		config    *string
		want      string
	}{
		{name: "flag", flagValue: "~/flag", flagSet: true, envValue: "~/environment", envSet: true, config: &configured, want: "/home/example/flag"},
		{name: "environment", envValue: "~/environment", envSet: true, config: &configured, want: "/home/example/environment"},
		{name: "config", config: &configured, want: "/home/example/configured"},
		{name: "default", want: "/home/example/.local/share/dot/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Repository(home, tt.flagValue, tt.flagSet, func(name string) (string, bool) {
				if name != RepoEnvironment {
					t.Fatalf("unexpected environment lookup %q", name)
				}
				return tt.envValue, tt.envSet
			}, tt.config)
			if err != nil {
				t.Fatalf("Repository() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Repository() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigRejectsExplicitEmptyEnvironment(t *testing.T) {
	_, err := Config("/home/example", func(name string) (string, bool) {
		return "", name == ConfigEnvironment
	})
	if err == nil {
		t.Fatal("Config() error = nil, want explicit empty path error")
	}
}
