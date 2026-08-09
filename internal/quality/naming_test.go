package quality

import (
	"go/ast"
	"go/token"
	"os"
	"strings"
	"testing"
)

// The two exact package-variable exceptions permitted by Section 12.1: the
// build-info linker strings and the embedded migration SQL.
var allowedGlobalNames = map[string]bool{
	"Version":             true,
	"Commit":              true,
	"BuildTimestamp":      true,
	"initialMigrationSQL": true,
}

func forbiddenFileName(base string) bool {
	switch base {
	case "manager.go", "helpers.go", "utils.go", "common.go", "misc.go":
		return true
	}
	return false
}

func forbiddenPackageName(name string) bool {
	switch name {
	case "manager", "helpers", "utils", "common", "misc":
		return true
	}
	return false
}

// The marker fragments are built by concatenation so the checker's own source
// does not literally contain the patterns it scans for.
func suppressionMarker(line string) bool {
	return strings.Contains(line, "//"+nolintFragment) ||
		strings.Contains(line, "//"+lintIgnoreFragment) ||
		strings.Contains(line, "//"+reviveFragment)
}

func generatedMarker(line string) bool {
	return strings.Contains(line, "DO NOT "+editFragment) ||
		strings.Contains(line, "Code "+generatedFragment)
}

const (
	nolintFragment     = "nolint"
	lintIgnoreFragment = "lint:ignore"
	reviveFragment     = "revive:disable"
	editFragment       = "EDIT"
	generatedFragment  = "generated"
)

// forbiddenIdentName flags the local abbreviations Section 12.1 bans. Short
// idiomatic names remain acceptable and are not listed here.
func forbiddenIdentName(name string) bool {
	switch name {
	case "cfg", "mgr", "svc", "req", "res", "opts", "fsys", "curr", "prev":
		return true
	}
	return false
}

// namingViolations scans one Go file for the naming and structure rules.
func namingViolations(path string) []violation {
	fileSet, file, err := parseSource(path)
	if err != nil {
		return nil
	}
	checker := namingChecker{path: path, fileSet: fileSet, production: !isTestPath(path)}
	checker.scan(file)
	return checker.breaches
}

type namingChecker struct {
	path       string
	fileSet    *token.FileSet
	production bool
	breaches   []violation
}

func (checker *namingChecker) scan(file *ast.File) {
	checker.packageName(file.Name)
	for _, declaration := range file.Decls {
		checker.declaration(declaration)
	}
	for _, line := range fileLines(checker.path) {
		checker.sourceLine(line)
	}
}

func (checker *namingChecker) packageName(name *ast.Ident) {
	if name != nil && forbiddenPackageName(name.Name) {
		checker.note(name.Pos(), "forbidden package name")
	}
}

func (checker *namingChecker) declaration(declaration ast.Decl) {
	general, ok := declaration.(*ast.GenDecl)
	if ok {
		checker.genDeclaration(general)
		return
	}
	function, ok := declaration.(*ast.FuncDecl)
	if ok && checker.production && function.Name.Name == "init" {
		checker.note(function.Pos(), "init function")
	}
}

func (checker *namingChecker) genDeclaration(general *ast.GenDecl) {
	if general.Tok == token.VAR && checker.production {
		checker.varSpecs(general.Specs)
	}
	for _, spec := range general.Specs {
		if typeSpec, ok := spec.(*ast.TypeSpec); ok {
			checker.interfaceName(typeSpec)
		}
	}
}

func (checker *namingChecker) varSpecs(specs []ast.Spec) {
	for _, spec := range specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		checker.valueNames(valueSpec.Names)
	}
}

func (checker *namingChecker) valueNames(names []*ast.Ident) {
	for _, name := range names {
		if !allowedGlobalNames[name.Name] {
			checker.note(name.Pos(), "forbidden package global "+name.Name)
		}
	}
}

func (checker *namingChecker) interfaceName(spec *ast.TypeSpec) {
	if forbiddenIdentName(spec.Name.Name) {
		checker.note(spec.Name.Pos(), "abbreviated type name "+spec.Name.Name)
	}
}

func (checker *namingChecker) sourceLine(line string) {
	if suppressionMarker(line) {
		checker.noteLine(line, "suppression marker")
	}
	if generatedMarker(line) {
		checker.noteLine(line, "generated marker")
	}
}

func (checker *namingChecker) note(position token.Pos, rule string) {
	checker.breaches = append(checker.breaches, violation{
		file: checker.path, line: checker.fileSet.Position(position).Line, rule: rule,
	})
}

func (checker *namingChecker) noteLine(line, rule string) {
	checker.breaches = append(checker.breaches, violation{
		file: checker.path, rule: rule + ": " + strings.TrimSpace(line),
	})
}

func fileLines(path string) []string {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(string(bytes), "\n")
}

func isTestPath(path string) bool {
	return strings.HasSuffix(path, "_test.go")
}

func TestNamingChecker(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"synthetic bad names", testSyntheticBadNames},
		{"buildinfo globals are the only exception", testAllowedGlobals},
		{"live tree is clean", testLiveNamingClean},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testSyntheticBadNames(t *testing.T) {
	scenarios := []struct {
		rule   string
		source string
	}{
		{"forbidden package global", "package p\nvar cfg = 1\n"},
		{"forbidden package name", "package manager\nvar x = 1\n"},
		{"suppression marker", "package p " + "//" + nolintFragment + "\n"},
		{"generated marker", "package p\n// Code " + generatedFragment + ". DO NOT " + editFragment + ".\n"},
		{"init function", "package p\nfunc init() {}\n"},
	}
	for _, scenario := range scenarios {
		path := writeTempFile(t, "case.go", scenario.source)
		if !anyRuleMatches(namingViolations(path), scenario.rule) {
			t.Fatalf("rule %q did not fire", scenario.rule)
		}
	}
}

func testAllowedGlobals(t *testing.T) {
	scenarios := []string{"Version", "Commit", "BuildTimestamp", "initialMigrationSQL"}
	for _, name := range scenarios {
		path := writeTempFile(t, "case.go", "package p\nvar "+name+" = \"x\"\n")
		if anyRuleMatches(namingViolations(path), "forbidden package global") {
			t.Fatalf("allowed global %q was rejected", name)
		}
	}
}

func testLiveNamingClean(t *testing.T) {
	var breaches []violation
	for _, path := range allSources(t) {
		if isGoPath(path) {
			breaches = append(breaches, namingViolations(path)...)
		}
		if forbiddenFileName(baseName(path)) {
			breaches = append(breaches, violation{file: path, rule: "forbidden file name"})
		}
	}
	failOn(t, "live naming violations", breaches)
}

func isGoPath(path string) bool {
	return strings.HasSuffix(path, ".go")
}

func baseName(path string) string {
	return path[strings.LastIndex(path, "/")+1:]
}
