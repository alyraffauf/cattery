package quality

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

const (
	maxFileLines        = 400
	maxFunctionLines    = 40
	maxRunELines        = 15
	maxStatements       = 25
	maxDecisions        = 10
	maxNesting          = 2
	maxParameters       = 3
	maxInterfaceMethods = 3
)

// shapeViolations scans one Go file for function and interface limit breaches.
func shapeViolations(path string) []violation {
	fileSet, file, err := parseSource(path)
	if err != nil {
		return nil
	}
	runE := runELiterals(file)
	var breaches []violation
	for _, decl := range file.Decls {
		breaches = append(breaches, declViolations(decl, fileSet, runE)...)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if literal, ok := node.(*ast.FuncLit); ok {
			breaches = append(breaches, literalViolations(literal, fileSet, runE)...)
		}
		return true
	})
	breaches = append(breaches, interfaceViolations(file, fileSet)...)
	return breaches
}

// functionContext bundles the inputs needed to measure one function so every
// helper obeys the three-parameter limit.
type functionContext struct {
	name    string
	body    *ast.BlockStmt
	params  *ast.FieldList
	node    ast.Node
	fileSet *token.FileSet
	runE    map[token.Pos]bool
}

// measurement captures the computed metrics for one function.
type measurement struct {
	length int
	limit  int
	shape  functionShape
	start  int
}

func declViolations(decl ast.Decl, fileSet *token.FileSet, runE map[token.Pos]bool) []violation {
	function, ok := decl.(*ast.FuncDecl)
	if !ok || function.Body == nil {
		return nil
	}
	context := functionContext{
		name: function.Name.Name, body: function.Body,
		params: function.Type.Params, node: function, fileSet: fileSet, runE: runE,
	}
	return functionViolations(context)
}

func literalViolations(literal *ast.FuncLit, fileSet *token.FileSet, runE map[token.Pos]bool) []violation {
	if literal.Body == nil {
		return nil
	}
	name := "func-literal"
	if runE[literal.Pos()] {
		name = "RunE-literal"
	}
	context := functionContext{
		name: name, body: literal.Body, params: literal.Type.Params,
		node: literal, fileSet: fileSet, runE: runE,
	}
	return functionViolations(context)
}

func functionViolations(context functionContext) []violation {
	return buildViolations(context, measureFunction(context))
}

func measureFunction(context functionContext) measurement {
	limit := maxFunctionLines
	if context.name == "RunE" || context.name == "RunE-literal" || context.runE[context.node.Pos()] {
		limit = maxRunELines
	}
	return measurement{
		length: lineSpan(context.node, context.fileSet),
		limit:  limit,
		shape:  measureBody(context.body),
		start:  context.fileSet.Position(context.node.Pos()).Line,
	}
}

func buildViolations(context functionContext, measured measurement) []violation {
	var breaches []violation
	if measured.length > measured.limit {
		breaches = append(breaches, violation{rule: context.name + " length", line: measured.start})
	}
	if measured.shape.statements > maxStatements {
		breaches = append(breaches, violation{rule: context.name + " statements", line: measured.start})
	}
	if measured.shape.decisions > maxDecisions {
		breaches = append(breaches, violation{rule: context.name + " decisions", line: measured.start})
	}
	if nestingDepth(context.body) > maxNesting {
		breaches = append(breaches, violation{rule: context.name + " nesting", line: measured.start})
	}
	if parameterCount(context.params) > maxParameters {
		breaches = append(breaches, violation{rule: context.name + " parameters", line: measured.start})
	}
	return breaches
}

// interfaceViolations rejects any interface whose transitive method count,
// including embedded same-package interfaces, exceeds the limit.
func interfaceViolations(file *ast.File, fileSet *token.FileSet) []violation {
	set := collectInterfaces(file)
	var breaches []violation
	for name, source := range set {
		count := countInterfaceMethods(name, set, map[string]bool{})
		if count > maxInterfaceMethods {
			breaches = append(breaches, violation{rule: "interface " + name + " methods", line: fileSet.Position(source.Pos()).Line})
		}
	}
	return breaches
}

func collectInterfaces(file *ast.File) map[string]*ast.InterfaceType {
	set := make(map[string]*ast.InterfaceType)
	for _, decl := range file.Decls {
		declaration, ok := decl.(*ast.GenDecl)
		if !ok || declaration.Tok != token.TYPE {
			continue
		}
		mergeTypeSpecs(declaration, set)
	}
	return set
}

func mergeTypeSpecs(declaration *ast.GenDecl, set map[string]*ast.InterfaceType) {
	for _, spec := range declaration.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		interfaceType, ok := typeSpec.Type.(*ast.InterfaceType)
		if !ok {
			continue
		}
		set[typeSpec.Name.Name] = interfaceType
	}
}

func countInterfaceMethods(name string, set map[string]*ast.InterfaceType, seen map[string]bool) int {
	if seen[name] {
		return 0
	}
	source, ok := set[name]
	if !ok {
		return 0
	}
	seen[name] = true
	total := 0
	for _, field := range source.Methods.List {
		total += methodContribution(field, set, seen)
	}
	return total
}

func methodContribution(field *ast.Field, set map[string]*ast.InterfaceType, seen map[string]bool) int {
	if len(field.Names) > 0 {
		return len(field.Names)
	}
	ident, ok := field.Type.(*ast.Ident)
	if !ok {
		return 1
	}
	return countInterfaceMethods(ident.Name, set, seen)
}

func TestSourceShapeChecker(t *testing.T) {
	t.Run("synthetic bad snippets", func(t *testing.T) {
		assertRuleFails(t, "length", longFunctionSource)
		assertRuleFails(t, "RunE-literal", longRunESource)
		assertRuleFails(t, "statements", manyStatementsSource)
		assertRuleFails(t, "decisions", manyDecisionsSource)
		assertRuleFails(t, "nesting", deepNestingSource)
		assertRuleFails(t, "parameters", manyParametersSource)
		assertRuleFails(t, "interface", fatInterfaceSource)
	})

	t.Run("file length limit", func(t *testing.T) {
		path := writeTempFile(t, "long.go", strings.Repeat("x()\n", maxFileLines+5))
		breaches := fileLengthViolations([]string{path})
		if len(breaches) != 1 {
			t.Fatalf("expected one file-length breach, got %d", len(breaches))
		}
	})

	t.Run("live tree is clean", func(t *testing.T) {
		liveTreeHasNoShapeOrLengthBreaches(t)
	})
}

func liveTreeHasNoShapeOrLengthBreaches(t *testing.T) {
	t.Helper()
	var breaches []violation
	for _, path := range allSources(t) {
		breaches = append(breaches, fileLengthViolations([]string{path})...)
		if filepath.Ext(path) == ".go" {
			breaches = append(breaches, shapeViolations(path)...)
		}
	}
	failOn(t, "live shape or length violations", breaches)
}
