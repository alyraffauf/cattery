package failure

import (
	"errors"
	"fmt"
	"os"
)

// Kind categorizes a failure for the CLI exit mapper. It carries no numeric
// status so this package never depends on CLI or exit-code concepts.
type Kind string

// The five presentation-neutral categories.
const (
	InvalidInput Kind = "InvalidInput"
	Operational  Kind = "Operational"
	Difference   Kind = "Difference"
	Hook         Kind = "Hook"
	Dependency   Kind = "Dependency"
)

// Error is a categorized failure wrapping an optional cause with stable,
// safe-path context. Cause may be nil for leaf failures.
type Error struct {
	Kind    Kind
	Message string
	Cause   error
}

// Error renders a stable single-line message and delegates to the cause so
// errors.Is and errors.As traverse the wrapped chain.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// Unwrap exposes the wrapped cause to errors.Is and errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// New constructs a categorized failure. Pass nil for cause when the failure
// is a leaf; pass a lower-level error to preserve its chain.
func New(kind Kind, message string, cause error) *Error {
	return &Error{Kind: kind, Message: message, Cause: cause}
}

// FromPathError categorizes a compile failure: filesystem path errors are
// operational, while repository validation errors are invalid input.
func FromPathError(message string, cause error) *Error {
	var pathError *os.PathError
	if errors.As(cause, &pathError) {
		return New(Operational, message, cause)
	}
	return New(InvalidInput, message, cause)
}

// HasKind walks an error chain and any errors.Join group, returning the first
// categorized failure's kind. The empty kind and false mean no failure.Error
// is present.
func HasKind(err error) (Kind, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target.Kind, true
	}
	return "", false
}

// Signal names the cancellation cause that interrupted the process. The CLI
// maps these to process termination statuses; no numeric value lives here.
type Signal string

// The two cancellation causes.
const (
	Interrupt Signal = "Interrupt"
	Terminate Signal = "Terminate"
)

// Interruption is the context cancellation cause set when the process
// receives SIGINT or SIGTERM. It is detected with errors.As on context.Cause.
type Interruption struct {
	Signal Signal
}

// Error reports which signal interrupted the process.
func (i *Interruption) Error() string {
	if i == nil {
		return "interruption"
	}
	return fmt.Sprintf("interrupted by %s", i.Signal)
}

// NewInterruption builds a cancellation cause for the given signal.
func NewInterruption(signal Signal) *Interruption {
	return &Interruption{Signal: signal}
}
