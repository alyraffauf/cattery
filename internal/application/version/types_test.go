package version

import (
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/buildinfo"
)

func TestVersionContract(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"zero result carries no build identity", testContractZeroResult},
		{"result exposes the eight typed fields", testContractResultShape},
		{"from-snapshot copies development defaults", testContractDevelopmentDefaults},
		{"from-snapshot copies release values", testContractReleaseValues},
		{"from-snapshot normalizes input to UTC", testContractUTCInput},
		{"from-snapshot copies runtime fields", testContractRuntimeFields},
		{"service exposes one version method", testContractServiceSignature},
		{"no cli or third-party imports", testContractNoCLIImports},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testContractZeroResult(t *testing.T) {
	var result Result
	if result.Version != "" || result.Commit != "" || result.Timestamp != "" {
		t.Fatalf("zero Result carries build identity %+v", result)
	}
	if result.HasTimestamp || !result.BuiltAt.IsZero() {
		t.Fatalf("zero Result carries a timestamp %+v", result.BuiltAt)
	}
	if result.GoVersion != "" || result.OperatingSystem != "" || result.Architecture != "" {
		t.Fatalf("zero Result carries runtime identity %+v", result)
	}
}

func testContractResultShape(t *testing.T) {
	result := reflect.TypeOf(Result{})
	want := map[string]reflect.Type{
		"Version":         reflect.TypeOf(""),
		"Commit":          reflect.TypeOf(""),
		"Timestamp":       reflect.TypeOf(""),
		"BuiltAt":         reflect.TypeOf(time.Time{}),
		"HasTimestamp":    reflect.TypeOf(false),
		"GoVersion":       reflect.TypeOf(""),
		"OperatingSystem": reflect.TypeOf(""),
		"Architecture":    reflect.TypeOf(""),
	}
	if result.NumField() != len(want) {
		t.Fatalf("Result has %d fields, want %d", result.NumField(), len(want))
	}
	for name, fieldType := range want {
		field, found := result.FieldByName(name)
		if !found || field.Type != fieldType {
			t.Fatalf("Result.%s type = %v, want %v", name, field.Type, fieldType)
		}
	}
}

func testContractDevelopmentDefaults(t *testing.T) {
	result := FromSnapshot(buildinfo.FromValues("dev", "unknown", "unknown"))
	if result.Version != "dev" {
		t.Fatalf("version = %q", result.Version)
	}
	if result.Commit != "unknown" {
		t.Fatalf("commit = %q", result.Commit)
	}
	if result.Timestamp != "unknown" {
		t.Fatalf("timestamp = %q", result.Timestamp)
	}
	if result.HasTimestamp {
		t.Fatal("development timestamp must not parse")
	}
	if !result.BuiltAt.IsZero() {
		t.Fatal("development built-at must be zero")
	}
}

func testContractReleaseValues(t *testing.T) {
	result := FromSnapshot(buildinfo.FromValues("v1.2.3", "abcdef1234567890", "2026-08-09T12:00:00Z"))
	if result.Version != "v1.2.3" {
		t.Fatalf("version = %q", result.Version)
	}
	if result.Commit != "abcdef1234567890" {
		t.Fatalf("commit = %q", result.Commit)
	}
	if result.Timestamp != "2026-08-09T12:00:00Z" {
		t.Fatalf("timestamp = %q", result.Timestamp)
	}
	if !result.HasTimestamp {
		t.Fatal("release timestamp must parse")
	}
}

func testContractUTCInput(t *testing.T) {
	result := FromSnapshot(buildinfo.FromValues("v1.2.3", "deadbeef", "2026-08-09T14:00:00+02:00"))
	if !result.HasTimestamp {
		t.Fatal("offset timestamp must parse")
	}
	if want := "2026-08-09T12:00:00Z"; result.BuiltAt.Format(time.RFC3339) != want {
		t.Fatalf("built-at = %q, want %q", result.BuiltAt.Format(time.RFC3339), want)
	}
	if result.BuiltAt.Location().String() != "UTC" {
		t.Fatalf("location = %q", result.BuiltAt.Location())
	}
}

func testContractRuntimeFields(t *testing.T) {
	result := FromSnapshot(buildinfo.FromValues("dev", "unknown", "unknown"))
	if result.GoVersion != runtime.Version() {
		t.Fatalf("go version = %q", result.GoVersion)
	}
	if result.OperatingSystem != runtime.GOOS {
		t.Fatalf("os = %q", result.OperatingSystem)
	}
	if result.Architecture != runtime.GOARCH {
		t.Fatalf("arch = %q", result.Architecture)
	}
}

func testContractServiceSignature(t *testing.T) {
	method, found := reflect.TypeOf((*Service)(nil)).MethodByName("Version")
	if !found {
		t.Fatal("Service.Version method missing")
	}
	signature := method.Type
	if signature.NumIn() != 1 || signature.In(0) != reflect.TypeOf((*Service)(nil)) {
		t.Fatalf("Version receiver = %v, want (*Service)", signature)
	}
	if signature.NumOut() != 1 || signature.Out(0) != reflect.TypeOf(Result{}) {
		t.Fatalf("Version results = %v, want (Result)", signature)
	}
	if constructor := reflect.TypeOf(NewService); constructor.NumIn() != 0 {
		t.Fatalf("NewService parameters = %d, want none", constructor.NumIn())
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
