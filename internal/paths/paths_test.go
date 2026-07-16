package paths

import (
	"errors"
	"path/filepath"
	"strings"
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

func TestRepository_SelectionPriority(t *testing.T) {
	home := "/home/example"
	configured := "~/configured"

	// 每个用例保留可用的低优先级来源，以证明 flag > environment > config > default。
	tests := []struct {
		name      string
		flagValue string
		flagSet   bool
		envValue  string
		envSet    bool
		config    *string
		want      string
	}{
		{
			name:      "flag",
			flagValue: "~/flag",
			flagSet:   true,
			envValue:  "~/environment",
			envSet:    true,
			config:    &configured,
			want:      "/home/example/flag",
		},
		{
			name:     "environment",
			envValue: "~/environment",
			envSet:   true,
			config:   &configured,
			want:     "/home/example/environment",
		},
		{name: "config", config: &configured, want: "/home/example/configured"},
		{name: "default", want: "/home/example/.local/share/dot/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Repository(home, tt.flagValue, tt.flagSet, func(name string) (string, bool) {
				if name != RepoEnvironment {
					t.Fatalf("Repository() environment lookup = %q, want %q", name, RepoEnvironment)
				}
				return tt.envValue, tt.envSet
			}, tt.config)
			if err != nil {
				t.Fatalf("Repository() error = %v, want nil", err)
			}
			if got != tt.want {
				t.Fatalf("Repository() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRepository_RejectsInvalidSelectedPath(t *testing.T) {
	home := "/home/example"
	validConfigured := "~/configured"
	invalidConfigured := "relative/configured"

	// 较低优先级来源保持有效，以证明非法的已选来源不会被静默跳过。
	tests := []struct {
		name       string
		flagValue  string
		flagSet    bool
		envValue   string
		envSet     bool
		configured *string
		wantSource string
	}{
		{
			name:       "empty flag",
			flagSet:    true,
			envValue:   "~/environment",
			envSet:     true,
			configured: &validConfigured,
			wantSource: "--repo",
		},
		{name: "empty environment", envSet: true, configured: &validConfigured, wantSource: RepoEnvironment},
		{name: "relative machine config", configured: &invalidConfigured, wantSource: "machine config repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Repository(home, tt.flagValue, tt.flagSet, func(name string) (string, bool) {
				if name != RepoEnvironment {
					t.Fatalf("Repository() environment lookup = %q, want %q", name, RepoEnvironment)
				}
				return tt.envValue, tt.envSet
			}, tt.configured)
			if err == nil {
				t.Fatal("Repository() error = nil, want invalid selected path error")
			}
			if !strings.Contains(err.Error(), tt.wantSource) {
				t.Errorf("Repository() error = %q, want source %q", err, tt.wantSource)
			}
		})
	}
}

func TestConfig(t *testing.T) {
	home := "/home/example"
	tests := []struct {
		name     string
		envValue string
		envSet   bool
		want     string
	}{
		{
			name:     "environment override",
			envValue: "~/dot/config.toml",
			envSet:   true,
			want:     "/home/example/dot/config.toml",
		},
		{name: "default", want: "/home/example/.config/dot/config.toml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Config(home, func(name string) (string, bool) {
				if name != ConfigEnvironment {
					t.Fatalf("Config() environment lookup = %q, want %q", name, ConfigEnvironment)
				}
				return tt.envValue, tt.envSet
			})
			if err != nil {
				t.Fatalf("Config() error = %v, want nil", err)
			}
			if got != tt.want {
				t.Errorf("Config() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfig_RejectsExplicitEmptyEnvironment(t *testing.T) {
	_, err := Config("/home/example", func(name string) (string, bool) {
		return "", name == ConfigEnvironment
	})
	if err == nil {
		t.Fatal("Config() error = nil, want explicit empty path error")
	}
}
