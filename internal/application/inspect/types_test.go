package inspect

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/secrets"
)

func TestInspectionContract(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"zero request carries no repository or groups", testContractZeroRequest},
		{"repository input carries the five raw fields", testContractRepositoryInput},
		{"dependencies carry the narrow ports", testContractDependencyShape},
		{"repository identity carries the canonical pair", testContractIdentityShape},
		{"result carries no exported surface", testContractResultShape},
		{"ports stay narrow", testContractPortShapes},
		{"service exposes one evaluate method", testContractServiceSignature},
		{"no cli or third-party imports", testContractNoCLIImports},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testContractZeroRequest(t *testing.T) {
	var request Request
	if request.Repository != (RepositoryInput{}) {
		t.Fatalf("zero Request.Repository = %+v, want the zero repository input", request.Repository)
	}
	if request.Groups != nil {
		t.Fatalf("zero Request.Groups = %v, want nil", request.Groups)
	}
}

func testContractRepositoryInput(t *testing.T) {
	repositoryInput := reflect.TypeOf(RepositoryInput{})
	want := map[string]reflect.Type{
		"RawExplicit": reflect.TypeOf(""),
		"ExplicitSet": reflect.TypeOf(false),
		"RawEnv":      reflect.TypeOf(""),
		"EnvSet":      reflect.TypeOf(false),
		"WorkingDir":  reflect.TypeOf(""),
	}
	if repositoryInput.NumField() != len(want) {
		t.Fatalf("RepositoryInput has %d fields, want %d", repositoryInput.NumField(), len(want))
	}
	for name, fieldType := range want {
		field, found := repositoryInput.FieldByName(name)
		if !found || field.Type != fieldType {
			t.Fatalf("RepositoryInput.%s type = %v, want %v", name, field.Type, fieldType)
		}
	}
}

func testContractDependencyShape(t *testing.T) {
	dependencies := reflect.TypeOf(Dependencies{})
	want := map[string]reflect.Type{
		"RepositorySource": reflect.TypeOf((*RepositorySource)(nil)).Elem(),
		"Compiler":         reflect.TypeOf((*Compiler)(nil)).Elem(),
		"State":            reflect.TypeOf((*StateReader)(nil)).Elem(),
		"Secrets":          reflect.TypeOf((*secrets.Client)(nil)),
		"ProtectedTrees":   reflect.TypeOf([]string(nil)),
		"Platform":         reflect.TypeOf(""),
	}
	if dependencies.NumField() != len(want) {
		t.Fatalf("Dependencies has %d fields, want %d", dependencies.NumField(), len(want))
	}
	for name, fieldType := range want {
		field, found := dependencies.FieldByName(name)
		if !found || field.Type != fieldType {
			t.Fatalf("Dependencies.%s type = %v, want %v", name, field.Type, fieldType)
		}
	}
}

func testContractIdentityShape(t *testing.T) {
	identity := reflect.TypeOf(RepositoryIdentity{})
	if identity.NumField() != 2 {
		t.Fatalf("RepositoryIdentity has %d fields, want 2", identity.NumField())
	}
	if field, found := identity.FieldByName("Root"); !found || field.Type != reflect.TypeOf("") {
		t.Fatalf("RepositoryIdentity.Root type = %v, want string", field.Type)
	}
	if field, found := identity.FieldByName("Home"); !found || field.Type != reflect.TypeOf("") {
		t.Fatalf("RepositoryIdentity.Home type = %v, want string", field.Type)
	}
}

func testContractResultShape(t *testing.T) {
	result := reflect.TypeOf(Result{})
	exported := 0
	for index := 0; index < result.NumField(); index++ {
		if result.Field(index).PkgPath == "" {
			exported++
		}
	}
	if exported != 0 {
		t.Fatalf("Result exposes %d exported fields, want an opaque evaluation carrier", exported)
	}
}

func testContractPortShapes(t *testing.T) {
	if count := methodCount(reflect.TypeOf((*RepositorySource)(nil)).Elem()); count != 1 {
		t.Fatalf("RepositorySource has %d methods, want 1", count)
	}
	if count := methodCount(reflect.TypeOf((*Compiler)(nil)).Elem()); count != 1 {
		t.Fatalf("Compiler has %d methods, want 1", count)
	}
	if count := methodCount(reflect.TypeOf((*StateReader)(nil)).Elem()); count != 3 {
		t.Fatalf("StateReader has %d methods, want 3", count)
	}
}

func methodCount(port reflect.Type) int {
	count := 0
	for index := 0; index < port.NumMethod(); index++ {
		count++
	}
	return count
}

func testContractServiceSignature(t *testing.T) {
	method, found := reflect.TypeOf((*Service)(nil)).MethodByName("Evaluate")
	if !found {
		t.Fatal("Service.Evaluate method missing")
	}
	signature := method.Type
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if signature.NumIn() != 3 ||
		signature.In(1) != contextType ||
		signature.In(2) != reflect.TypeOf(Request{}) {
		t.Fatalf("Evaluate parameters = %v, want (context.Context, Request)", signature)
	}
	if signature.NumOut() != 2 ||
		signature.Out(0) != reflect.TypeOf(Result{}) ||
		signature.Out(1) != errorType {
		t.Fatalf("Evaluate results = %v, want (Result, error)", signature)
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
