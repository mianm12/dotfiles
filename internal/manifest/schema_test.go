package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeRootManifest(t *testing.T) {
	path := writeManifest(t, `
requires = ">=0.3.0"

[defaults]
os = ["darwin", "linux"]
target = "~"

[ignore]
patterns = ["README.md"]

[profiles]
base = ["zsh"]

[data.email]
prompt = "Email"
default = "me@example.com"
`)

	got, err := decodeRootManifest(path)
	if err != nil {
		t.Fatalf("decodeRootManifest() error = %v, want nil", err)
	}
	if got.requirement.String() != ">=0.3.0" {
		t.Errorf("requirement = %q, want %q", got.requirement, ">=0.3.0")
	}
	if !got.defaults.os.set || strings.Join(got.defaults.os.value, ",") != "darwin,linux" {
		t.Errorf("defaults.os = %#v, want explicit darwin,linux", got.defaults.os)
	}
	if !got.defaults.target.set || got.defaults.target.value.common == nil || *got.defaults.target.value.common != "~" {
		t.Errorf("defaults.target = %#v, want common ~", got.defaults.target)
	}
	if strings.Join(got.ignore, ",") != "README.md" {
		t.Errorf("ignore = %v, want [README.md]", got.ignore)
	}
	if strings.Join(got.profiles["base"], ",") != "zsh" {
		t.Errorf("profiles.base = %v, want [zsh]", got.profiles["base"])
	}
	if got.data["email"].defaultValue == nil || *got.data["email"].defaultValue != "me@example.com" {
		t.Errorf("data.email.default = %#v, want me@example.com", got.data["email"].defaultValue)
	}
}

