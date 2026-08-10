package add

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/selection"
	"github.com/alyraffauf/cattery/internal/state"
)

func TestAddContract(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"targets preserve raw order", testContractRawOrder},
		{"omitted options keep presence false", testContractExplicitFalse},
		{"plans copy and validate defensively", testContractDefensivePlans},
		{"ports and dependencies expose narrow shapes", testContractPortShapes},
		{"no cli or adapter types", testContractCleanTypes},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testContractRawOrder(t *testing.T) {
	targets := []string{"third", "first", "second"}
	request := Request{Targets: targets}
	if !reflect.DeepEqual(request.Targets, targets) {
		t.Fatalf("Request.Targets = %v, want the raw command-line order %v", request.Targets, targets)
	}
	if request.Repository.WorkingDir != "" {
		t.Fatalf("zero RepositoryInput has working directory %q", request.Repository.WorkingDir)
	}
}

func testContractExplicitFalse(t *testing.T) {
	var request Request
	if request.GroupSet || request.PlatformSet || request.SecretSet {
		t.Fatal("omitted --group/--platform/--secret must leave their presence bits false")
	}
	if request.Group != "" || request.Platform != "" || request.Secret || request.DryRun {
		t.Fatal("omitted options must retain zero values")
	}
}

func testContractDefensivePlans(t *testing.T) {
	first, err := NewItemPlan(itemInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewItemPlan(itemInput("second"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewBatchPlan(BatchPlanInput{
		Items:          []ItemPlan{first, second},
		ExecutionOrder: []int{1, 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFrozenCopies(t, plan)
	rejectBadOrders(t, []ItemPlan{first, second})
	if _, err := NewItemPlan(ItemPlanInput{}); err == nil {
		t.Fatal("NewItemPlan accepted an empty input")
	}
}

// assertFrozenCopies mutates the slices returned by the accessors and proves
// the plan still yields its pristine values; shared storage would leak the
// mutation back through the accessors.
func assertFrozenCopies(t *testing.T, plan BatchPlan) {
	t.Helper()
	items := plan.Items()
	items[0] = ItemPlan{}
	order := plan.ExecutionOrder()
	order[0] = 99
	if reflect.DeepEqual(plan.Items(), items) {
		t.Fatal("BatchPlan.Items shares storage with a caller")
	}
	if reflect.DeepEqual(plan.ExecutionOrder(), order) {
		t.Fatal("BatchPlan.ExecutionOrder shares storage with a caller")
	}
}

func rejectBadOrders(t *testing.T, items []ItemPlan) {
	t.Helper()
	invalidOrders := [][]int{{0}, {0, 0}, {2, 0}, {0, -1}}
	for _, invalid := range invalidOrders {
		if _, err := NewBatchPlan(BatchPlanInput{Items: items, ExecutionOrder: invalid}); err == nil {
			t.Fatalf("NewBatchPlan accepted invalid execution order %v", invalid)
		}
	}
}

func itemInput(name string) ItemPlanInput {
	return ItemPlanInput{
		Scope:                deployment.NewScope("group"),
		Layer:                deployment.LayerLinux,
		Kind:                 deployment.FileOrdinary,
		TargetAbsolutePath:   "/home/user/" + name,
		TargetRelativePath:   name,
		SourceAbsolutePath:   "/repo/" + name,
		SourceRepositoryPath: name,
	}
}

func testContractPortShapes(t *testing.T) {
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	contracts := []expectedMethod{
		{
			port:    reflect.TypeOf((*RepositorySource)(nil)).Elem(),
			name:    "Resolve",
			inputs:  []reflect.Type{reflect.TypeOf(selection.RepositoryRequest{})},
			outputs: []reflect.Type{reflect.TypeOf(RepositoryIdentity{}), errorType},
		},
		{
			port:    reflect.TypeOf((*Compiler)(nil)).Elem(),
			name:    "Compile",
			inputs:  []reflect.Type{reflect.TypeOf(repository.CompileInput{})},
			outputs: []reflect.Type{reflect.TypeOf(deployment.Plan{}), errorType},
		},
		{
			port:    reflect.TypeOf((*AtomicWriter)(nil)).Elem(),
			name:    "ReplaceResult",
			inputs:  []reflect.Type{contextType, reflect.TypeOf(filesystem.Precondition{}), reflect.TypeOf(filesystem.ReplacementSpec{})},
			outputs: []reflect.Type{reflect.TypeOf(filesystem.ReplaceResult{}), errorType},
		},
		{
			port:    reflect.TypeOf((*BaselineStore)(nil)).Elem(),
			name:    "UpsertFileBaseline",
			inputs:  []reflect.Type{reflect.TypeOf(""), reflect.TypeOf(""), reflect.TypeOf(state.FileBaseline{})},
			outputs: []reflect.Type{reflect.TypeOf(state.FileBaseline{}), errorType},
		},
	}
	for _, contract := range contracts {
		assertMethod(t, contract)
	}
	assertDependencySeams(t)
}

type expectedMethod struct {
	port    reflect.Type
	name    string
	inputs  []reflect.Type
	outputs []reflect.Type
}

func assertMethod(t *testing.T, expected expectedMethod) {
	t.Helper()
	if expected.port.Kind() != reflect.Interface || expected.port.NumMethod() != 1 {
		t.Fatalf("%v is %v with %d methods, want a one-method interface",
			expected.port, expected.port.Kind(), expected.port.NumMethod())
	}
	method, found := expected.port.MethodByName(expected.name)
	if !found {
		t.Fatalf("%v.%s is missing", expected.port, expected.name)
	}
	shape := method.Type
	if shape.NumIn() != len(expected.inputs) || shape.NumOut() != len(expected.outputs) {
		t.Fatalf("%v.%s has %d inputs and %d outputs, want %d and %d",
			expected.port, expected.name, shape.NumIn(), shape.NumOut(),
			len(expected.inputs), len(expected.outputs))
	}
	assertSide(t, expected, methodSide{label: "input", actual: methodTypes(shape.NumIn(), shape.In), want: expected.inputs})
	assertSide(t, expected, methodSide{label: "output", actual: methodTypes(shape.NumOut(), shape.Out), want: expected.outputs})
}

type methodSide struct {
	label  string
	actual []reflect.Type
	want   []reflect.Type
}

func assertSide(t *testing.T, expected expectedMethod, side methodSide) {
	t.Helper()
	for index, want := range side.want {
		if side.actual[index] != want {
			t.Fatalf("%v.%s %s %d = %v, want %v",
				expected.port, expected.name, side.label, index, side.actual[index], want)
		}
	}
}

func methodTypes(count int, at func(int) reflect.Type) []reflect.Type {
	types := make([]reflect.Type, count)
	for index := range types {
		types[index] = at(index)
	}
	return types
}

func assertDependencySeams(t *testing.T) {
	t.Helper()
	dependencies := reflect.TypeOf(Dependencies{})
	ports := map[string]reflect.Type{
		"RepositorySource": reflect.TypeOf((*RepositorySource)(nil)).Elem(),
		"Compiler":         reflect.TypeOf((*Compiler)(nil)).Elem(),
		"Writer":           reflect.TypeOf((*AtomicWriter)(nil)).Elem(),
		"Baselines":        reflect.TypeOf((*BaselineStore)(nil)).Elem(),
	}
	if dependencies.NumField() != len(ports) {
		t.Fatalf("Dependencies has %d fields, want %d", dependencies.NumField(), len(ports))
	}
	for name, port := range ports {
		field, found := dependencies.FieldByName(name)
		if !found {
			t.Fatalf("Dependencies.%s is missing", name)
		}
		if field.Type != port {
			t.Fatalf("Dependencies.%s type = %v, want the %v port", name, field.Type, port)
		}
	}
}

func testContractCleanTypes(t *testing.T) {
	for _, name := range packageSources(t) {
		assertCleanImports(t, name)
	}
	dtos := []any{RepositoryInput{}, Request{}, ItemResult{}, Result{}, Summary{}}
	for _, dto := range dtos {
		assertDTOFields(t, reflect.TypeOf(dto))
	}
}

func assertDTOFields(t *testing.T, kind reflect.Type) {
	t.Helper()
	for index := 0; index < kind.NumField(); index++ {
		field := kind.Field(index)
		if !dtoFieldAllowed(field.Type) {
			t.Fatalf("%s.%s has type %v, a cli/adapter type must not cross the seam",
				kind.Name(), field.Name, field.Type)
		}
	}
}

func dtoFieldAllowed(kind reflect.Type) bool {
	switch kind.Kind() {
	case reflect.Bool, reflect.Int, reflect.String:
		return true
	case reflect.Slice:
		return dtoFieldAllowed(kind.Elem())
	}
	switch kind {
	case reflect.TypeOf(RepositoryInput{}), reflect.TypeOf(Request{}),
		reflect.TypeOf(ItemResult{}), reflect.TypeOf(Summary{}):
		return true
	}
	return false
}

func assertCleanImports(t *testing.T, name string) {
	t.Helper()
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
