package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/selection"
)

func TestValidateService(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"maps raw repository fields onto the selection request", testServiceMapsRequest},
		{"reports exactly two sorted records for the selection", testServiceCounts},
		{"source failures are invalid input", testServiceSourceFailure},
		{"unknown and duplicate groups are invalid input", testServiceGroupFailures},
		{"invalid unselected scopes still fail", testServiceInvalidUnselectedScope},
		{"secrets must be nonempty valid JSON", testServiceSecretShape},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

type fixture struct {
	service *Service
	source  *fakeSource
	root    string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	source := &fakeSource{identity: RepositoryIdentity{Root: root, Home: filepath.Join(root, "home")}}
	service := NewService(Dependencies{
		RepositorySource: source,
		Compiler:         compileFunc(repository.Compile),
		ProtectedTrees:   []string{filepath.Join(root, "state")},
	})
	return &fixture{service: service, source: source, root: root}
}

// compileFunc adapts the package compiler function to the narrow port.
type compileFunc func(repository.CompileInput) (deployment.Plan, error)

func (adapter compileFunc) Compile(input repository.CompileInput) (deployment.Plan, error) {
	return adapter(input)
}

type fakeSource struct {
	identity RepositoryIdentity
	last     selection.RepositoryRequest
	calls    int
	fail     error
}

func (fake *fakeSource) Resolve(request selection.RepositoryRequest) (RepositoryIdentity, error) {
	fake.calls++
	fake.last = request
	if fake.fail != nil {
		return RepositoryIdentity{}, fake.fail
	}
	return fake.identity, nil
}

// repositoryTree creates a repository with one ordinary file and one valid
// secret per group plus a root _secrets tree.
func repositoryTree(t *testing.T, groups []string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "_secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "_secrets", "root-secret"), `{"root": true}`)
	for _, group := range groups {
		if err := os.MkdirAll(filepath.Join(root, group), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, group, "_secrets"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(root, group, group+"-file"), "x")
		writeFile(t, filepath.Join(root, group, "_secrets", group+"-secret"), `{"data": 1}`)
	}
	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
func assertInvalidInput(t *testing.T, err error) {
	t.Helper()
	if kind, matched := failure.HasKind(err); !matched || kind != failure.InvalidInput {
		t.Fatalf("error = %v, want InvalidInput", err)
	}
}

func testServiceMapsRequest(t *testing.T) {
	fx := newFixture(t)
	request := Request{Repository: RepositoryInput{
		RawExplicit: filepath.Join(fx.root, "repo"), ExplicitSet: true,
		RawEnv: "/ignored", EnvSet: true, WorkingDir: "/work",
	}}
	if _, err := fx.service.Validate(context.Background(), request); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	want := selection.RepositoryRequest{
		RawExplicit: filepath.Join(fx.root, "repo"), ExplicitSet: true,
		RawEnv: "/ignored", EnvSet: true, WorkingDir: "/work",
	}
	if fx.source.calls != 1 || fx.source.last != want {
		t.Fatalf("resolve = %d calls, last %+v; want one mapped request", fx.source.calls, fx.source.last)
	}
}

func testServiceCounts(t *testing.T) {
	fx := newFixture(t)
	root := repositoryTree(t, []string{"g1", "g2"})
	fx.source.identity = RepositoryIdentity{Root: root, Home: fx.source.identity.Home}
	result, err := fx.service.Validate(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	want := []PlatformCount{
		{Platform: "darwin", Files: 5, Secrets: 3, Groups: 2},
		{Platform: "linux", Files: 5, Secrets: 3, Groups: 2},
	}
	if !slices.Equal(result.Platforms, want) {
		t.Fatalf("platforms = %v, want %v", result.Platforms, want)
	}
	selected, err := fx.service.Validate(context.Background(), Request{Groups: []string{"g1"}})
	if err != nil {
		t.Fatalf("Validate(g1): %v", err)
	}
	wantSelected := []PlatformCount{
		{Platform: "darwin", Files: 2, Secrets: 1, Groups: 1},
		{Platform: "linux", Files: 2, Secrets: 1, Groups: 1},
	}
	if !slices.Equal(selected.Platforms, wantSelected) {
		t.Fatalf("selected platforms = %v, want %v", selected.Platforms, wantSelected)
	}
	if fx.source.calls != 2 {
		t.Fatalf("resolve calls = %d, want 2", fx.source.calls)
	}
}

func testServiceSourceFailure(t *testing.T) {
	fx := newFixture(t)
	fx.source.fail = errors.New("no default repository")
	_, err := fx.service.Validate(context.Background(), Request{})
	assertInvalidInput(t, err)
	if fx.source.calls != 1 {
		t.Fatalf("resolve invoked %d times, want 1", fx.source.calls)
	}
}

func testServiceGroupFailures(t *testing.T) {
	fx := newFixture(t)
	root := repositoryTree(t, []string{"g1"})
	fx.source.identity = RepositoryIdentity{Root: root, Home: fx.source.identity.Home}
	cases := []struct {
		name   string
		groups []string
	}{
		{"unknown", []string{"ghost"}},
		{"duplicate", []string{"g1", "g1"}},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := fx.service.Validate(context.Background(), Request{Groups: scenario.groups})
			assertInvalidInput(t, err)
		})
	}
}

func testServiceInvalidUnselectedScope(t *testing.T) {
	fx := newFixture(t)
	root := repositoryTree(t, []string{"g1", "g2"})
	fx.source.identity = RepositoryIdentity{Root: root, Home: fx.source.identity.Home}
	writeFile(t, filepath.Join(root, "g2", "_routes.toml"), "not [valid toml")
	_, err := fx.service.Validate(context.Background(), Request{Groups: []string{"g1"}})
	assertInvalidInput(t, err)
}

func testServiceSecretShape(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    failure.Kind
	}{
		{"valid json passes", `{"sops": {}}`, ""},
		{"malformed json fails", "{not json", failure.InvalidInput},
		{"empty storage fails", "", failure.InvalidInput},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			fx := newFixture(t)
			root := repositoryTree(t, []string{"g1", "g2"})
			fx.source.identity = RepositoryIdentity{Root: root, Home: fx.source.identity.Home}
			writeFile(t, filepath.Join(root, "g2", "_secrets", "g2-secret"), scenario.content)
			_, err := fx.service.Validate(context.Background(), Request{Groups: []string{"g1"}})
			if scenario.want == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if kind, matched := failure.HasKind(err); !matched || kind != scenario.want {
				t.Fatalf("error = %v, want %s", err, scenario.want)
			}
		})
	}
	err := checkSecretShape(filepath.Join(t.TempDir(), "absent"))
	if kind, matched := failure.HasKind(err); !matched || kind != failure.Operational {
		t.Fatalf("missing secret error = %v, want Operational", err)
	}
}
