package quality

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// violation records one limit breach found during a scan.
type violation struct {
	file string
	line int
	rule string
}

// repositoryRoot walks up from the test working directory to the directory
// holding go.mod, the anchor for live scans.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("go.mod not found above working directory")
		}
		directory = parent
	}
}

// walkSourceFiles visits every implementation file beneath root, skipping VCS,
// Nix, and build-output directories.
func walkSourceFiles(t *testing.T, root string, visit func(string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return skipDirectory(path)
		}
		if isImplementationSource(path) {
			visit(path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func skipDirectory(path string) error {
	switch filepath.Base(path) {
	case ".git", ".direnv", "vendor", "node_modules":
		return filepath.SkipDir
	}
	return nil
}

// isImplementationSource reports whether path is a scanned source file. Prose
// documentation, go.sum, and flake.lock are excluded by Section 12.1.
func isImplementationSource(path string) bool {
	base := filepath.Base(path)
	if base == "go.sum" || base == "flake.lock" || strings.HasSuffix(path, ".md") {
		return false
	}
	if base == "justfile" || base == "Justfile" {
		return true
	}
	for _, extension := range []string{".go", ".sh", ".bash", ".py", ".nix", ".yml", ".yaml", ".sql"} {
		if strings.HasSuffix(path, extension) {
			return true
		}
	}
	return false
}

func parseSource(path string) (*token.FileSet, *ast.File, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
	return fileSet, file, err
}

// failOn fails the test when any violation is present, listing each one.
func failOn(t *testing.T, what string, violations []violation) {
	t.Helper()
	if len(violations) == 0 {
		return
	}
	var builder strings.Builder
	builder.WriteString(what + ":\n")
	for _, breach := range violations {
		builder.WriteString(breach.rule + " at " + breach.file + ":" + strconv.Itoa(breach.line) + "\n")
	}
	t.Fatal(builder.String())
}

// writeTempFile writes content beneath a unique temporary directory and returns
// its path for synthetic-rule checks.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// assertRuleFails writes source as a Go file and requires shapeViolations to
// name the expected rule at least once.
func assertRuleFails(t *testing.T, rule, source string) {
	t.Helper()
	path := writeTempFile(t, "case.go", source)
	breaches := shapeViolations(path)
	if len(breaches) == 0 {
		t.Fatalf("rule %q did not fire", rule)
	}
	if !anyRuleMatches(breaches, rule) {
		t.Fatalf("rule %q not in %v", rule, breaches)
	}
}

func anyRuleMatches(breaches []violation, rule string) bool {
	for _, breach := range breaches {
		if strings.Contains(breach.rule, rule) {
			return true
		}
	}
	return false
}

// fileLengthViolations reports implementation files exceeding the line limit.
func fileLengthViolations(paths []string) []violation {
	var breaches []violation
	for _, path := range paths {
		if count := countLines(path); count > maxFileLines {
			breaches = append(breaches, violation{rule: "file length", line: count})
		}
	}
	return breaches
}

func countLines(path string) int {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(bytes), "\n") + trailingLine(bytes)
}

func trailingLine(bytes []byte) int {
	if len(bytes) == 0 {
		return 0
	}
	if bytes[len(bytes)-1] == '\n' {
		return 0
	}
	return 1
}

// allSources collects every implementation source path beneath the repo root.
func allSources(t *testing.T) []string {
	t.Helper()
	root := repositoryRoot(t)
	var paths []string
	walkSourceFiles(t, root, func(path string) {
		paths = append(paths, path)
	})
	return paths
}
