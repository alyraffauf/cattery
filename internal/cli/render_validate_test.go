package cli

import (
	"bytes"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/validate"
)

func TestValidateRenderer(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"two count lines", testRenderTwoLines},
		{"deterministic order", testRenderOrder},
		{"writer failure", testRenderWriterFailure},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testRenderTwoLines(t *testing.T) {
	stdout := &bytes.Buffer{}
	if err := renderValidate(stdout, validate.Result{Platforms: []validate.PlatformCount{
		{Platform: "linux", Files: 2, Secrets: 1, Aliases: 0, Groups: 3},
	}}); err != nil {
		t.Fatalf("render: %v", err)
	}
	if stdout.String() != "linux files=2 secrets=1 aliases=0 groups=3\n" {
		t.Fatalf("stdout = %q, want one count line", stdout.String())
	}
}

func testRenderOrder(t *testing.T) {
	stdout := &bytes.Buffer{}
	if err := renderValidate(stdout, validate.Result{Platforms: []validate.PlatformCount{
		{Platform: "linux", Files: 1},
		{Platform: "darwin", Files: 2},
	}}); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := stdout.String()
	want := "linux files=1 secrets=0 aliases=0 groups=0\ndarwin files=2 secrets=0 aliases=0 groups=0\n"
	if got != want {
		t.Fatalf("stdout = %q, want the given sorted order", got)
	}
}

func testRenderWriterFailure(t *testing.T) {
	if err := renderValidate(failingWriter{}, validate.Result{Platforms: []validate.PlatformCount{
		{Platform: "linux"},
	}}); err == nil {
		t.Fatal("a writer failure must surface")
	}
}
