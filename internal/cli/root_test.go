package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCobraRoot(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"help without arguments", testRootHelp},
		{"command inventory", testRootInventory},
		{"flags between arguments", testRootFlags},
		{"unknown version flag", testRootUnknownVersion},
		{"zero service calls", testRootZeroCalls},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// rootFakes bundles the recording services of one application.
type rootFakes struct {
	initialize *initServiceFake
	validate   *validateServiceFake
	status     *statusServiceFake
	diff       *diffServiceFake
	add        *addServiceFake
	apply      *applyServiceFake
}

// rootFixture builds one application over recording services.
func rootFixture(t *testing.T) (*Application, *rootFakes, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	fakes := &rootFakes{
		initialize: &initServiceFake{},
		validate:   &validateServiceFake{},
		status:     &statusServiceFake{},
		diff:       &diffServiceFake{},
		add:        &addServiceFake{},
		apply:      &applyServiceFake{},
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{
		Streams: Streams{Stdout: stdout, Stderr: stderr}, WorkingDir: "/work",
	})
	application := NewApplication(Dependencies{
		Initialize: fakes.initialize,
		Validate:   fakes.validate,
		Status:     fakes.status,
		Diff:       fakes.diff,
		Add:        fakes.add,
		Apply:      fakes.apply,
	}, runtime)
	return application, fakes, stdout, stderr
}

// zeroCalls reports whether no service was invoked.
func zeroCalls(fakes *rootFakes) bool {
	return len(fakes.initialize.requests) == 0 && len(fakes.validate.requests) == 0 &&
		len(fakes.status.requests) == 0 && len(fakes.diff.requests) == 0 &&
		len(fakes.add.requests) == 0 && len(fakes.apply.requests) == 0
}

func testRootHelp(t *testing.T) {
	application, fakes, stdout, _ := rootFixture(t)
	if err := application.Execute(context.Background(), nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), "cattery") {
		t.Fatalf("stdout = %q, want the help text", stdout.String())
	}
	if !zeroCalls(fakes) {
		t.Fatal("help must not call any service")
	}
}

func testRootInventory(t *testing.T) {
	application, fakes, stdout, _ := rootFixture(t)
	if err := application.Execute(context.Background(), []string{"--help"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, name := range []string{"init", "validate", "version", "status", "diff", "add", "apply"} {
		if !strings.Contains(stdout.String(), name) {
			t.Fatalf("stdout = %q, want the %s command listed", stdout.String(), name)
		}
	}
	if !zeroCalls(fakes) {
		t.Fatal("help must not call any service")
	}
}

func testRootFlags(t *testing.T) {
	application, fakes, _, _ := rootFixture(t)
	fakes.status.result = statusResult()
	if err := application.Execute(context.Background(), []string{"status", "apps", "-r", "repo", "tools"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	request := fakes.status.requests[0]
	if len(request.Groups) != 2 || request.Groups[0] != "apps" || request.Groups[1] != "tools" {
		t.Fatalf("groups = %v, want the interspersed order", request.Groups)
	}
	if request.Repository.RawExplicit != "repo" || !request.Repository.ExplicitSet {
		t.Fatalf("repository = %+v, want the persistent flag", request.Repository)
	}
}

func testRootUnknownVersion(t *testing.T) {
	application, fakes, _, _ := rootFixture(t)
	if err := application.Execute(context.Background(), []string{"--version"}); err == nil {
		t.Fatal("--version must remain an unknown root flag")
	}
	if !zeroCalls(fakes) {
		t.Fatal("an unknown flag must not call any service")
	}
}

func testRootZeroCalls(t *testing.T) {
	application, fakes, _, _ := rootFixture(t)
	if err := application.Execute(context.Background(), []string{"nonsense"}); err == nil {
		t.Fatal("an unknown command must fail")
	}
	if !zeroCalls(fakes) {
		t.Fatal("an unknown command must not call any service")
	}
}
