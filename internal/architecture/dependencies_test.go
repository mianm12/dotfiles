package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var allowedInternalImports = map[string]map[string]bool{
	"internal/buildinfo":     {},
	"internal/storage":       {},
	"internal/core/paths":    {},
	"internal/core/state":    {},
	"internal/core/config":   allow("internal/core/paths"),
	"internal/core/converge": allow("internal/core/config", "internal/core/paths", "internal/core/state", "internal/storage"),
	"internal/cli":           allow("internal/buildinfo", "internal/core/config", "internal/core/converge"),
	"cmd/dot":                allow("internal/cli"),
}

var allowedThirdPartyImports = map[string]map[string]bool{
	"github.com/gofrs/flock":          allow("internal/core/converge"),
	"github.com/google/renameio/v2":   allow("internal/storage"),
	"github.com/pelletier/go-toml/v2": allow("internal/core/config"),
	"github.com/spf13/cobra":          allow("internal/cli"),
}

func TestProductionPackageDependenciesConformToArchitecture(t *testing.T) {
	root := repositoryRoot(t)
	actual := productionInternalImports(t, root)
	failures := internalDependencyFailures(actual, allowedInternalImports)
	if len(failures) != 0 {
		t.Fatalf("production dependency boundary changed:\n%s", strings.Join(failures, "\n"))
	}
}

func TestProductionThirdPartyDependenciesConformToArchitecture(t *testing.T) {
	root := repositoryRoot(t)
	actual := productionThirdPartyImports(t, root)
	failures := thirdPartyDependencyFailures(actual, allowedThirdPartyImports)
	if len(failures) != 0 {
		t.Fatalf(
			"production third-party dependency boundary changed:\n%s",
			strings.Join(failures, "\n"),
		)
	}
}

func TestDependencyPolicyAllowsRemovingImports(t *testing.T) {
	internalActual := map[string]map[string]bool{
		"internal/upper": {},
		"internal/lower": {},
	}
	internalAllowed := map[string]map[string]bool{
		"internal/upper": allow("internal/lower"),
		"internal/lower": {},
	}
	if failures := internalDependencyFailures(internalActual, internalAllowed); len(failures) != 0 {
		t.Fatalf("unused allowed internal edge failed policy: %v", failures)
	}

	thirdPartyAllowed := map[string]map[string]bool{
		"example.net/dependency": allow("internal/owner"),
	}
	if failures := thirdPartyDependencyFailures(nil, thirdPartyAllowed); len(failures) != 0 {
		t.Fatalf("unused allowed third-party import failed policy: %v", failures)
	}
}

func TestDependencyPolicyStillRejectsExpansion(t *testing.T) {
	t.Run("unknown internal package", func(t *testing.T) {
		failures := internalDependencyFailures(
			map[string]map[string]bool{
				"internal/known":   {},
				"internal/unknown": {},
			},
			map[string]map[string]bool{"internal/known": {}},
		)
		if got := strings.Join(failures, "\n"); !strings.Contains(got, "internal/unknown") {
			t.Fatalf("failures = %q, want unknown package rejection", got)
		}
	})

	t.Run("forbidden internal direction", func(t *testing.T) {
		failures := internalDependencyFailures(
			map[string]map[string]bool{
				"internal/upper": allow("internal/lower"),
				"internal/lower": {},
			},
			map[string]map[string]bool{
				"internal/upper": {},
				"internal/lower": {},
			},
		)
		if got := strings.Join(failures, "\n"); !strings.Contains(got, "forbidden internal package") {
			t.Fatalf("failures = %q, want forbidden direction rejection", got)
		}
	})

	t.Run("new third-party dependency", func(t *testing.T) {
		failures := thirdPartyDependencyFailures(
			map[string]map[string]bool{
				"example.net/new": allow("internal/owner"),
			},
			nil,
		)
		if got := strings.Join(failures, "\n"); !strings.Contains(got, "unlisted third-party package") {
			t.Fatalf("failures = %q, want new dependency rejection", got)
		}
	})

	t.Run("wrong third-party owner", func(t *testing.T) {
		failures := thirdPartyDependencyFailures(
			map[string]map[string]bool{
				"example.net/dependency": allow("internal/wrong"),
			},
			map[string]map[string]bool{
				"example.net/dependency": allow("internal/owner"),
			},
		)
		if got := strings.Join(failures, "\n"); !strings.Contains(got, "forbidden third-party package") {
			t.Fatalf("failures = %q, want owner rejection", got)
		}
	})
}

