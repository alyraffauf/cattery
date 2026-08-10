package integration

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/add"
	"github.com/alyraffauf/cattery/internal/bootstrap"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/selection"
	"github.com/alyraffauf/cattery/internal/state"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestBackendAdd(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"ordinary adoption", testAddOrdinary},
		{"dry run writes nothing", testAddDryRun},
		{"explicit group", testAddGroup},
		{"secret adoption", testAddSecret},
		{"partial batch", testAddPartial},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// addRequest freezes one add over the fixture default repository.
func addRequest(fixture BackendFixture, targets ...string) add.Request {
	return add.Request{Repository: add.RepositoryInput{WorkingDir: fixture.Home}, Targets: targets}
}

// readRepository reads one repository-relative source.
func readRepository(t *testing.T, fixture BackendFixture, relative string) []byte {
	t.Helper()
	content, err := os.ReadFile(fixture.RepositoryPath(relative))
	if err != nil {
		t.Fatalf("read source %s: %v", relative, err)
	}
	return content
}

func testAddOrdinary(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	fixture.WriteTarget(t, "a.conf", []byte("content"))
	result, err := fixture.Applications.Add.Add(context.Background(), addRequest(fixture, "a.conf"))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if result.Summary.Completed != 1 {
		t.Fatalf("summary = %+v, want one completed", result.Summary)
	}
	if string(readRepository(t, fixture, "a.conf")) != "content" {
		t.Fatal("the repository must carry the exact target bytes")
	}
	if string(readTarget(t, fixture, "a.conf")) != "content" {
		t.Fatal("the target must be preserved")
	}
	row, err := fixture.Store.FileBaseline(fixture.Repository, fixture.Home, "a.conf")
	if err != nil {
		t.Fatalf("row: %v", err)
	}
	if row.Status != state.StatusActive {
		t.Fatalf("row = %+v, want an active baseline", row)
	}
}

func testAddDryRun(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	fixture.WriteTarget(t, "a.conf", []byte("content"))
	request := addRequest(fixture, "a.conf")
	request.DryRun = true
	result, err := fixture.Applications.Add.Add(context.Background(), request)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if result.Summary.Planned != 1 {
		t.Fatalf("summary = %+v, want one planned", result.Summary)
	}
	if _, err := os.Stat(fixture.RepositoryPath("a.conf")); !os.IsNotExist(err) {
		t.Fatal("a dry run must not write the source")
	}
	if _, err := fixture.Store.FileBaseline(fixture.Repository, fixture.Home, "a.conf"); err == nil {
		t.Fatal("a dry run must not establish a baseline")
	}
}

func testAddGroup(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	fixture.WriteTarget(t, "app.conf", []byte("content"))
	request := addRequest(fixture, "app.conf")
	request.Group = "apps"
	request.GroupSet = true
	result, err := fixture.Applications.Add.Add(context.Background(), request)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if result.Summary.Completed != 1 {
		t.Fatalf("summary = %+v, want one completed", result.Summary)
	}
	if string(readRepository(t, fixture, "apps/app.conf")) != "content" {
		t.Fatal("a group source must live under the group directory")
	}
}

func testAddSecret(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	envelope := []byte(`{"data":"ZmFrZS1jaXBoZXI=","sops":{"version":"3.9.0"}}`)
	fixture.WriteTarget(t, "token", envelope)
	adapters := fixture.Adapters
	adapters.SOPS = fakeSOPSClient(t)
	service := bootstrap.BuildApplications(bootstrap.ApplicationsInput{
		Adapters: adapters, Home: fixture.Home, Platform: fixture.Platform,
		Protected: []string{fixture.StateHome},
		Stdin:     strings.NewReader(""), Stderr: io.Discard, IsTerminal: func(int) bool { return false },
	}).Add
	request := addRequest(fixture, "token")
	request.Secret = true
	request.SecretSet = true
	result, err := service.Add(context.Background(), request)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if result.Summary.Completed != 1 || !result.Items[0].Secret {
		t.Fatalf("summary = %+v, want one completed secret", result.Summary)
	}
	if string(readRepository(t, fixture, "_secrets/token")) != string(envelope) {
		t.Fatal("the repository must carry the ciphertext only")
	}
}

func testAddPartial(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	fixture.WriteTarget(t, "a.conf", []byte("a"))
	fixture.WriteTarget(t, "b.conf", []byte("b"))
	service := add.NewService(add.Dependencies{
		RepositorySource: addSource{resolver: selection.NewRepositoryResolver(fixture.Home, fixture.Store)},
		Compiler:         compileAdapter{},
		Writer:           &partialWriter{inner: filesystem.NewReplacer()},
		Baselines:        fixture.Store,
	})
	result, err := service.Add(context.Background(), addRequest(fixture, "a.conf", "b.conf"))
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("error = %v, want an operational failure", err)
	}
	if len(result.Items) != 2 || result.Items[0].Status != add.StatusCompleted || result.Items[1].Status != add.StatusPartial {
		t.Fatalf("items = %+v, want completed then partial", result.Items)
	}
}

// addSource adapts one selection resolver into the add identity.
type addSource struct {
	resolver *selection.RepositoryResolver
}

func (source addSource) Resolve(request selection.RepositoryRequest) (add.RepositoryIdentity, error) {
	repository, err := source.resolver.Resolve(request)
	if err != nil {
		return add.RepositoryIdentity{}, err
	}
	return add.RepositoryIdentity{Root: repository.RootPath, Home: repository.HomePath}, nil
}

// compileAdapter runs the frozen repository compiler.
type compileAdapter struct{}

func (compileAdapter) Compile(input repository.CompileInput) (deployment.Plan, error) {
	return repository.Compile(input)
}

// partialWriter fails the second replacement.
type partialWriter struct {
	inner *filesystem.Replacer
	calls int
}

func (writer *partialWriter) ReplaceResult(ctx context.Context, precondition filesystem.Precondition, spec filesystem.ReplacementSpec) (filesystem.ReplaceResult, error) {
	writer.calls++
	if writer.calls == 2 {
		return filesystem.ReplaceResult{}, errors.New("injected write failure")
	}
	return writer.inner.ReplaceResult(ctx, precondition, spec)
}

// fakeSOPSClient builds one client over the fake sops executable.
func fakeSOPSClient(t *testing.T) *secrets.Client {
	t.Helper()
	executable := sops.Build(t)
	command, err := executable.Command(sops.Behavior{Stdout: []byte(`{"data":"ZmFrZS1jaXBoZXI=","sops":{"version":"3.9.0"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	return secrets.NewClient(executable.Path, filepath.Dir(executable.Path), command.Env)
}

// kindIs reports whether err carries the given failure kind.
func kindIs(err error, want failure.Kind) bool {
	kind, ok := failure.HasKind(err)
	return ok && kind == want
}
