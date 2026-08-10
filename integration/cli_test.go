package integration

import (
	"strings"
	"testing"
	"time"
)

func TestExecutableCLI(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"root help", testCLIHelp},
		{"no command shows help", testCLINoCommand},
		{"unknown command fails", testCLIUnknownCommand},
		{"unknown root flag", testCLIUnknownFlag},
		{"version output", testCLIVersion},
		{"version flag rejected", testCLIVersionFlag},
		{"init arity", testCLIInitArity},
		{"validate usage", testCLIValidate},
		{"deterministic output", testCLIDeterministic},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// cliFixture builds the executable fixture once per test.
func cliFixture(t *testing.T) ProcessFixture {
	t.Helper()
	return NewProcessFixture(t)
}

// cliRun runs the executable under a fresh isolated home.
func cliRun(t *testing.T, fixture ProcessFixture, args ...string) ProcessResult {
	t.Helper()
	return fixture.Run(t, ProcessInput{Args: args, Home: t.TempDir(), Timeout: 30 * time.Second})
}

func testCLIHelp(t *testing.T) {
	fixture := cliFixture(t)
	result := cliRun(t, fixture, "--help")
	if result.Code != 0 || !strings.Contains(result.Stdout, "cattery") {
		t.Fatalf("result = %+v, want help on stdout", result)
	}
}

func testCLINoCommand(t *testing.T) {
	fixture := cliFixture(t)
	result := cliRun(t, fixture)
	if result.Code != 0 || !strings.Contains(result.Stdout, "cattery") {
		t.Fatalf("result = %+v, want help without arguments", result)
	}
}

func testCLIUnknownCommand(t *testing.T) {
	fixture := cliFixture(t)
	result := cliRun(t, fixture, "nonsense")
	if result.Code != 1 || result.Stderr == "" {
		t.Fatalf("result = %+v, want a usage failure", result)
	}
}

func testCLIUnknownFlag(t *testing.T) {
	fixture := cliFixture(t)
	result := cliRun(t, fixture, "--version")
	if result.Code != 1 || result.Stderr == "" {
		t.Fatalf("result = %+v, want an unknown-flag failure", result)
	}
}

func testCLIVersion(t *testing.T) {
	fixture := cliFixture(t)
	result := cliRun(t, fixture, "version")
	if result.Code != 0 {
		t.Fatalf("result = %+v, want success", result)
	}
	if !strings.HasPrefix(result.Stdout, "cattery dev commit=unknown built=unknown go=") {
		t.Fatalf("stdout = %q, want the development version line", result.Stdout)
	}
	if !strings.HasSuffix(result.Stdout, "\n") {
		t.Fatal("the version line must end with a newline")
	}
}

func testCLIVersionFlag(t *testing.T) {
	fixture := cliFixture(t)
	result := cliRun(t, fixture, "--version")
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for the unknown --version flag", result.Code)
	}
}

func testCLIInitArity(t *testing.T) {
	fixture := cliFixture(t)
	result := cliRun(t, fixture, "init", "a", "b")
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for an arity error", result.Code)
	}
}

func testCLIValidate(t *testing.T) {
	fixture := cliFixture(t)
	result := cliRun(t, fixture, "validate")
	if result.Code == 0 {
		t.Fatal("validate without a repository must fail")
	}
	if result.Stdout != "" {
		t.Fatalf("stdout = %q, want no count lines without a repository", result.Stdout)
	}
}

func testCLIDeterministic(t *testing.T) {
	fixture := cliFixture(t)
	first := cliRun(t, fixture, "version")
	second := cliRun(t, fixture, "version")
	if first.Stdout != second.Stdout || first.Code != second.Code {
		t.Fatal("repeated runs must produce identical output")
	}
}
