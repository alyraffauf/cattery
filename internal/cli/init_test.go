package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/initialize"
	"github.com/spf13/cobra"
)

func TestInitCommand(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"one argument", testInitArgument},
		{"defaults to working directory", testInitCwd},
		{"arity rejected", testInitArity},
		{"service error stops rendering", testInitServiceError},
		{"writer error surfaces", testInitWriterError},
		{"zero calls on failure", testInitZeroCalls},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// initServiceFake records requests and returns fixed results.
type initServiceFake struct {
	requests []initialize.Request
	result   initialize.Result
	err      error
}

func (f *initServiceFake) Initialize(ctx context.Context, request initialize.Request) (initialize.Result, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return initialize.Result{}, f.err
	}
	return f.result, nil
}

// initFixture builds one init command over a recording service.
func initFixture(t *testing.T, service *initServiceFake, workingDir string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{
		Streams: Streams{Stdout: stdout}, WorkingDir: workingDir,
	})
	return newInitCommand(service, runtime), stdout
}

// initResult freezes one registered repository result without importing
// the state package, which the CLI boundary may not reference.
func initResult(root string) initialize.Result {
	var result initialize.Result
	repository := reflect.ValueOf(&result.Repository).Elem()
	repository.FieldByName("RootPath").SetString(root)
	repository.FieldByName("HomePath").SetString("/home")
	repository.FieldByName("IsDefault").SetBool(true)
	return result
}

// runInit executes one init invocation.
func runInit(t *testing.T, command *cobra.Command, args ...string) error {
	t.Helper()
	command.SetArgs(args)
	return command.Execute()
}

func testInitArgument(t *testing.T) {
	service := &initServiceFake{result: initResult("/repo")}
	command, stdout := initFixture(t, service, "/work")
	if err := runInit(t, command, "/repo"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(service.requests) != 1 || service.requests[0].Path != "/repo" {
		t.Fatalf("requests = %+v, want one /repo request", service.requests)
	}
	if stdout.String() != "Repository initialized\n\n  Repository: /repo\n\nNext: run `cattery status` to review changes.\n" {
		t.Fatalf("stdout = %q, want the initialization guidance", stdout.String())
	}
}

func testInitCwd(t *testing.T) {
	service := &initServiceFake{result: initResult("/work")}
	command, _ := initFixture(t, service, "/work")
	if err := runInit(t, command); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(service.requests) != 1 || service.requests[0].Path != "/work" {
		t.Fatalf("requests = %+v, want the working directory", service.requests)
	}
}

func testInitArity(t *testing.T) {
	service := &initServiceFake{result: initResult("/repo")}
	command, _ := initFixture(t, service, "/work")
	if err := runInit(t, command, "a", "b"); err == nil {
		t.Fatal("two positional arguments must be rejected")
	}
	if len(service.requests) != 0 {
		t.Fatalf("an arity error must not call the service, calls = %d", len(service.requests))
	}
}

func testInitServiceError(t *testing.T) {
	service := &initServiceFake{err: errors.New("unsupported repository")}
	command, stdout := initFixture(t, service, "/work")
	if err := runInit(t, command, "/bad"); err == nil {
		t.Fatal("the service error must propagate")
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want no render after a service error", stdout.String())
	}
}

func testInitWriterError(t *testing.T) {
	service := &initServiceFake{result: initResult("/repo")}
	runtime := NewRuntime(RuntimeInput{
		Streams: Streams{Stdout: failingWriter{}}, WorkingDir: "/work",
	})
	command := newInitCommand(service, runtime)
	if err := runInit(t, command, "/repo"); err == nil {
		t.Fatal("a writer failure must surface")
	}
}

func testInitZeroCalls(t *testing.T) {
	service := &initServiceFake{err: errors.New("boom")}
	command, _ := initFixture(t, service, "/work")
	if err := runInit(t, command, "/repo"); err == nil {
		t.Fatal("the service error must propagate")
	}
	if len(service.requests) != 1 {
		t.Fatalf("calls = %d, want exactly one", len(service.requests))
	}
}

// failingWriter fails on every write.
type failingWriter struct{}

func (f failingWriter) Write(data []byte) (int, error) {
	return 0, errors.New("write failed")
}