func internalDependencyFailures(
	actual map[string]map[string]bool,
	allowed map[string]map[string]bool,
) []string {
	var failures []string
	for source := range actual {
		if _, known := allowed[source]; !known {
			failures = append(failures, "production package is missing from the architecture table: "+source)
		}
	}
	for source, allowedTargets := range allowed {
		imports, exists := actual[source]
		if !exists {
			failures = append(failures, "architecture table contains no production package: "+source)
			continue
		}
		for target := range imports {
			if !allowedTargets[target] {
				failures = append(failures, source+" imports forbidden internal package "+target)
			}
		}
	}
	sort.Strings(failures)
	return failures
}

func thirdPartyDependencyFailures(
	actual map[string]map[string]bool,
	allowed map[string]map[string]bool,
) []string {
	var failures []string
	for importPath, sources := range actual {
		allowedSources, known := allowed[importPath]
		if !known {
			failures = append(
				failures,
				"production imports unlisted third-party package "+importPath,
			)
			continue
		}
		for source := range sources {
			if !allowedSources[source] {
				failures = append(
					failures,
					source+" imports forbidden third-party package "+importPath,
				)
			}
		}
	}
	sort.Strings(failures)
	return failures
}

func TestCollectInternalImportsUsesRepositoryModulePath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "go.mod"),
		[]byte("module example.com/dot\n"),
		0o600,
	); err != nil {
		t.Fatalf("write synthetic go.mod: %v", err)
	}
	source := filepath.Join(root, "sample.go")
	if err := os.WriteFile(
		source,
		[]byte("package sample\nimport _ \"example.com/dot/internal/core/paths\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write synthetic Go source: %v", err)
	}

	imports := make(map[string]bool)
	collectInternalImports(t, source, repositoryModulePath(t, root), imports)
	if !imports["internal/core/paths"] {
		t.Fatal("repository module import was not collected")
	}
}

func TestCollectThirdPartyImportsExcludesStandardAndRepositoryPackages(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "go.mod"),
		[]byte("module example.com/dot\n"),
		0o600,
	); err != nil {
		t.Fatalf("write synthetic go.mod: %v", err)
	}
	source := filepath.Join(root, "sample.go")
	if err := os.WriteFile(
		source,
		[]byte(
			"package sample\n"+
				"import (\n"+
				"\t_ \"fmt\"\n"+
				"\t_ \"example.com/dot/internal/core/paths\"\n"+
				"\t_ \"example.net/dependency/pkg\"\n"+
				")\n",
		),
		0o600,
	); err != nil {
		t.Fatalf("write synthetic Go source: %v", err)
	}

	imports := make(map[string]bool)
	collectThirdPartyImports(t, source, repositoryModulePath(t, root), imports)
	if len(imports) != 1 || !imports["example.net/dependency/pkg"] {
		t.Fatalf("third-party imports = %v, want only example.net/dependency/pkg", imports)
	}
}

func TestWalkProductionGoFilesSkipsIgnoredDirectories(t *testing.T) {
	root := t.TempDir()
	files := []string{
		"cmd/dot/main.go",
		"internal/example/example.go",
		"internal/example/testdata/fixture.go",
		"internal/example/vendor/dependency.go",
		"internal/example/.hidden/fixture.go",
		"internal/example/_fixture/fixture.go",
	}
	for _, filename := range files {
		fullPath := filepath.Join(root, filename)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
			t.Fatalf("create synthetic directory for %q: %v", filename, err)
		}
		if err := os.WriteFile(fullPath, []byte("package synthetic\n"), 0o600); err != nil {
			t.Fatalf("write synthetic Go file %q: %v", filename, err)
		}
	}

	var visited []string
	walkProductionGoFiles(t, root, func(filename, _ string) {
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			t.Fatalf("make visited filename relative: %v", err)
		}
		visited = append(visited, filepath.ToSlash(relative))
	})
	sort.Strings(visited)
	want := []string{"cmd/dot/main.go", "internal/example/example.go"}
	if !slices.Equal(visited, want) {
		t.Fatalf("visited files = %v, want %v", visited, want)
	}
}

