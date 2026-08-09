package failure

import (
	"errors"
	"fmt"
	"testing"
)

func TestFailureContract(t *testing.T) {
	t.Run("new wraps cause and message", func(t *testing.T) {
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
	})

	t.Run("nil cause renders message alone", func(t *testing.T) {
		err := New(InvalidInput, "bad path", nil)
		if err.Error() != "bad path" {
			t.Fatalf("error = %q", err.Error())
		}
		if err.Unwrap() != nil {
			t.Fatal("unwrap of nil cause must be nil")
		}
	})

	t.Run("nil pointer error is empty", func(t *testing.T) {
		var err *Error
		if err.Error() != "" {
			t.Fatalf("nil error = %q", err.Error())
		}
		if err.Unwrap() != nil {
			t.Fatal("nil unwrap must be nil")
		}
	})

	t.Run("hasKind traverses joins", func(t *testing.T) {
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
	})

	t.Run("hasKind false for plain error", func(t *testing.T) {
		if _, ok := HasKind(errors.New("plain")); ok {
			t.Fatal("HasKind must be false for non-failure error")
		}
	})

	t.Run("interruption signal cause", func(t *testing.T) {
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
	})
}
