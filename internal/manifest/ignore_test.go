package manifest

import "testing"

func TestIgnorePatternMatches(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		isDir   bool
		want    bool
	}{
		{name: "star matches empty", pattern: "file*.txt", path: "file.txt", want: true},
		{name: "star matches dot", pattern: "*", path: ".hidden", want: true},
		{name: "star does not cross separator", pattern: "/a*b", path: "a/x/b", want: false},
		{name: "basename at root", pattern: "*.md", path: "README.md", want: true},
		{name: "basename nested", pattern: "*.md", path: "docs/README.md", want: true},
		{name: "basename case sensitive", pattern: "*.md", path: "README.MD", want: false},
		{name: "root only at root", pattern: "/config", path: "config", want: true},
		{name: "root only not nested", pattern: "/config", path: "nested/config", want: false},
		{name: "internal slash anchors root", pattern: "a/b", path: "a/b", want: true},
		{name: "internal slash does not slide", pattern: "a/b", path: "x/a/b", want: false},
		{name: "double star matches zero segments", pattern: "a/**/b", path: "a/b", want: true},
		{name: "double star matches many segments", pattern: "a/**/b", path: "a/x/y/b", want: true},
		{name: "double star remains segment based", pattern: "a/**/b", path: "a/x/yb", want: false},
		{name: "leading double star matches root", pattern: "**/file", path: "file", want: true},
		{name: "leading double star matches nested", pattern: "**/file", path: "a/b/file", want: true},
		{name: "directory rule rejects same named file", pattern: "cache/", path: "cache", want: false},
		{name: "directory rule matches directory", pattern: "cache/", path: "cache", isDir: true, want: true},
		{name: "directory rule matches descendants", pattern: "cache/", path: "a/cache/data", want: true},
		{name: "file rule matching directory includes descendants", pattern: "build*", path: "a/build-cache/data", want: true},
		{name: "root directory includes descendants", pattern: "/cache/", path: "cache/data", want: true},
		{name: "root directory excludes nested peer", pattern: "/cache/", path: "a/cache/data", want: false},
		{name: "unicode remains exact", pattern: "配置*", path: "nested/配置文件", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern, err := parseIgnorePattern(tt.pattern)
			if err != nil {
				t.Fatalf("parseIgnorePattern(%q) error = %v, want nil", tt.pattern, err)
			}
			if got := pattern.matches(tt.path, tt.isDir); got != tt.want {
				t.Fatalf("matches(%q, %v) = %v, want %v", tt.path, tt.isDir, got, tt.want)
			}
		})
	}
}

func TestParseIgnorePatternRejectsInvalidSyntax(t *testing.T) {
	patterns := []string{
		"",
		"/",
		"//root",
		"root//",
		"foo//bar",
		"foo/./bar",
		"foo/../bar",
		"!secret",
		"file?",
		"[ab]",
		`a\b`,
		"a/**b",
		"a/b**",
		"a/***/b",
		"a/****/b",
		"nul\x00path",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			if _, err := parseIgnorePattern(pattern); err == nil {
				t.Fatalf("parseIgnorePattern(%q) error = nil, want syntax error", pattern)
			}
		})
	}
}

func TestIgnorePatternRejectsNonCanonicalMatchPath(t *testing.T) {
	pattern, err := parseIgnorePattern("**")
	if err != nil {
		t.Fatalf("parseIgnorePattern() error = %v, want nil", err)
	}
	for _, path := range []string{"", "/a", "a/", "a//b", "a/./b", "a/../b"} {
		if pattern.matches(path, true) {
			t.Errorf("matches(%q) = true, want false for non-canonical path", path)
		}
	}
}
