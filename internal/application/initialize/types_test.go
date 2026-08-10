package initialize

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/state"
)

func TestInitializeContract(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"zero request selects the working directory", testContractZeroRequest},
		{"result carries the registered repository", testContractResultShape},
		{"dependencies carry home and store", testContractDependencyShape},
		{"service exposes one initialize method", testContractServiceSignature},
		{"no cli or third-party imports", testContractNoCLIImports},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testContractZeroRequest(t *testing.T) {
	var request Request
	if request.Path != "" {
		t.Fatalf("zero Request.Path = %q, want the empty working-directory default", request.Path)
	}
}

func testContractResultShape(t *testing.T) {
	field, found := reflect.TypeOf(Result{}).FieldByName("Repository")
	if !found {
		t.Fatal("Result.Repository field missing")
	}
	if field.Type != reflect.TypeOf(RegisteredRepository{}) {
		t.Fatalf("Result.Repository type = %v, want initialize.RegisteredRepository", field.Type)
	}
}

func testContractDependencyShape(t *testing.T) {
	dependencies := reflect.TypeOf(Dependencies{})
	fields := map[string]reflect.Type{
		"Home":  reflect.TypeOf(""),
		"Store": reflect.TypeOf((*state.Store)(nil)),
	}
	if dependencies.NumField() != len(fields) {
		t.Fatalf("Dependencies has %d fields, want %d", dependencies.NumField(), len(fields))
	}
	for name, want := range fields {
		field, found := dependencies.FieldByName(name)
		if !found {
			t.Fatalf("Dependencies.%s missing", name)
		}
		if field.Type != want {
			t.Fatalf("Dependencies.%s type = %v, want %v", name, field.Type, want)
		}
	}
}

func testContractServiceSignature(t *testing.T) {
	method, found := reflect.TypeOf((*Service)(nil)).MethodByName("Initialize")
	if !found {
		t.Fatal("Service.Initialize method missing")
	}
	signature := method.Type
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if signature.NumIn() != 3 ||
		signature.In(1) != contextType ||
		signature.In(2) != reflect.TypeOf(Request{}) {
		t.Fatalf("Initialize parameters = %v, want (context.Context, Request)", signature)
	}
	if signature.NumOut() != 2 ||
		signature.Out(0) != reflect.TypeOf(Result{}) ||
		signature.Out(1) != errorType {
		t.Fatalf("Initialize results = %v, want (Result, error)", signature)
	}
}

func testContractNoCLIImports(t *testing.T) {
	for _, name := range packageSources(t) {
		assertCleanImports(t, name)
	}
}

func assertCleanImports(t *testing.T, name string) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, name, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	for _, spec := range file.Imports {
		if isForbiddenImport(strings.Trim(spec.Path.Value, `"`)) {
			t.Fatalf("%s imports %q", name, spec.Path.Value)
		}
	}
}

func packageSources(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			sources = append(sources, name)
		}
	}
	return sources
}

func isForbiddenImport(path string) bool {
	return strings.HasPrefix(path, "github.com/spf13/cobra") ||
		strings.HasPrefix(path, "github.com/spf13/pflag") ||
		strings.HasSuffix(path, "/internal/cli")
}
