package quality

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// modulePath reads the module path from go.mod at root.
func modulePath(root string) string {
	bytes, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(bytes), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(trim, "module "))
		}
	}
	return ""
}

// allowedFamilies is the Section 12.5 internal import allowlist. Families not
// present may import no internal package.
var allowedFamilies = map[string][]string{
	"routes":                 {"deployment", "pathsafe"},
	"state":                  {"deployment", "pathsafe"},
	"secrets":                {"failure", "subprocess"},
	"hooks":                  {"deployment", "subprocess"},
	"filesystem":             {"deployment", "pathsafe"},
	"repository":             {"deployment", "hooks", "pathsafe", "routes"},
	"reconcile":              {"deployment", "pathsafe", "state", "secrets"},
	"diff":                   {"deployment", "reconcile"},
	"selection":              {"deployment", "pathsafe", "state"},
	"application/initialize": {"failure", "pathsafe", "state"},
	"application/validate":   {"deployment", "failure", "repository", "selection"},
	"application/evaluation": {"deployment", "failure", "reconcile", "repository", "secrets", "selection", "state"},
	"application/inspect":    {"application/evaluation", "deployment", "diff", "failure", "reconcile", "repository", "secrets", "selection", "state"},
	"application/apply":      {"application/evaluation", "application/outcome", "deployment", "diff", "failure", "filesystem", "hooks", "pathsafe", "reconcile", "repository", "secrets", "selection", "state"},
	"application/add":        {"application/outcome", "deployment", "failure", "filesystem", "pathsafe", "reconcile", "repository", "secrets", "selection", "state"},
	"application/outcome":    {},
	"application/version":    {"buildinfo"},
	"bootstrap":              {"application/initialize", "application/validate", "application/inspect", "application/apply", "application/add", "application/version", "cli", "deployment", "failure", "filesystem", "hooks", "repository", "secrets", "selection", "state"},
	"cmd/cattery":            {"bootstrap", "cli", "failure"},
}

// familyFromRelative maps an internal relative path to its DAG family key.
func familyFromRelative(relative string) string {
	parts := strings.Split(relative, "/")
	if len(parts) >= 3 && parts[0] == "internal" && (parts[1] == "application" || parts[1] == "testfixture") {
		return parts[1] + "/" + parts[2]
	}
	if len(parts) >= 2 && parts[0] == "internal" {
		return parts[1]
	}
	if len(parts) >= 2 && parts[0] == "cmd" {
		return "cmd/" + parts[1]
	}
	return relative
}

func familyOfFile(root, path string) string {
	relative := fileRelativePath(root, path)
	return familyFromRelative(filepath.Dir(relative))
}

func fileRelativePath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return relative
}

// isAllowed reports whether candidate may be imported by family. cli is
// special: it may import failure and any application family.
func isAllowed(family, candidate string) bool {
	if family == "cli" {
		return candidate == "failure" || strings.HasPrefix(candidate, "application/")
	}
	for _, allowed := range allowedFamilies[family] {
		if allowed == candidate {
			return true
		}
	}
	return false
}

// cliForbiddenImports are backend packages no CLI file may import.
var cliForbiddenImports = map[string]bool{
	"selection": true, "repository": true, "state": true, "reconcile": true,
	"diff": true, "filesystem": true, "hooks": true, "secrets": true,
}

func importFamily(importPath, module string) (string, bool) {
	if !strings.HasPrefix(importPath, module+"/") {
		return "", false
	}
	return familyFromRelative(strings.TrimPrefix(importPath, module+"/")), true
}

// dagViolations checks the internal import DAG and cli purity beneath root.
func dagViolations(root, module string) []violation {
	var breaches []violation
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return skipDirectory(path)
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		breaches = append(breaches, fileDagViolations(root, module, path)...)
		return nil
	})
	if err != nil {
		return nil
	}
	return breaches
}

func fileDagViolations(root, module, path string) []violation {
	_, file, err := parseSource(path)
	if err != nil {
		return nil
	}
	family := familyOfFile(root, path)
	if family == "quality" || strings.HasPrefix(family, "testfixture") || family == "integration" {
		return nil
	}
	context := edgeContext{family: family, module: module, path: path}
	var breaches []violation
	testFile := strings.HasSuffix(path, "_test.go")
	for _, spec := range file.Imports {
		breaches = append(breaches, importEdgeViolations(context, spec, testFile)...)
	}
	breaches = append(breaches, cliFilePurity(family, file, path)...)
	return breaches
}

type edgeContext struct {
	family string
	module string
	path   string
}

func importEdgeViolations(context edgeContext, spec *ast.ImportSpec, testFile bool) []violation {
	candidate, internal := importFamily(strings.Trim(spec.Path.Value, "\""), context.module)
	if !internal || isAllowed(context.family, candidate) {
		return nil
	}
	if testFile && strings.HasPrefix(candidate, "testfixture/") {
		return nil
	}
	return []violation{{file: context.path, rule: context.family + " forbidden import " + candidate}}
}

func cliFilePurity(family string, file *ast.File, path string) []violation {
	if family != "cli" {
		return nil
	}
	var breaches []violation
	for _, spec := range file.Imports {
		tail := lastSegment(strings.Trim(spec.Path.Value, "\""))
		if cliForbiddenImports[tail] {
			breaches = append(breaches, violation{file: path, rule: "cli backend import " + tail})
		}
	}
	return breaches
}

func lastSegment(importPath string) string {
	parts := strings.Split(importPath, "/")
	return parts[len(parts)-1]
}

type edgeScenario struct {
	name      string
	importing string
	candidate string
}

func TestArchitectureChecker(t *testing.T) {
	t.Run("synthetic forbidden edges", func(t *testing.T) {
		scenarios := []edgeScenario{
			{"state imports cli", "state", "cli"},
			{"deployment imports repository", "deployment", "repository"},
			{"cli imports state backend", "cli", "state"},
			{"application imports cli upward", "application/version", "cli"},
			{"repository imports filesystem fixture", "repository", "testfixture/filesystem"},
		}
		for _, scenario := range scenarios {
			t.Run(scenario.name, func(t *testing.T) { assertEdgeScenario(t, scenario) })
		}
	})

	t.Run("live DAG is clean", func(t *testing.T) {
		root := repositoryRoot(t)
		failOn(t, "live DAG violations", dagViolations(root, modulePath(root)))
	})

	t.Run("test files may import narrow fixtures", func(t *testing.T) {
		const module = "example.com/test"
		root := t.TempDir()
		writeModule(t, root, module)
		directory := filepath.Join(root, "internal", "repository")
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		source := "package repository\nimport _ \"" + module + "/internal/testfixture/filesystem\"\n"
		path := filepath.Join(directory, "f_test.go")
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		if violations := dagViolations(root, module); len(violations) != 0 {
			t.Fatalf("expected no DAG violation for a test-file fixture import, got %v", violations)
		}
	})
}

func assertEdgeScenario(t *testing.T, scenario edgeScenario) {
	t.Helper()
	const module = "example.com/test"
	root := t.TempDir()
	writeModule(t, root, module)
	directory := filepath.Join(root, "internal", scenario.importing)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	source := "package " + lastSegment(scenario.importing) + "\nimport _ \"" + module + "/internal/" + scenario.candidate + "\"\n"
	if err := os.WriteFile(filepath.Join(directory, "f.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(dagViolations(root, module)) == 0 {
		t.Fatalf("expected a DAG violation for %s", scenario.name)
	}
}

func writeModule(t *testing.T, root, module string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module "+module+"\n\ngo 1.25.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
