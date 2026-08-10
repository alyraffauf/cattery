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

func skipDirectory(path string) error {
	switch filepath.Base(path) {
	case ".git", ".direnv", "vendor", "node_modules":
		return filepath.SkipDir
	}
	return nil
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
