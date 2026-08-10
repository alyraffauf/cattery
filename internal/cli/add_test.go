package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/add"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/spf13/cobra"
)

func TestAddCommand(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"target order preserved", testAddOrder},
		{"interspersed flags", testAddInterspersed},
		{"explicit false", testAddExplicitFalse},
		{"repeated arguments", testAddRepeated},
		{"dry run flag", testAddDryRun},
		{"partial error", testAddPartial},
		{"writer failure", testAddWriterError},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// addServiceFake records requests and returns fixed results.
type addServiceFake struct {
	requests []add.Request
	result   add.Result
	err      error
}

func (f *addServiceFake) Add(ctx context.Context, request add.Request) (add.Result, error) {
	f.requests = append(f.requests, request)
	return f.result, f.err
}

// addFixture builds one add command over a recording service.
func addFixture(t *testing.T, service *addServiceFake, options Options) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: stdout}, WorkingDir: "/work", Environment: []string{"CATTERY_REPO=envrepo"}})
	command := newAddCommand(service, runtime, &options)
	bindSharedFlags(command, &options)
	return command, stdout
}

func testAddOrder(t *testing.T) {
	service := &addServiceFake{result: addResult()}
	command, _ := addFixture(t, service, Options{})
	command.SetArgs([]string{"b.conf", "a.conf"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	request := service.requests[0]
	if len(request.Targets) != 2 || request.Targets[0] != "b.conf" || request.Targets[1] != "a.conf" {
		t.Fatalf("targets = %v, want the raw order preserved", request.Targets)
	}
	if request.Repository.WorkingDir != "/work" {
		t.Fatalf("working dir = %q, want /work", request.Repository.WorkingDir)
	}
}

func testAddInterspersed(t *testing.T) {
	service := &addServiceFake{result: addResult()}
	command, _ := addFixture(t, service, Options{})
	command.SetArgs([]string{"--group", "apps", "first", "--secret", "second", "third"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	request := service.requests[0]
	if request.Group != "apps" || !request.GroupSet {
		t.Fatalf("group = %q set = %v, want the flag value", request.Group, request.GroupSet)
	}
	if !request.SecretSet || !request.Secret {
		t.Fatalf("secret = %v set = %v, want the flag value", request.Secret, request.SecretSet)
	}
	if len(request.Targets) != 3 {
		t.Fatalf("targets = %v, want all three arguments", request.Targets)
	}
}

func testAddExplicitFalse(t *testing.T) {
	service := &addServiceFake{result: addResult()}
	command, _ := addFixture(t, service, Options{})
	command.SetArgs([]string{"--secret=false", "--dry-run=false", "a.conf"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	request := service.requests[0]
	if !request.SecretSet || request.Secret {
		t.Fatalf("secret = %v set = %v, want an explicit false", request.Secret, request.SecretSet)
	}
	if request.DryRun {
		t.Fatalf("dry run = %v, want an explicit false", request.DryRun)
	}
}

func testAddRepeated(t *testing.T) {
	service := &addServiceFake{result: addResult()}
	command, _ := addFixture(t, service, Options{})
	command.SetArgs([]string{"a.conf", "a.conf"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(service.requests[0].Targets) != 2 {
		t.Fatalf("repeated arguments must be preserved for the service")
	}
}

func testAddDryRun(t *testing.T) {
	service := &addServiceFake{result: addResult()}
	command, _ := addFixture(t, service, Options{})
	command.SetArgs([]string{"--dry-run", "a.conf"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !service.requests[0].DryRun {
		t.Fatalf("dry run = %v, want the flag value", service.requests[0].DryRun)
	}
}

func testAddPartial(t *testing.T) {
	service := &addServiceFake{result: addResult(), err: failure.New(failure.Operational, "add: partial batch", nil)}
	command, stdout := addFixture(t, service, Options{})
	command.SetArgs([]string{"a.conf"})
	err := command.Execute()
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("error = %v, want an operational failure", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("$HOME/a.conf")) {
		t.Fatalf("stdout = %q, want the partial records rendered", stdout.String())
	}
}

func testAddWriterError(t *testing.T) {
	service := &addServiceFake{result: addResult()}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: failingWriter{}}, WorkingDir: "/work"})
	command := newAddCommand(service, runtime, &Options{})
	if err := command.Execute(); err == nil {
		t.Fatal("a writer failure must surface")
	}
}

// addResult freezes one completed add result.
func addResult() add.Result {
	return add.Result{Items: []add.ItemResult{
		{Target: "a.conf", Source: "a.conf", Status: add.StatusCompleted},
	}, Summary: add.Summary{Completed: 1}}
}
