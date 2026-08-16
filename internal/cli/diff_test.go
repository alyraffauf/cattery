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

func TestDiffCommand(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"flags and args mapped", testDiffFlags},
		{"one call", testDiffOneCall},
		{"output before difference", testDiffDifference},
		{"service error", testDiffError},
		{"writer failure", testDiffWriterError},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// diffServiceFake records requests and returns fixed results.
type diffServiceFake struct {
	requests []inspect.Request
	result   inspect.DiffResult
	err      error
}

func (f *diffServiceFake) Diff(ctx context.Context, request inspect.Request) (inspect.DiffResult, error) {
	f.requests = append(f.requests, request)
	return f.result, f.err
}

// diffFixture builds one diff command over a recording service.
func diffFixture(t *testing.T, service *diffServiceFake, options Options) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: stdout}, WorkingDir: "/work"})
	command := newDiffCommand(service, runtime, &options)
	bindSharedFlags(command, &options)
	return command, stdout
}

func testDiffFlags(t *testing.T) {
	service := &diffServiceFake{result: diffResult()}
	command, _ := diffFixture(t, service, Options{})
	command.SetArgs([]string{"-r", "repo", "apps"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	request := service.requests[0]
	if request.Repository.RawExplicit != "repo" || !request.Repository.ExplicitSet {
		t.Fatalf("repository = %+v, want the flag value", request.Repository)
	}
	if len(request.Groups) != 1 || request.Groups[0] != "apps" {
		t.Fatalf("groups = %v, want the raw argument", request.Groups)
	}
}

func testDiffOneCall(t *testing.T) {
	service := &diffServiceFake{result: diffResult()}
	command, _ := diffFixture(t, service, Options{})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(service.requests) != 1 {
		t.Fatalf("calls = %d, want one", len(service.requests))
	}
}

func testDiffDifference(t *testing.T) {
	service := &diffServiceFake{result: diffResult(diffRecord(diffSpec{target: "a.conf", kind: inspect.StatusKindFile, tag: "none", action: "write-source"})), err: failure.New(failure.Difference, "diff: selected state is not converged", nil)}
	command, stdout := diffFixture(t, service, Options{})
	err := command.Execute()
	if err == nil || !kindIs(err, failure.Difference) {
		t.Fatalf("error = %v, want a difference failure", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("~/a.conf")) {
		t.Fatalf("stdout = %q, want the records rendered before the difference", stdout.String())
	}
}

func testDiffError(t *testing.T) {
	service := &diffServiceFake{err: errors.New("broken")}
	command, stdout := diffFixture(t, service, Options{})
	if err := command.Execute(); err == nil {
		t.Fatal("the service error must propagate")
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want no render after an error", stdout.String())
	}
}

func testDiffWriterError(t *testing.T) {
	service := &diffServiceFake{result: diffResult()}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: failingWriter{}}, WorkingDir: "/work"})
	command := newDiffCommand(service, runtime, &Options{})
	if err := command.Execute(); err == nil {
		t.Fatal("a writer failure must surface")
	}
}
