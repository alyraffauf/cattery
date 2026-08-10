package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/failure"
)

func TestCLIExecute(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"success is zero", testExecuteSuccess},
		{"usage failure is one", testExecuteUsage},
		{"operational failure is one", testExecuteOperational},
		{"difference is two", testExecuteDifference},
		{"hook is three", testExecuteHook},
		{"dependency is four", testExecuteDependency},
		{"signal outranks joined", testExecuteSignal},
		{"terminate is 143", testExecuteTerminate},
		{"second use rejected", testExecuteSecondUse},
		{"silence settings", testExecuteSilence},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testExecuteSuccess(t *testing.T) {
	application, _, stdout, stderr := rootFixture(t)
	status := Execute(context.Background(), application, []string{"version"})
	if status != 0 {
		t.Fatalf("status = %d, want 0", status)
	}
	if stdout.String() == "" {
		t.Fatal("stdout must carry the version line")
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want no diagnostic on success", stderr.String())
	}
}

func testExecuteUsage(t *testing.T) {
	application, _, _, stderr := rootFixture(t)
	status := Execute(context.Background(), application, []string{"--version"})
	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	if stderr.String() == "" {
		t.Fatal("a diagnostic must be written")
	}
}

func testExecuteOperational(t *testing.T) {
	application, fakes, _, _ := rootFixture(t)
	fakes.status.err = failure.New(failure.Operational, "status: broken", nil)
	fakes.status.result = statusResult()
	status := Execute(context.Background(), application, []string{"status"})
	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
}

func testExecuteDifference(t *testing.T) {
	application, fakes, _, _ := rootFixture(t)
	fakes.status.err = failure.New(failure.Difference, "status: not converged", nil)
	fakes.status.result = statusResult()
	status := Execute(context.Background(), application, []string{"status"})
	if status != 2 {
		t.Fatalf("status = %d, want 2", status)
	}
}

func testExecuteHook(t *testing.T) {
	application, fakes, _, _ := rootFixture(t)
	fakes.apply.err = failure.New(failure.Hook, "apply: hooks failed", nil)
	status := Execute(context.Background(), application, []string{"apply"})
	if status != 3 {
		t.Fatalf("status = %d, want 3", status)
	}
}

func testExecuteDependency(t *testing.T) {
	application, fakes, _, _ := rootFixture(t)
	fakes.apply.err = failure.New(failure.Dependency, "apply: sops missing", nil)
	status := Execute(context.Background(), application, []string{"apply"})
	if status != 4 {
		t.Fatalf("status = %d, want 4", status)
	}
}

func testExecuteSignal(t *testing.T) {
	application, fakes, _, _ := rootFixture(t)
	interruption := failure.NewInterruption(failure.Interrupt)
	fakes.apply.err = errors.Join(failure.New(failure.Hook, "apply: hooks failed", nil), interruption)
	status := Execute(context.Background(), application, []string{"apply"})
	if status != 130 {
		t.Fatalf("status = %d, want 130", status)
	}
}

func testExecuteTerminate(t *testing.T) {
	application, fakes, _, _ := rootFixture(t)
	fakes.apply.err = fmt.Errorf("wrapped: %w", failure.NewInterruption(failure.Terminate))
	status := Execute(context.Background(), application, []string{"apply"})
	if status != 143 {
		t.Fatalf("status = %d, want 143", status)
	}
}

func testExecuteSecondUse(t *testing.T) {
	application, _, _, stderr := rootFixture(t)
	first := Execute(context.Background(), application, []string{"version"})
	second := Execute(context.Background(), application, []string{"version"})
	if first != 0 || second != 1 {
		t.Fatalf("statuses = %d %d, want 0 then 1", first, second)
	}
	if !strings.Contains(stderr.String(), "already executed") {
		t.Fatalf("stderr = %q, want the second-use diagnostic", stderr.String())
	}
}

func testExecuteSilence(t *testing.T) {
	application, _, stdout, stderr := rootFixture(t)
	status := Execute(context.Background(), application, []string{"nonsense"})
	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	if !strings.Contains(stderr.String(), "unknown command") && !strings.Contains(stderr.String(), "nonsense") {
		t.Fatalf("stderr = %q, want the single diagnostic", stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want no usage dump on stdout", stdout.String())
	}
}
