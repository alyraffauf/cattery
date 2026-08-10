package integration

import (
	"context"
	"os"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/inspect"
	"github.com/alyraffauf/cattery/internal/bootstrap"
)

func TestBackendInspect(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"converged status", testInspectConverged},
		{"drift reports difference", testInspectDrift},
		{"status and diff parity", testInspectParity},
		{"state-only retirement", testInspectRetired},
		{"secret records stay safe", testInspectSecret},
		{"inspection mutates nothing", testInspectImmutable},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// inspectRequest freezes one inspection over the fixture repository.
func inspectRequest(fixture BackendFixture) inspect.Request {
	return inspect.Request{Repository: inspect.RepositoryInput{WorkingDir: fixture.Home}}
}

// removeRepositoryFile removes one repository-relative path.
func removeRepositoryFile(fixture BackendFixture, relative string) error {
	return os.Remove(fixture.RepositoryPath(relative))
}

// DiffTagNameOf returns the stable tag name of one diff record.
func DiffTagNameOf(record inspect.DiffRecord) string {
	return inspect.DiffTagName(record)
}

// buildApplications wires a fresh application over the given adapters.
func buildApplications(t *testing.T, fixture BackendFixture, adapters bootstrap.Adapters) bootstrap.Applications {
	t.Helper()
	return bootstrap.BuildApplications(bootstrap.ApplicationsInput{
		Adapters:   adapters,
		Home:       fixture.Home,
		Platform:   fixture.Platform,
		Protected:  []string{fixture.StateHome},
		Stdin:      os.Stdin,
		Stderr:     os.Stderr,
		IsTerminal: func(fd int) bool { return false },
	})
}

func testInspectConverged(t *testing.T) {
	fixture, _ := appliedFixture(t, ".config/app", "v1")
	result, err := fixture.Applications.Inspect.Status(context.Background(), inspectRequest(fixture))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !result.Converged() || len(result.Records()) != 0 {
		t.Fatalf("result = %+v, want a converged empty status", result)
	}
}

func testInspectDrift(t *testing.T) {
	fixture, _ := appliedFixture(t, ".config/app", "v1")
	fixture.WriteTarget(t, ".config/app", []byte("drifted"))
	result, err := fixture.Applications.Inspect.Status(context.Background(), inspectRequest(fixture))
	if err == nil {
		t.Fatal("drift must report a difference failure")
	}
	if len(result.Records()) != 1 || result.Records()[0].TargetPath() != ".config/app" {
		t.Fatalf("records = %+v, want the drifted record", result.Records())
	}
	if result.Converged() {
		t.Fatal("drift must not converge")
	}
}

func testInspectParity(t *testing.T) {
	fixture, _ := appliedFixture(t, ".config/app", "v1")
	fixture.WriteTarget(t, ".config/app", []byte("drifted"))
	status, statusErr := fixture.Applications.Inspect.Status(context.Background(), inspectRequest(fixture))
	diff, diffErr := fixture.Applications.Inspect.Diff(context.Background(), inspectRequest(fixture))
	if (statusErr == nil) != (diffErr == nil) {
		t.Fatalf("status error = %v, diff error = %v, want parity", statusErr, diffErr)
	}
	if len(status.Records()) != len(diff.Records()) {
		t.Fatalf("records = %d/%d, want parity", len(status.Records()), len(diff.Records()))
	}
	if status.Records()[0].TargetPath() != diff.Records()[0].TargetPath() {
		t.Fatalf("paths = %q/%q, want the same evaluation", status.Records()[0].TargetPath(), diff.Records()[0].TargetPath())
	}
}

func testInspectRetired(t *testing.T) {
	fixture, _ := appliedFixture(t, ".config/app", "v1")
	if err := removeRepositoryFile(fixture, ".config/app"); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.Applications.Inspect.Status(context.Background(), inspectRequest(fixture))
	if err == nil {
		t.Fatal("a pending retirement must report a difference")
	}
	if len(result.Records()) != 1 || result.Records()[0].Kind() != inspect.StatusKindRetired {
		t.Fatalf("records = %+v, want one retired record", result.Records())
	}
}

func testInspectSecret(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	envelope := []byte(`{"data":"ZmFrZS1jaXBoZXI=","sops":{"version":"3.9.0"}}`)
	fixture.WriteRepository(t, "_secrets/token", envelope)
	fixture.WriteTarget(t, "token", []byte("tampered"))
	adapters := fixture.Adapters
	adapters.SOPS = fakeSOPSClient(t)
	applications := buildApplications(t, fixture, adapters)
	result, err := applications.Inspect.Diff(context.Background(), inspectRequest(fixture))
	if err == nil {
		t.Fatal("a drifted secret must report a difference")
	}
	if len(result.Records()) != 1 {
		t.Fatalf("records = %+v, want one record", result.Records())
	}
	record := result.Records()[0]
	if DiffTagNameOf(record) != "secret" {
		t.Fatalf("tag = %q, want secret", DiffTagNameOf(record))
	}
	if record.Lines() != "" || record.SourceSize() != 0 {
		t.Fatalf("record = %+v, a secret record must stay payload-free", record)
	}
}

func testInspectImmutable(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	fixture.WriteRepository(t, ".config/app", []byte("v1"))
	fixture.WriteTarget(t, ".config/app", []byte("drifted"))
	before := fixture.Store.Database()
	if _, err := fixture.Applications.Inspect.Status(context.Background(), inspectRequest(fixture)); err == nil {
		t.Fatal("drift must report a difference")
	}
	if _, err := fixture.Applications.Inspect.Diff(context.Background(), inspectRequest(fixture)); err == nil {
		t.Fatal("drift must report a difference")
	}
	if fixture.Store.Database() != before {
		t.Fatal("inspection must not touch the store")
	}
	if string(readTarget(t, fixture, ".config/app")) != "drifted" {
		t.Fatal("inspection must not mutate targets")
	}
}
