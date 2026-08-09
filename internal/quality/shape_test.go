package quality

import (
	"go/ast"
	"go/token"
	"strings"
)

// functionShape tallies statement and decision counts for one function body.
type functionShape struct {
	statements int
	decisions  int
}

func (shape *functionShape) observe(node ast.Node) {
	switch typed := node.(type) {
	case *ast.CaseClause:
		if len(typed.List) > 0 {
			shape.decisions++
		}
	case *ast.CommClause:
		if typed.Comm != nil {
			shape.decisions++
		}
	case *ast.BinaryExpr:
		if typed.Op == token.LAND || typed.Op == token.LOR {
			shape.decisions++
		}
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
		shape.decisions++
	}
	if _, ok := node.(ast.Stmt); ok {
		shape.statements++
	}
}

func measureBody(body *ast.BlockStmt) functionShape {
	var shape functionShape
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		shape.observe(node)
		return true
	})
	return shape
}

// nestingDepth measures the deepest stack of nested control-flow bodies. An
// else-if chain stays at the same depth rather than deepening.
func nestingDepth(body *ast.BlockStmt) int {
	deepest := 0
	for _, stmt := range body.List {
		depth := controlFlowDepth(stmt)
		if depth > deepest {
			deepest = depth
		}
	}
	return deepest
}

func controlFlowDepth(stmt ast.Stmt) int {
	bodies := controlBodies(stmt)
	if bodies == nil {
		return 0
	}
	return 1 + maxChildDepth(bodies)
}

func maxChildDepth(bodies [][]ast.Stmt) int {
	deepest := 0
	for _, body := range bodies {
		deepest = maxOf(deepest, bodyChildDepth(body))
	}
	return deepest
}

func bodyChildDepth(body []ast.Stmt) int {
	deepest := 0
	for _, child := range body {
		depth := controlFlowDepth(child)
		if depth > deepest {
			deepest = depth
		}
	}
	return deepest
}

func maxOf(first, second int) int {
	if first > second {
		return first
	}
	return second
}

func controlBodies(stmt ast.Stmt) [][]ast.Stmt {
	switch node := stmt.(type) {
	case *ast.IfStmt:
		return ifBodies(node)
	case *ast.ForStmt:
		return [][]ast.Stmt{node.Body.List}
	case *ast.RangeStmt:
		return [][]ast.Stmt{node.Body.List}
	case *ast.SwitchStmt:
		return clauseBodies(node.Body.List)
	case *ast.TypeSwitchStmt:
		return clauseBodies(node.Body.List)
	case *ast.SelectStmt:
		return clauseBodies(node.Body.List)
	}
	return nil
}

func clauseBodies(list []ast.Stmt) [][]ast.Stmt {
	var bodies [][]ast.Stmt
	for _, stmt := range list {
		if clause, ok := stmt.(*ast.CaseClause); ok {
			bodies = append(bodies, clause.Body)
		}
		if clause, ok := stmt.(*ast.CommClause); ok {
			bodies = append(bodies, clause.Body)
		}
	}
	return bodies
}

func ifBodies(node *ast.IfStmt) [][]ast.Stmt {
	bodies := [][]ast.Stmt{node.Body.List}
	branch := node.Else
	for branch != nil {
		chain, isChain := branch.(*ast.IfStmt)
		if isChain {
			bodies = append(bodies, chain.Body.List)
			branch = chain.Else
			continue
		}
		if block, isBlock := branch.(*ast.BlockStmt); isBlock {
			bodies = append(bodies, block.List)
		}
		break
	}
	return bodies
}

func parameterCount(fieldList *ast.FieldList) int {
	count := 0
	if fieldList == nil {
		return count
	}
	for _, field := range fieldList.List {
		names := len(field.Names)
		if names == 0 {
			count++
			continue
		}
		count += names
	}
	return count
}

func lineSpan(node ast.Node, fileSet *token.FileSet) int {
	start := fileSet.Position(node.Pos()).Line
	end := fileSet.Position(node.End()).Line
	return end - start + 1
}

// runELiterals marks function literals assigned to a .RunE field so the
// stricter line limit applies to them.
func runELiterals(file *ast.File) map[token.Pos]bool {
	runE := make(map[token.Pos]bool)
	for _, decl := range file.Decls {
		ast.Inspect(decl, func(node ast.Node) bool {
			recordRunEAssignment(node, runE)
			return true
		})
	}
	return runE
}

func recordRunEAssignment(node ast.Node, runE map[token.Pos]bool) {
	assign, ok := node.(*ast.AssignStmt)
	if !ok {
		return
	}
	for index, left := range assign.Lhs {
		if isRunEField(left) && index < len(assign.Rhs) {
			addRunELiteral(assign.Rhs[index], runE)
		}
	}
}

func isRunEField(left ast.Expr) bool {
	selector, ok := left.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "RunE"
}

func addRunELiteral(value ast.Expr, runE map[token.Pos]bool) {
	if literal, ok := value.(*ast.FuncLit); ok {
		runE[literal.Pos()] = true
	}
}

// Synthetic sources for the shape-rule table. Each is valid Go that exceeds one
// named limit so the checker must report it.

func repeatedStatements(count int) string {
	return "package p\nfunc f() {\n" + strings.Repeat("\tx = 1\n", count) + "}\n"
}

var longFunctionSource = "package p\nfunc long() {\n" + strings.Repeat("\tx = 1\n", maxFunctionLines+5) + "}\n"

var longRunESource = "package p\ntype command struct{ RunE func() }\n" +
	"var instance command\n" +
	"func init() { instance.RunE = func() {\n" +
	strings.Repeat("\tx = 1\n", maxRunELines+5) + "} }\n"

var manyStatementsSource = repeatedStatements(maxStatements + 5)

var manyDecisionsSource = "package p\nfunc f() {\n" + strings.Repeat("\tif true {}\n", maxDecisions+1) + "}\n"

var deepNestingSource = "package p\nfunc f() {\n\tif true {\n\t\tif true {\n\t\t\tif true {}\n\t\t}\n\t}\n}\n"

var manyParametersSource = "package p\nfunc f(a, b, c, d int) {}\n"

var fatInterfaceSource = "package p\ntype Big interface { A(); B(); C(); D() }\n"