func allow(packages ...string) map[string]bool {
	result := make(map[string]bool, len(packages))
	for _, pkg := range packages {
		result[pkg] = true
	}
	return result
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not return the test filename")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func productionInternalImports(t *testing.T, root string) map[string]map[string]bool {
	t.Helper()
	modulePath := repositoryModulePath(t, root)
	result := make(map[string]map[string]bool)
	walkProductionGoFiles(t, root, func(filename, source string) {
		if result[source] == nil {
			result[source] = make(map[string]bool)
		}
		collectInternalImports(t, filename, modulePath, result[source])
	})
	return result
}

func productionThirdPartyImports(t *testing.T, root string) map[string]map[string]bool {
	t.Helper()
	modulePath := repositoryModulePath(t, root)
	result := make(map[string]map[string]bool)
	walkProductionGoFiles(t, root, func(filename, source string) {
		imports := make(map[string]bool)
		collectThirdPartyImports(t, filename, modulePath, imports)
		for importPath := range imports {
			if result[importPath] == nil {
				result[importPath] = make(map[string]bool)
			}
			result[importPath][source] = true
		}
	})
	return result
}

func walkProductionGoFiles(
	t *testing.T,
	root string,
	visit func(filename, source string),
) {
	t.Helper()
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(
			filename string,
			entry os.DirEntry,
			walkErr error,
		) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if ignoredProductionDirectory(entry.Name()) && filename != filepath.Join(root, top) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(filename, ".go") ||
				strings.HasSuffix(filename, "_test.go") {
				return nil
			}
			visit(filename, packagePath(t, root, filepath.Dir(filename)))
			return nil
		})
		if err != nil {
			t.Fatalf("walk production packages under %q: %v", top, err)
		}
	}
}

func ignoredProductionDirectory(name string) bool {
	return name == "testdata" ||
		name == "vendor" ||
		strings.HasPrefix(name, ".") ||
		strings.HasPrefix(name, "_")
}

func packagePath(t *testing.T, root, directory string) string {
	t.Helper()
	relative, err := filepath.Rel(root, directory)
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q): %v", root, directory, err)
	}
	return filepath.ToSlash(relative)
}

func repositoryModulePath(t *testing.T, root string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read repository go.mod: %v", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], "\"`")
		}
	}
	t.Fatal("repository go.mod does not declare a module path")
	return ""
}

func collectInternalImports(
	t *testing.T,
	filename string,
	modulePath string,
	imports map[string]bool,
) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse imports from %q: %v", filename, err)
	}
	for _, spec := range file.Decls {
		declaration, ok := spec.(*ast.GenDecl)
		if !ok || declaration.Tok != token.IMPORT {
			continue
		}
		for _, item := range declaration.Specs {
			importSpec := item.(*ast.ImportSpec)
			path, unquoteErr := strconv.Unquote(importSpec.Path.Value)
			if unquoteErr != nil {
				t.Fatalf("unquote import %s in %q: %v", importSpec.Path.Value, filename, unquoteErr)
			}
			prefix := modulePath + "/"
			if strings.HasPrefix(path, prefix) {
				imports[strings.TrimPrefix(path, prefix)] = true
			}
		}
	}
}

func collectThirdPartyImports(
	t *testing.T,
	filename string,
	modulePath string,
	imports map[string]bool,
) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse imports from %q: %v", filename, err)
	}
	for _, importSpec := range file.Imports {
		importPath, err := strconv.Unquote(importSpec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %s in %q: %v", importSpec.Path.Value, filename, err)
		}
		if importPath == "C" ||
			importPath == modulePath ||
			strings.HasPrefix(importPath, modulePath+"/") {
			continue
		}
		firstElement, _, _ := strings.Cut(importPath, "/")
		if strings.Contains(firstElement, ".") {
			imports[importPath] = true
		}
	}
}
