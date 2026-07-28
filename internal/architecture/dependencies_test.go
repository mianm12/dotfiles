package architecture_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
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
	"internal/cli":           allow("internal/buildinfo", "internal/core/config", "internal/core/executor", "internal/core/paths", "internal/core/planner", "internal/core/state"),
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

func TestProductionMutationLifecycleUsesSessionOnly(t *testing.T) {
	root := repositoryRoot(t)
	modulePath := repositoryModulePath(t, root)
	var failures []string

	walkProductionGoFiles(t, root, func(filename, source string) {
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, filename, nil, 0)
		if err != nil {
			t.Fatalf("parse production file %q: %v", filename, err)
		}
		imports := importedPackageNames(t, file, modulePath)
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				receiver := receiverTypeName(declaration)
				if forbiddenMutationDeclaration(source, declaration.Name.Name, receiver) {
					failures = append(
						failures,
						fmtPosition(fileSet, declaration.Pos())+
							": forbidden mutation lifecycle declaration",
					)
				}
				ast.Inspect(declaration.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					target, ok := importedCallTarget(call, imports)
					if !ok || allowedMutationCall(source, declaration.Name.Name, receiver, target) {
						return true
					}
					if target == "internal/core/config.PublishMachine" &&
						source == "internal/cli" {
						failures = append(
							failures,
							fmtPosition(fileSet, call.Pos())+
								": CLI must publish selection through executor.Session",
						)
						return true
					}
					if target == "internal/lock.Acquire" ||
						target == "internal/core/config.PublishMachine" {
						failures = append(
							failures,
							fmtPosition(fileSet, call.Pos())+
								": forbidden direct call to "+target,
						)
					}
					return true
				})
			case *ast.GenDecl:
				if declaration.Tok != token.TYPE || source != "internal/lock" {
					continue
				}
				for _, item := range declaration.Specs {
					typeSpec := item.(*ast.TypeSpec)
					if typeSpec.Name.Name == "Guard" {
						failures = append(
							failures,
							fmtPosition(fileSet, typeSpec.Pos())+
								": forbidden mutation lifecycle declaration",
						)
					}
				}
			}
		}
	})

	sort.Strings(failures)
	if len(failures) != 0 {
		t.Fatalf("production mutation lifecycle bypasses Session:\n%s", strings.Join(failures, "\n"))
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
	walkProductionGoFiles(t, root, func(filename, source string) {
		if result[source] == nil {
			result[source] = make(map[string]bool)
		}
		collectInternalImports(t, filename, modulePath, result[source])
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
			if entry.IsDir() ||
				!strings.HasSuffix(filename, ".go") ||
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

func importedPackageNames(
	t *testing.T,
	file *ast.File,
	modulePath string,
) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, importSpec := range file.Imports {
		importPath, err := strconv.Unquote(importSpec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %s: %v", importSpec.Path.Value, err)
		}
		if !strings.HasPrefix(importPath, modulePath+"/") {
			continue
		}
		name := path.Base(importPath)
		if importSpec.Name != nil {
			name = importSpec.Name.Name
		}
		result[name] = strings.TrimPrefix(importPath, modulePath+"/")
	}
	return result
}

func importedCallTarget(
	call *ast.CallExpr,
	imports map[string]string,
) (string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	importPath, ok := imports[qualifier.Name]
	if !ok {
		return "", false
	}
	return importPath + "." + selector.Sel.Name, true
}

func allowedMutationCall(
	source, function, receiver, target string,
) bool {
	switch target {
	case "internal/lock.Acquire":
		return source == "internal/core/executor" &&
			function == "OpenSession" &&
			receiver == ""
	case "internal/core/config.PublishMachine":
		return source == "internal/core/executor" &&
			function == "PublishSelection" &&
			receiver == "Session"
	default:
		return true
	}
}

func forbiddenMutationDeclaration(
	source, function, receiver string,
) bool {
	switch source {
	case "internal/core/executor":
		return receiver == "" &&
			(function == "Run" || function == "RunWithLock")
	case "internal/cli":
		return receiver == "" && function == "withMutationLock"
	case "internal/lock":
		return receiver == "Ownership" && function == "Reuse"
	default:
		return false
	}
}

func receiverTypeName(declaration *ast.FuncDecl) string {
	if declaration.Recv == nil || len(declaration.Recv.List) == 0 {
		return ""
	}
	expression := declaration.Recv.List[0].Type
	if pointer, ok := expression.(*ast.StarExpr); ok {
		expression = pointer.X
	}
	identifier, _ := expression.(*ast.Ident)
	if identifier == nil {
		return ""
	}
	return identifier.Name
}

func fmtPosition(fileSet *token.FileSet, position token.Pos) string {
	location := fileSet.Position(position)
	return fmt.Sprintf("%s:%d", location.Filename, location.Line)
}
