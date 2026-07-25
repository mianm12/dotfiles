package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
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
	"internal/core/config":   allow("internal/core/paths", "internal/storage"),
	"internal/lock":          allow("internal/storage"),
	"internal/core/planner":  allow("internal/core/config", "internal/core/paths", "internal/core/state"),
	"internal/core/executor": allow("internal/core/config", "internal/core/paths", "internal/core/planner", "internal/core/state", "internal/lock", "internal/storage"),
	"internal/cli":           allow("internal/buildinfo", "internal/core/config", "internal/core/executor", "internal/core/paths", "internal/core/planner", "internal/core/state", "internal/lock"),
	"cmd/dot":                allow("internal/cli"),
}

func TestProductionPackageDependenciesMatchArchitecture(t *testing.T) {
	root := repositoryRoot(t)
	actual := productionInternalImports(t, root)

	var failures []string
	for source := range actual {
		if _, known := allowedInternalImports[source]; !known {
			failures = append(failures, "production package is missing from the architecture table: "+source)
		}
	}
	for source, allowed := range allowedInternalImports {
		imports, exists := actual[source]
		if !exists {
			failures = append(failures, "architecture table contains no production package: "+source)
			continue
		}
		for target := range imports {
			if !allowed[target] {
				failures = append(failures, source+" imports forbidden internal package "+target)
			}
		}
	}
	sort.Strings(failures)
	if len(failures) != 0 {
		t.Fatalf("production dependency boundary changed:\n%s", strings.Join(failures, "\n"))
	}
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
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(
			path string,
			entry os.DirEntry,
			walkErr error,
		) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			source := packagePath(t, root, filepath.Dir(path))
			if result[source] == nil {
				result[source] = make(map[string]bool)
			}
			collectInternalImports(t, path, modulePath, result[source])
			return nil
		})
		if err != nil {
			t.Fatalf("walk production packages under %q: %v", top, err)
		}
	}
	return result
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
