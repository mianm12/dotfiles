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
	"internal/core/config":   allow("internal/core/paths", "internal/storage"),
	"internal/lock":          allow("internal/storage"),
	"internal/core/planner":  allow("internal/core/config", "internal/core/paths", "internal/core/state"),
	"internal/core/executor": allow("internal/core/config", "internal/core/paths", "internal/core/planner", "internal/core/state", "internal/lock", "internal/storage"),
	"internal/cli":           allow("internal/buildinfo", "internal/core/config", "internal/core/executor", "internal/core/paths", "internal/core/planner", "internal/core/state"),
	"cmd/dot":                allow("internal/cli"),
}

var allowedThirdPartyImports = map[string]map[string]bool{
	"github.com/gofrs/flock":          allow("internal/lock"),
	"github.com/google/renameio/v2":   allow("internal/storage"),
	"github.com/pelletier/go-toml/v2": allow("internal/core/config"),
	"github.com/spf13/cobra":          allow("internal/cli"),
}

type mutationReferenceOwner struct {
	source   string
	function string
	receiver string
}

var sensitiveMutationOwners = map[string]mutationReferenceOwner{
	"internal/lock.Acquire": {
		source:   "internal/core/executor",
		function: "OpenSession",
	},
	"internal/core/config.PublishMachine": {
		source:   "internal/core/executor",
		function: "PublishSelection",
		receiver: "Session",
	},
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

func TestProductionThirdPartyDependenciesMatchArchitecture(t *testing.T) {
	root := repositoryRoot(t)
	actual := productionThirdPartyImports(t, root)

	var failures []string
	for importPath, sources := range actual {
		allowedSources, known := allowedThirdPartyImports[importPath]
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
	for importPath, allowedSources := range allowedThirdPartyImports {
		sources, exists := actual[importPath]
		if !exists {
			failures = append(
				failures,
				"third-party architecture table contains unused package "+importPath,
			)
			continue
		}
		for source := range allowedSources {
			if !sources[source] {
				failures = append(
					failures,
					"third-party architecture table contains unused edge "+
						importPath+" -> "+source,
				)
			}
		}
	}

	sort.Strings(failures)
	if len(failures) != 0 {
		t.Fatalf(
			"production third-party dependency boundary changed:\n%s",
			strings.Join(failures, "\n"),
		)
	}
}

func TestProductionSensitiveMutationReferencesHaveSingleOwner(t *testing.T) {
	root := repositoryRoot(t)
	modulePath := repositoryModulePath(t, root)
	var failures []string
	observed := make(map[string]bool, len(sensitiveMutationOwners))

	walkProductionGoFiles(t, root, func(filename, source string) {
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, filename, nil, 0)
		if err != nil {
			t.Fatalf("parse production file %q: %v", filename, err)
		}
		failures = append(
			failures,
			sensitiveMutationFailures(
				t,
				fileSet,
				file,
				source,
				modulePath,
				observed,
			)...,
		)
	})
	for target := range sensitiveMutationOwners {
		if !observed[target] {
			failures = append(
				failures,
				"sensitive mutation owner table contains unused target "+target,
			)
		}
	}

	sort.Strings(failures)
	if len(failures) != 0 {
		t.Fatalf(
			"production sensitive mutation reference has wrong owner:\n%s",
			strings.Join(failures, "\n"),
		)
	}
}

func TestSensitiveMutationBoundaryRejectsIndirectReferences(t *testing.T) {
	const modulePath = "example.com/dot"
	tests := map[string]struct {
		sourcePackage string
		code          string
	}{
		"function value": {
			sourcePackage: "internal/cli",
			code: `package sample
import config "example.com/dot/internal/core/config"
func bypass() {
	publish := config.PublishMachine
	_ = publish
}
`,
		},
		"package function value": {
			sourcePackage: "internal/cli",
			code: `package sample
import config "example.com/dot/internal/core/config"
var publish = config.PublishMachine
`,
		},
		"nested function value": {
			sourcePackage: "internal/cli",
			code: `package sample
import config "example.com/dot/internal/core/config"
var publish = (&struct{ Value any }{Value: config.PublishMachine}).Value
`,
		},
		"dot import": {
			sourcePackage: "internal/cli",
			code: `package sample
import . "example.com/dot/internal/core/config"
func bypass() {
	_ = PublishMachine
}
`,
		},
		"config package wrapper": {
			sourcePackage: "internal/core/config",
			code: `package config
func bypass() {
	_ = PublishMachine
}
`,
		},
		"lock package wrapper": {
			sourcePackage: "internal/lock",
			code: `package lock
func bypass() {
	_ = Acquire
}
`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, name+".go", test.code, 0)
			if err != nil {
				t.Fatalf("parse synthetic source: %v", err)
			}
			failures := sensitiveMutationFailures(
				t,
				fileSet,
				file,
				test.sourcePackage,
				modulePath,
				make(map[string]bool),
			)
			if len(failures) == 0 {
				t.Fatal("indirect sensitive mutation reference was not rejected")
			}
		})
	}
}

