package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLIRuntime(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"stream defaults", testRuntimeStreams},
		{"terminal predicates", testRuntimeTerminal},
		{"verbosity callback", testRuntimeVerbose},
		{"instance isolation", testRuntimeIsolation},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// runtimeFixture assembles one runtime over isolated buffers.
func runtimeFixture(t *testing.T, environment []string, isTerminal func(int) bool) (Runtime, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{
		Streams:    Streams{Stdin: strings.NewReader("input"), Stdout: stdout, Stderr: stderr},
		WorkingDir: "/work", Environment: environment, IsTerminal: isTerminal,
	})
	return runtime, stdout, stderr
}

func testRuntimeStreams(t *testing.T) {
	runtime, stdout, stderr := runtimeFixture(t, []string{"A=1"}, nil)
	if runtime.WorkingDir() != "/work" {
		t.Fatalf("working dir = %q, want /work", runtime.WorkingDir())
	}
	if _, err := runtime.Stdout().Write([]byte("out")); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Stderr().Write([]byte("err")); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "out" || stderr.String() != "err" {
		t.Fatalf("streams = %q %q, want routed output", stdout.String(), stderr.String())
	}
	environment := runtime.Environment()
	if len(environment) != 1 || environment[0] != "A=1" {
		t.Fatalf("environment = %v, want the injected copy", environment)
	}
}

func testRuntimeTerminal(t *testing.T) {
	runtime, _, _ := runtimeFixture(t, nil, func(fd int) bool { return fd == 0 })
	if !runtime.IsTerminal(0) || runtime.IsTerminal(1) {
		t.Fatal("the injected terminal predicate must distinguish descriptors")
	}
	// A nil predicate defaults to the x/term binding.
	runtime, _, _ = runtimeFixture(t, nil, nil)
	if runtime.IsTerminal(3) {
		t.Fatal("an unmapped descriptor must not be a terminal")
	}
}

func testRuntimeVerbose(t *testing.T) {
	levels := []bool{}
	runtime, _, _ := runtimeFixture(t, nil, nil)
	runtime.setVerbose = func(verbose bool) { levels = append(levels, verbose) }
	runtime.SetVerbose(true)
	runtime.SetVerbose(false)
	if len(levels) != 2 || levels[0] != true || levels[1] != false {
		t.Fatalf("levels = %v, want true then false", levels)
	}
}

func testRuntimeIsolation(t *testing.T) {
	first, _, _ := runtimeFixture(t, []string{"A=1", "B=2"}, nil)
	second, _, _ := runtimeFixture(t, []string{"C=3"}, nil)
	environment := first.Environment()
	environment[0] = "mutated"
	if first.Environment()[0] != "A=1" || second.Environment()[0] != "C=3" {
		t.Fatal("runtimes must never share environment state")
	}
}
