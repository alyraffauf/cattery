package add

import (
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestAddInference(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"unmanaged root base ordinary", testInferRootBaseOrdinary},
		{"unmanaged dot tree is representable", testInferDotTree},
		{"unmanaged root base secret", testInferRootBaseSecret},
		{"unmanaged group ordinary", testInferGroupOrdinary},
		{"unmanaged platform layer", testInferPlatformLayer},
		{"unmanaged group platform secret", testInferGroupPlatformSecret},
		{"managed adopts owner", testInferManagedAdopts},
		{"managed rejects conflicting group", testInferManagedConflictGroup},
		{"managed rejects conflicting platform", testInferManagedConflictPlatform},
		{"alias target rejected", testInferAliasRejected},
		{"underscore target rejected", testInferUnderscoreRejected},
		{"metadata name rejected", testInferMetadataRejected},
		{"multi-segment non-dot rejected", testInferMultiSegmentRejected},
		{"platform must equal runtime", testInferPlatformMismatch},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testInferRootBaseOrdinary(t *testing.T) {
	item, err := inferOneCase(t, request{}, ".bashrc")
	if err != nil {
		t.Fatal(err)
	}
	assertItem(t, item, expectedItem{
		scope: deployment.NewScope(""), layer: deployment.LayerBase,
		kind: deployment.FileOrdinary, source: ".bashrc",
	})
}

func testInferDotTree(t *testing.T) {
	item, err := inferOneCase(t, request{}, ".config/app/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	assertItem(t, item, expectedItem{
		scope: deployment.NewScope(""), layer: deployment.LayerBase,
		kind: deployment.FileOrdinary, source: ".config/app/config.toml",
	})
}

func testInferRootBaseSecret(t *testing.T) {
	item, err := inferOneCase(t, request{SecretSet: true, Secret: true}, ".aws/creds")
	if err != nil {
		t.Fatal(err)
	}
	assertItem(t, item, expectedItem{
		scope: deployment.NewScope(""), layer: deployment.LayerBase,
		kind: deployment.FileSecret, source: "_secrets/.aws/creds",
	})
}

func testInferGroupOrdinary(t *testing.T) {
	item, err := inferOneCase(t, request{GroupSet: true, Group: "atuin"}, "config.toml")
	if err != nil {
		t.Fatal(err)
	}
	assertItem(t, item, expectedItem{
		scope: deployment.NewScope("atuin"), layer: deployment.LayerBase,
		kind: deployment.FileOrdinary, source: "atuin/config.toml",
	})
}

func testInferPlatformLayer(t *testing.T) {
	item, err := inferOneCase(t, request{PlatformSet: true, Platform: "linux"}, "bin/tool")
	if err != nil {
		t.Fatal(err)
	}
	assertItem(t, item, expectedItem{
		scope: deployment.NewScope(""), layer: deployment.LayerLinux,
		kind: deployment.FileOrdinary, source: "_linux/bin/tool",
	})
}

func testInferGroupPlatformSecret(t *testing.T) {
	item, err := inferOneCase(t,
		request{GroupSet: true, Group: "atuin", PlatformSet: true, Platform: "linux",
			SecretSet: true, Secret: true}, "db")
	if err != nil {
		t.Fatal(err)
	}
	assertItem(t, item, expectedItem{
		scope: deployment.NewScope("atuin"), layer: deployment.LayerLinux,
		kind: deployment.FileSecret, source: "atuin/_linux/_secrets/db",
	})
}

func testInferManagedAdopts(t *testing.T) {
	plan := planWith(t, managedRoot(t, ".vimrc", deployment.FileOrdinary), nil)
	context := inferContext{
		identity: RepositoryIdentity{Root: "/repo", Home: "/home/user"},
		plan:     plan, platform: deployment.LayerLinux,
		targets: []string{"/home/user/.vimrc"},
	}
	items, err := Infer(context, request{})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].SourceRepositoryPath != ".vimrc" {
		t.Fatalf("source = %q, want the owner path .vimrc", items[0].SourceRepositoryPath)
	}
}

func testInferManagedConflictGroup(t *testing.T) {
	plan := planWith(t, managedRoot(t, ".vimrc", deployment.FileOrdinary), nil)
	context := inferContext{
		identity: RepositoryIdentity{Root: "/repo", Home: "/home/user"},
		plan:     plan, platform: deployment.LayerLinux,
		targets: []string{"/home/user/.vimrc"},
	}
	if _, err := Infer(context, request{GroupSet: true, Group: "other"}); err == nil {
		t.Fatal("managed owner accepted a conflicting group")
	}
}