func TestSensitiveMutationBoundaryIgnoresLocalShadows(t *testing.T) {
	const modulePath = "example.com/dot"
	tests := map[string]struct {
		sourcePackage string
		code          string
	}{
		"import alias": {
			sourcePackage: "internal/cli",
			code: `package sample
import config "example.com/dot/internal/core/config"
var _ config.Machine
func allowed() {
	config := struct{ PublishMachine func() }{}
	_ = config.PublishMachine
}
`,
		},
		"sensitive name": {
			sourcePackage: "internal/core/config",
			code: `package config
func allowed() {
	PublishMachine := func() {}
	PublishMachine()
}
`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, "shadow.go", test.code, 0)
			if err != nil {
				t.Fatalf("parse synthetic source: %v", err)
			}
			failures := sensitiveMutationFailures(
				t,
				fileSet,
				file,
				test.sourcePackage,
				modulePath,
				make(map[string]bool),
			)
			if len(failures) != 0 {
				t.Fatalf("local shadow produced failures: %v", failures)
			}
		})
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

func sensitiveMutationFailures(
	t *testing.T,
	fileSet *token.FileSet,
	file *ast.File,
	source string,
	modulePath string,
	observed map[string]bool,
) []string {
	t.Helper()
	imports := importedPackageNames(t, file, modulePath)
	var failures []string
	for _, importSpec := range file.Imports {
		if importSpec.Name == nil || importSpec.Name.Name != "." {
			continue
		}
		importPath, err := strconv.Unquote(importSpec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %s: %v", importSpec.Path.Value, err)
		}
		internalPath := strings.TrimPrefix(importPath, modulePath+"/")
		if !sensitiveMutationPackage(internalPath) {
			continue
		}
		failures = append(
			failures,
			fmtPosition(fileSet, importSpec.Pos())+
				": dot import of sensitive mutation package "+internalPath,
		)
	}

	recordReference := func(position token.Pos, function, receiver, target string) {
		owner, sensitive := sensitiveMutationOwners[target]
		if !sensitive {
			return
		}
		if owner.source == source &&
			owner.function == function &&
			owner.receiver == receiver {
			observed[target] = true
			return
		}
		failures = append(
			failures,
			fmtPosition(fileSet, position)+
				": forbidden reference to "+target,
		)
	}
	var inspectReferences func(ast.Node, string, string)
	inspectReferences = func(node ast.Node, function, receiver string) {
		ast.Inspect(node, func(node ast.Node) bool {
			switch expression := node.(type) {
			case *ast.SelectorExpr:
				if target, ok := importedSelectorTarget(expression, imports); ok {
					recordReference(expression.Pos(), function, receiver, target)
				} else {
					inspectReferences(expression.X, function, receiver)
				}
				return false
			case *ast.Ident:
				target, ok := localSensitiveMutationTarget(file, source, expression)
				if ok {
					recordReference(expression.Pos(), function, receiver, target)
				}
			default:
				return true
			}
			return true
		})
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok {
			inspectReferences(
				function.Body,
				function.Name.Name,
				receiverTypeName(function),
			)
			continue
		}
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, specification := range general.Specs {
			values, ok := specification.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, value := range values.Values {
				inspectReferences(value, "", "")
			}
		}
	}
	return failures
}

func sensitiveMutationPackage(packagePath string) bool {
	prefix := packagePath + "."
	for target := range sensitiveMutationOwners {
		if strings.HasPrefix(target, prefix) {
			return true
		}
	}
	return false
}

func localSensitiveMutationTarget(
	file *ast.File,
	source string,
	identifier *ast.Ident,
) (string, bool) {
	if identifier.Obj != nil &&
		file.Scope.Lookup(identifier.Name) != identifier.Obj {
		return "", false
	}
	target := source + "." + identifier.Name
	_, exists := sensitiveMutationOwners[target]
	return target, exists
}

func importedSelectorTarget(
	selector *ast.SelectorExpr,
	imports map[string]string,
) (string, bool) {
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok || qualifier.Obj != nil {
		return "", false
	}
	importPath, ok := imports[qualifier.Name]
	if !ok {
		return "", false
	}
	return importPath + "." + selector.Sel.Name, true
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