func TestDecodeRootManifest_RejectsInvalidSchema(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "missing requires", content: "[profiles]\nbase = []", want: "requires is missing"},
		{name: "invalid requires", content: "requires = \"^1.0.0\"\n[profiles]\nbase = []", want: "invalid requires"},
		{name: "missing profiles", content: `requires = ">=1.0.0"`, want: "profiles must declare"},
		{name: "empty profiles", content: "requires = \">=1.0.0\"\n[profiles]", want: "profiles must declare"},
		{name: "unknown root", content: "requires = \">=1.0.0\"\nunknown = true\n[profiles]\nbase = []", want: "strict mode"},
		{name: "unknown defaults", content: "requires = \">=1.0.0\"\n[defaults]\nunknown = true\n[profiles]\nbase = []", want: "strict mode"},
		{name: "wrong profile type", content: "requires = \">=1.0.0\"\n[profiles]\nbase = 1", want: "cannot decode"},
		{name: "invalid data key", content: "requires = \">=1.0.0\"\n[profiles]\nbase = []\n[data.Upper]\nprompt = \"x\"", want: "invalid data key"},
		{name: "unknown data field", content: "requires = \">=1.0.0\"\n[profiles]\nbase = []\n[data.email]\nunknown = \"x\"", want: "strict mode"},
		{name: "from env", content: "requires = \">=1.0.0\"\n[profiles]\nbase = []\n[data.email]\nfrom_env = \"EMAIL\"", want: "requires M2"},
		{name: "invalid defaults os", content: "requires = \">=1.0.0\"\n[defaults]\nos = [\"freebsd\"]\n[profiles]\nbase = []", want: "unsupported OS"},
		{name: "duplicate defaults os", content: "requires = \">=1.0.0\"\n[defaults]\nos = [\"darwin\", \"darwin\"]\n[profiles]\nbase = []", want: "duplicate OS"},
		{name: "wrong target type", content: "requires = \">=1.0.0\"\n[defaults]\ntarget = 1\n[profiles]\nbase = []", want: "string or OS table"},
		{name: "empty target table", content: "requires = \">=1.0.0\"\n[defaults.target]\n[profiles]\nbase = []", want: "must contain darwin or linux"},
		{name: "unknown target os", content: "requires = \">=1.0.0\"\n[defaults.target]\nfreebsd = \"~\"\n[profiles]\nbase = []", want: "unsupported OS"},
		{name: "non-string target", content: "requires = \">=1.0.0\"\n[defaults.target]\ndarwin = 1\n[profiles]\nbase = []", want: "must be a string"},
		{name: "non-canonical target", content: "requires = \">=1.0.0\"\n[defaults]\ntarget = \"~/a/../b\"\n[profiles]\nbase = []", want: "canonical"},
		{name: "target environment variable", content: "requires = \">=1.0.0\"\n[defaults]\ntarget = \"~/$HOME/app\"\n[profiles]\nbase = []", want: "canonical"},
		{name: "invalid ignore pattern", content: "requires = \">=1.0.0\"\n[ignore]\npatterns = [\"a/**b\"]\n[profiles]\nbase = []", want: "requires ** to occupy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeRootManifest(writeManifest(t, tt.content))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("decodeRootManifest() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestDecodeModuleManifest(t *testing.T) {
	path := writeManifest(t, `
os = ["darwin"]

[target]
darwin = "~/Library/Application Support/App"

[ignore]
patterns = ["*.bak"]

[files."settings.json.template"]
mode = "0600"
target = "~/.config/app/settings.json"

[files."literal.tmpl"]
kind = "link"

[hooks]
run_once = ["hooks/setup.sh"]
`)

	got, err := decodeModuleManifest(path)
	if err != nil {
		t.Fatalf("decodeModuleManifest() error = %v, want nil", err)
	}
	if !got.os.set || strings.Join(got.os.value, ",") != "darwin" {
		t.Errorf("os = %#v, want explicit darwin", got.os)
	}
	if !got.target.set || got.target.value.byOS["darwin"] != "~/Library/Application Support/App" {
		t.Errorf("target = %#v, want darwin table", got.target)
	}
	if got.files["settings.json.template"].kind != FileKindScaffold {
		t.Errorf("settings kind = %q, want scaffold", got.files["settings.json.template"].kind)
	}
	if got.files["literal.tmpl"].kind != FileKindLink {
		t.Errorf("literal kind = %q, want link", got.files["literal.tmpl"].kind)
	}
	if strings.Join(got.runOnce, ",") != "hooks/setup.sh" {
		t.Errorf("runOnce = %v, want hooks/setup.sh", got.runOnce)
	}
}

func TestDecodeModuleManifest_RejectsInvalidSchema(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "unknown root", content: `unknown = true`, want: "strict mode"},
		{name: "unknown ignore", content: "[ignore]\nunknown = true", want: "strict mode"},
		{name: "unknown file field", content: "[files.x]\nunknown = true", want: "strict mode"},
		{name: "unknown hooks field", content: "[hooks]\nunknown = []", want: "strict mode"},
		{name: "invalid os", content: `os = ["windows"]`, want: "unsupported OS"},
		{name: "duplicate os", content: `os = ["linux", "linux"]`, want: "duplicate OS"},
		{name: "invalid target path", content: `target = "relative"`, want: "canonical"},
		{name: "invalid target table key", content: "[target]\nwindows = \"~\"", want: "unsupported OS"},
		{name: "invalid mode", content: "[files.\"x.template\"]\nmode = \"644\"", want: "invalid mode"},
		{name: "mode on link", content: "[files.x]\nmode = \"0644\"", want: "not allowed for link"},
		{name: "managed kind", content: "[files.x]\nkind = \"managed\"", want: "requires M2"},
		{name: "implicit managed", content: "[files.\"x.tmpl\"]", want: "requires M2"},
		{name: "invalid kind", content: "[files.x]\nkind = \"copy\"", want: "invalid kind"},
		{name: "invalid file target", content: "[files.x]\ntarget = \"/tmp/x\"", want: "canonical"},
		{name: "file target at home", content: "[files.x]\ntarget = \"~\"", want: "true descendant of HOME"},
		{name: "file path escapes module", content: "[files.\"../../x\"]", want: "stay within the module"},
		{name: "absolute file path", content: "[files.\"/tmp/x\"]", want: "relative path"},
		{name: "duplicate normalized file path", content: "[files.x]\n[files.\"a/../x\"]", want: "duplicates normalized source"},
		{name: "root manifest override", content: "[files.\"dot.toml\"]", want: "root dot.toml"},
		{name: "root hooks override", content: "[files.\"hooks/data\"]", want: "root hooks directory"},
		{name: "git path override", content: "[files.\"nested/.git/config\"]", want: ".git path"},
		{name: "swap path override", content: "[files.\"nested/.vimrc.swp\"]", want: "*.swp path"},
		{name: "hook reference override", content: "[files.\"scripts/setup.sh\"]\n[hooks]\nrun_once = [\"scripts/setup.sh\"]", want: "hook reference"},
		{name: "invalid ignore pattern", content: "[ignore]\npatterns = [\"foo//bar\"]", want: "invalid path segment"},
		{name: "empty hook", content: "[hooks]\nrun_once = [\"\"]", want: "must not be empty"},
		{name: "hook path escapes module", content: "[hooks]\nrun_once = [\"../setup.sh\"]", want: "stay within the module"},
		{name: "duplicate normalized hook", content: "[hooks]\nrun_once = [\"setup.sh\", \"hooks/../setup.sh\"]", want: "duplicates script"},
		{name: "inline hook", content: "[hooks]\nrun_once = [{ script = \"hooks/x\" }]", want: "requires M2"},
		{name: "wrong hook type", content: "[hooks]\nrun_once = [1]", want: "must be a string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeModuleManifest(writeManifest(t, tt.content))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("decodeModuleManifest() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestValidateIgnorePattern(t *testing.T) {
	tests := []struct {
		pattern string
		valid   bool
	}{
		{pattern: "*.md", valid: true},
		{pattern: "/root/file", valid: true},
		{pattern: "cache/", valid: true},
		{pattern: "a/**/b", valid: true},
		{pattern: ""},
		{pattern: "/"},
		{pattern: "!secret"},
		{pattern: "a?b"},
		{pattern: "[ab]"},
		{pattern: `a\b`},
		{pattern: "a//b"},
		{pattern: "a/./b"},
		{pattern: "a/../b"},
		{pattern: "a/**b"},
		{pattern: "a/***/b"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			err := validateIgnorePattern(tt.pattern)
			if (err == nil) != tt.valid {
				t.Fatalf("validateIgnorePattern(%q) error = %v, valid %v", tt.pattern, err, tt.valid)
			}
		})
	}
}

func TestDecodeModuleManifest_NormalizesRelativePaths(t *testing.T) {
	path := writeManifest(t, `
[files."dir/../settings"]
[hooks]
run_once = ["hooks/../setup.sh"]
`)

	got, err := decodeModuleManifest(path)
	if err != nil {
		t.Fatalf("decodeModuleManifest() error = %v, want nil", err)
	}
	if _, exists := got.files["settings"]; !exists {
		t.Errorf("files = %v, want normalized settings key", got.files)
	}
	if strings.Join(got.runOnce, ",") != "setup.sh" {
		t.Errorf("runOnce = %v, want normalized setup.sh", got.runOnce)
	}
}

func TestDecodeModuleManifest_ExplicitFileOverridesUserIgnore(t *testing.T) {
	path := writeManifest(t, `
[ignore]
patterns = ["README.md"]
[files."README.md"]
`)

	got, err := decodeModuleManifest(path)
	if err != nil {
		t.Fatalf("decodeModuleManifest() error = %v, want nil", err)
	}
	if _, exists := got.files["README.md"]; !exists {
		t.Errorf("files = %v, want explicit README.md", got.files)
	}
}

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dot.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}