func testInferManagedConflictPlatform(t *testing.T) {
	plan := planWith(t, managedRoot(t, ".vimrc", deployment.FileOrdinary), nil)
	context := inferContext{
		identity: RepositoryIdentity{Root: "/repo", Home: "/home/user"},
		plan:     plan, platform: deployment.LayerLinux,
		targets: []string{"/home/user/.vimrc"},
	}
	if _, err := Infer(context, request{PlatformSet: true, Platform: "linux"}); err == nil {
		t.Fatal("managed base owner accepted a conflicting platform")
	}
}

func testInferAliasRejected(t *testing.T) {
	alias, err := deployment.NewAlias(deployment.Alias{
		Scope: deployment.NewScope(""), Platform: "linux",
		AliasRelativePath: "readme", CanonicalTargetRelativePath: "README.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := planWith(t, nil, []deployment.Alias{alias})
	context := inferContext{
		identity: RepositoryIdentity{Root: "/repo", Home: "/home/user"},
		plan:     plan, platform: deployment.LayerLinux,
		targets: []string{"/home/user/readme"},
	}
	_, err = Infer(context, request{})
	if err == nil {
		t.Fatal("alias target was accepted")
	}
}

func testInferUnderscoreRejected(t *testing.T) {
	if _, err := inferOneCase(t, request{}, "_secrets/secret"); err == nil {
		t.Fatal("underscore target was accepted")
	}
}

func testInferMetadataRejected(t *testing.T) {
	if _, err := inferOneCase(t, request{}, ".gitignore"); err == nil {
		t.Fatal("metadata name was accepted")
	}
}

func testInferMultiSegmentRejected(t *testing.T) {
	if _, err := inferOneCase(t, request{}, "bin/tool"); err == nil {
		t.Fatal("root base multi-segment non-dot target was accepted")
	}
}

func testInferPlatformMismatch(t *testing.T) {
	context := inferContext{
		identity: RepositoryIdentity{Root: "/repo", Home: "/home/user"},
		plan:     planWith(t, nil, nil), platform: deployment.LayerLinux,
		targets: []string{"/home/user/bin/tool"},
	}
	if _, err := Infer(context, request{PlatformSet: true, Platform: "darwin"}); err == nil {
		t.Fatal("cross-platform add was accepted")
	}
}

// inferOneCase runs Infer for one unmanaged target with an empty plan.
func inferOneCase(t *testing.T, request request, relative string) (ItemPlanInput, error) {
	t.Helper()
	context := inferContext{
		identity: RepositoryIdentity{Root: "/repo", Home: "/home/user"},
		plan:     planWith(t, nil, nil), platform: deployment.LayerLinux,
		targets: []string{"/home/user/" + relative},
	}
	items, err := Infer(context, request)
	if err != nil {
		return ItemPlanInput{}, err
	}
	return items[0], nil
}

// request re-exports Request for table brevity; the field set mirrors the
// presence bits the CLI captures.
type request = Request

// expectedItem bundles the inferred fields one assertion checks.
type expectedItem struct {
	scope  deployment.Scope
	layer  deployment.Layer
	kind   deployment.FileKind
	source string
}

func assertItem(t *testing.T, item ItemPlanInput, want expectedItem) {
	t.Helper()
	if item.Scope != want.scope {
		t.Fatalf("scope = %v, want %v", item.Scope, want.scope)
	}
	if item.Layer != want.layer {
		t.Fatalf("layer = %v, want %v", item.Layer, want.layer)
	}
	if item.Kind != want.kind {
		t.Fatalf("kind = %v, want %v", item.Kind, want.kind)
	}
	if item.SourceRepositoryPath != want.source {
		t.Fatalf("source = %q, want %q", item.SourceRepositoryPath, want.source)
	}
}

func planWith(t *testing.T, files []deployment.ManagedFile, aliases []deployment.Alias) deployment.Plan {
	t.Helper()
	plan, err := deployment.NewPlan(deployment.PlanInput{
		RepositoryRoot: "/repo", Platform: "linux", Files: files, Aliases: aliases,
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func managedRoot(t *testing.T, target string, kind deployment.FileKind) []deployment.ManagedFile {
	t.Helper()
	file, err := deployment.NewManagedFile(deployment.ManagedFile{
		Scope: deployment.NewScope(""), Layer: deployment.LayerBase, Kind: kind,
		SourceAbsolutePath: "/repo/" + target, SourceRepositoryPath: target,
		TargetRelativePath: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	return []deployment.ManagedFile{file}
}
