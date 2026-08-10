package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/apply"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/spf13/cobra"
)

func TestApplyCommand(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"flags and args mapped", testApplyFlags},
		{"one call", testApplyOneCall},
		{"dry run flag", testApplyDryRun},
		{"partial error joins", testApplyPartial},
		{"writer failure", testApplyWriterError},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// applyServiceFake records requests and returns fixed results.
type applyServiceFake struct {
	requests []apply.Request
	result   apply.Result
	err      error
}

func (f *applyServiceFake) Apply(ctx context.Context, request apply.Request) (apply.Result, error) {
	f.requests = append(f.requests, request)
	return f.result, f.err
}

// applyFixture builds one apply command over a recording service.
func applyFixture(t *testing.T, service *applyServiceFake, options Options) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: stdout}, WorkingDir: "/work"})
	command := newApplyCommand(service, runtime, &options)
	bindSharedFlags(command, &options)
	return command, stdout
}

func testApplyFlags(t *testing.T) {
	service := &applyServiceFake{result: applyResult()}
	command, _ := applyFixture(t, service, Options{})
	command.SetArgs([]string{"-r", "repo", "--non-interactive", "--no-hooks", "apps", "tools"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	request := service.requests[0]
	if request.Repository.RawExplicit != "repo" || !request.Repository.ExplicitSet {
		t.Fatalf("repository = %+v, want the flag value", request.Repository)
	}
	if !request.NonInteractive || !request.NoHooks {
		t.Fatalf("policy = %+v, want the explicit flags", request)
	}
	if len(request.Groups) != 2 || request.Groups[0] != "apps" || request.Groups[1] != "tools" {
		t.Fatalf("groups = %v, want the raw order", request.Groups)
	}
}

func testApplyOneCall(t *testing.T) {
	service := &applyServiceFake{result: applyResult()}
	command, stdout := applyFixture(t, service, Options{})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(service.requests) != 1 {
		t.Fatalf("calls = %d, want one", len(service.requests))
	}
	want := "$HOME/a.conf completed write-source\nsummary planned=0 completed=1 partial=0\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testApplyDryRun(t *testing.T) {
	service := &applyServiceFake{result: applyResult()}
	command, _ := applyFixture(t, service, Options{})
	command.SetArgs([]string{"--dry-run", "apps"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	request := service.requests[0]
	if !request.DryRun {
		t.Fatalf("dry run = %v, want the flag value", request.DryRun)
	}
}

func testApplyPartial(t *testing.T) {
	service := &applyServiceFake{result: applyResult(), err: failure.New(failure.Operational, "apply: partial write", nil)}
	command, stdout := applyFixture(t, service, Options{})
	err := command.Execute()
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("error = %v, want an operational failure", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("$HOME/a.conf")) {
		t.Fatalf("stdout = %q, want the partial records rendered", stdout.String())
	}
}

func testApplyWriterError(t *testing.T) {
	service := &applyServiceFake{result: applyResult()}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: failingWriter{}}, WorkingDir: "/work"})
	command := newApplyCommand(service, runtime, &Options{})
	if err := command.Execute(); err == nil {
		t.Fatal("a writer failure must surface")
	}
}

// applyResult freezes one completed apply result.
func applyResult() apply.Result {
	return apply.Result{Items: []apply.ItemResult{
		{TargetPath: "a.conf", Status: apply.StatusCompleted, Kind: apply.ActionKindWriteSource},
	}, Summary: apply.Summary{Completed: 1}}
}
