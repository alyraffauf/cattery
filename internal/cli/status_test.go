package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/inspect"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/spf13/cobra"
)

func TestStatusCommand(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"flags and args mapped", testStatusFlags},
		{"one call", testStatusOneCall},
		{"output before difference", testStatusDifference},
		{"service error", testStatusError},
		{"writer failure", testStatusWriterError},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// statusServiceFake records requests and returns fixed results.
type statusServiceFake struct {
	requests []inspect.Request
	result   inspect.StatusResult
	err      error
}

func (f *statusServiceFake) Status(ctx context.Context, request inspect.Request) (inspect.StatusResult, error) {
	f.requests = append(f.requests, request)
	return f.result, f.err
}

// statusRecord freezes one pending status record.
func statusRecord(target string, kind inspect.StatusKind, action string) inspect.StatusRecord {
	return inspect.NewStatusRecord(target, kind, action)
}

// statusResult freezes one unconverged status result.
func statusResult() inspect.StatusResult {
	return inspect.NewStatusResult([]inspect.StatusRecord{
		statusRecord("a.conf", inspect.StatusKindFile, "write-source"),
	}, false)
}

// statusFixture builds one status command over a recording service.
func statusFixture(t *testing.T, service *statusServiceFake, options Options) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: stdout}, WorkingDir: "/work", Environment: []string{"CATTERY_REPO=envrepo"}})
	command := newStatusCommand(service, runtime, &options)
	bindSharedFlags(command, &options)
	return command, stdout
}

func testStatusFlags(t *testing.T) {
	service := &statusServiceFake{result: statusResult()}
	command, _ := statusFixture(t, service, Options{})
	command.SetArgs([]string{"-r", "repo", "apps", "tools"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	request := service.requests[0]
	if request.Repository.RawExplicit != "repo" || !request.Repository.ExplicitSet {
		t.Fatalf("repository = %+v, want the flag value", request.Repository)
	}
	if len(request.Groups) != 2 || request.Groups[0] != "apps" || request.Groups[1] != "tools" {
		t.Fatalf("groups = %v, want the raw order", request.Groups)
	}
}

func testStatusOneCall(t *testing.T) {
	service := &statusServiceFake{result: statusResult()}
	command, stdout := statusFixture(t, service, Options{})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(service.requests) != 1 {
		t.Fatalf("calls = %d, want one", len(service.requests))
	}
	want := "Changes needed — 1 change\n\n  Update   ~/a.conf\n\nNo files were changed.\nNext: run `cattery apply` to make these changes.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testStatusDifference(t *testing.T) {
	service := &statusServiceFake{result: statusResult(), err: failure.New(failure.Difference, "status: selected state is not converged", nil)}
	command, stdout := statusFixture(t, service, Options{})
	err := command.Execute()
	if err == nil || !kindIs(err, failure.Difference) {
		t.Fatalf("error = %v, want a difference failure", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("~/a.conf")) {
		t.Fatalf("stdout = %q, want the records rendered before the difference", stdout.String())
	}
}

func testStatusError(t *testing.T) {
	service := &statusServiceFake{err: errors.New("broken")}
	command, stdout := statusFixture(t, service, Options{})
	if err := command.Execute(); err == nil {
		t.Fatal("the service error must propagate")
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want no render after an error", stdout.String())
	}
}

func testStatusWriterError(t *testing.T) {
	service := &statusServiceFake{result: statusResult()}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: failingWriter{}}, WorkingDir: "/work"})
	command := newStatusCommand(service, runtime, &Options{})
	if err := command.Execute(); err == nil {
		t.Fatal("a writer failure must surface")
	}
}
