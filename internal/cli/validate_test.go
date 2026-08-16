package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/validate"
	"github.com/spf13/cobra"
)

func TestValidateCommand(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"argument order preserved", testValidateOrder},
		{"repository flag mapped", testValidateRepository},
		{"one call", testValidateOneCall},
		{"service error propagates", testValidateError},
		{"writer failure", testValidateWriterError},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// validateServiceFake records requests and returns fixed results.
type validateServiceFake struct {
	requests []validate.Request
	result   validate.Result
	err      error
}

func (f *validateServiceFake) Validate(ctx context.Context, request validate.Request) (validate.Result, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return validate.Result{}, f.err
	}
	return f.result, nil
}

// validateFixture builds one validate command over a recording service.
func validateFixture(t *testing.T, service *validateServiceFake, options Options) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: stdout}, WorkingDir: "/work", Environment: []string{"CATTERY_REPO=envrepo"}})
	command := newValidateCommand(service, runtime, &options)
	bindSharedFlags(command, &options)
	return command, stdout
}

// validateResult freezes the two sorted platform count records.
func validateResult() validate.Result {
	return validate.Result{Platforms: []validate.PlatformCount{
		{Platform: "darwin", Files: 1, Secrets: 2, Aliases: 3, Groups: 4},
		{Platform: "linux", Files: 5, Secrets: 6, Aliases: 7, Groups: 8},
	}}
}

func testValidateOrder(t *testing.T) {
	service := &validateServiceFake{result: validateResult()}
	command, _ := validateFixture(t, service, Options{})
	command.SetArgs([]string{"first", "--repo", "repo", "second"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(service.requests) != 1 {
		t.Fatalf("calls = %d, want one", len(service.requests))
	}
	request := service.requests[0]
	if len(request.Groups) != 2 || request.Groups[0] != "first" || request.Groups[1] != "second" {
		t.Fatalf("groups = %v, want the raw interspersed order", request.Groups)
	}
	if request.Repository.RawExplicit != "repo" || !request.Repository.ExplicitSet {
		t.Fatalf("repository = %+v, want the explicit flag value", request.Repository)
	}
}

func testValidateRepository(t *testing.T) {
	service := &validateServiceFake{result: validateResult()}
	command, _ := validateFixture(t, service, Options{Repository: "flagrepo", RepositorySet: true})
	command.SetArgs([]string{"-r", "flagrepo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	repository := service.requests[0].Repository
	if repository.RawExplicit != "flagrepo" || !repository.ExplicitSet {
		t.Fatalf("repository = %+v, want the flag value", repository)
	}
	if repository.RawEnv != "envrepo" || !repository.EnvSet {
		t.Fatalf("repository = %+v, want the injected environment", repository)
	}
	if repository.WorkingDir != "/work" {
		t.Fatalf("working dir = %q, want /work", repository.WorkingDir)
	}
}

func testValidateOneCall(t *testing.T) {
	service := &validateServiceFake{result: validateResult()}
	command, stdout := validateFixture(t, service, Options{})
	command.SetArgs([]string{"apps"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(service.requests) != 1 {
		t.Fatalf("calls = %d, want one", len(service.requests))
	}
	want := "Repository is valid.\n\n  darwin\n    Files: 1  Secrets: 2  Links: 3  Groups: 4\n\n  linux\n    Files: 5  Secrets: 6  Links: 7  Groups: 8\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want the two count lines", stdout.String())
	}
}

func testValidateError(t *testing.T) {
	service := &validateServiceFake{err: errors.New("broken")}
	command, stdout := validateFixture(t, service, Options{})
	command.SetArgs([]string{})
	if err := command.Execute(); err == nil {
		t.Fatal("the service error must propagate")
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want no render after an error", stdout.String())
	}
}

func testValidateWriterError(t *testing.T) {
	service := &validateServiceFake{result: validateResult()}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: failingWriter{}}, WorkingDir: "/work"})
	command := newValidateCommand(service, runtime, &Options{})
	if err := command.Execute(); err == nil {
		t.Fatal("a writer failure must surface")
	}
}
