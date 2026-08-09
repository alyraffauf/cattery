package failure

import (
	"errors"
	"fmt"
	"testing"
)

func TestFailureContract(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"new wraps cause and message", testNewWrapsCause},
		{"nil cause renders message alone", testNilCauseMessage},
		{"nil pointer error is empty", testNilPointerEmpty},
		{"hasKind traverses joins", testHasKindJoins},
		{"hasKind false for plain error", testHasKindPlain},
		{"interruption signal cause", testInterruptionCause},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testNewWrapsCause(t *testing.T) {
	leaf := errors.New("disk full")
	err := New(Operational, "write target", leaf)
	if err.Kind != Operational {
		t.Fatalf("kind = %q, want Operational", err.Kind)
	}
	if !errors.Is(err, leaf) {
		t.Fatal("errors.Is must reach wrapped cause")
	}
	var target *Error
	if !errors.As(err, &target) {
		t.Fatal("errors.As must match *Error")
	}
	if target.Message != "write target" {
		t.Fatalf("message = %q", target.Message)
	}
}

func testNilCauseMessage(t *testing.T) {
	err := New(InvalidInput, "bad path", nil)
	if err.Error() != "bad path" {
		t.Fatalf("error = %q", err.Error())
	}
	if err.Unwrap() != nil {
		t.Fatal("unwrap of nil cause must be nil")
	}
}

func testNilPointerEmpty(t *testing.T) {
	var err *Error
	if err.Error() != "" {
		t.Fatalf("nil error = %q", err.Error())
	}
	if err.Unwrap() != nil {
		t.Fatal("nil unwrap must be nil")
	}
}

func testHasKindJoins(t *testing.T) {
	joined := errors.Join(
		New(Difference, "drift", nil),
		fmt.Errorf("unrelated"),
		New(Hook, "after hook", nil),
	)
	kind, ok := HasKind(joined)
	if !ok {
		t.Fatal("HasKind must find categorized failure in join")
	}
	if kind != Difference {
		t.Fatalf("kind = %q, want first categorized Difference", kind)
	}
}

func testHasKindPlain(t *testing.T) {
	if _, ok := HasKind(errors.New("plain")); ok {
		t.Fatal("HasKind must be false for non-failure error")
	}
}

func testInterruptionCause(t *testing.T) {
	interrupt := NewInterruption(Interrupt)
	var cause *Interruption
	if !errors.As(errors.Join(interrupt), &cause) {
		t.Fatal("errors.As must find Interruption in join")
	}
	if cause.Signal != Interrupt {
		t.Fatalf("signal = %q", cause.Signal)
	}
	if cause.Error() == "" {
		t.Fatal("interruption error must render")
	}
}
