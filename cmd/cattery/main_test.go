package main

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/cli"
	"github.com/alyraffauf/cattery/internal/failure"
)

func TestMainBoundary(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"interrupt is 130", testBoundaryInterrupt},
		{"terminate is 143", testBoundaryTerminate},
		{"uninterrupted passes through", testBoundaryPassthrough},
		{"state home derivation", testBoundaryStateHome},
		{"streams and environment forward", testBoundaryForwarding},
		{"static imports constrained", testBoundaryImports},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testBoundaryInterrupt(t *testing.T) {
	if code := signalCode(failure.NewInterruption(failure.Interrupt)); code != 130 {
		t.Fatalf("code = %d, want 130", code)
	}
	if code := signalCode(errors.New("other")); code != 130 {
		t.Fatalf("unknown cause code = %d, want 130", code)
	}
}

func testBoundaryTerminate(t *testing.T) {
	if code := signalCode(failure.NewInterruption(failure.Terminate)); code != 143 {
		t.Fatalf("code = %d, want 143", code)
	}
}

func testBoundaryPassthrough(t *testing.T) {
	application := stubApplication(t)
	if code := exitCode(context.Background(), application, nil); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
}

func testBoundaryStateHome(t *testing.T) {
	got := stateHomeOf([]string{"HOME=/home/user", "XDG_STATE_HOME=/state"})
	if got != "/state" {
		t.Fatalf("state base = %q, want /state", got)
	}
	got = stateHomeOf([]string{"HOME=/home/user"})
	if got != "/home/user/.local/state" {
		t.Fatalf("state base = %q, want the home fallback", got)
	}
	if got := stateHomeOf(nil); got != "" {
		t.Fatalf("state base = %q, want empty without HOME", got)
	}
}

func testBoundaryForwarding(t *testing.T) {
	application := stubApplication(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(failure.NewInterruption(failure.Interrupt))
	if code := exitCode(ctx, application, nil); code != 130 {
		t.Fatalf("code = %d, want 130 for an interrupted context", code)
	}
}

func testBoundaryImports(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		if strings.Contains(path, "cobra") || strings.Contains(path, "x/term") {
			t.Fatalf("main.go must not import %q", path)
		}
	}
}

// stubApplication builds one inert application for boundary tests.
func stubApplication(t *testing.T) *cli.Application {
	t.Helper()
	return cli.NewApplication(cli.Dependencies{}, cli.NewRuntime(cli.RuntimeInput{}))
}
