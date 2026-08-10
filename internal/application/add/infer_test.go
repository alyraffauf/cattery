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
		{"unmanaged root ordinary", testInferUnmanagedRoot},
		{"explicit group and platform", testInferExplicit},
		{"managed ordinary reused", testInferManagedOrdinary},
		{"managed secret with flag", testInferManagedSecret},
		{"storage class conflict", testInferStorageConflict},
		{"alias target rejected", testInferAlias},
		{"inactive platform rejected", testInferInactivePlatform},
		{"omitted flags default", testInferDefaults},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// inferPlan freezes one platform plan over the given files and aliases.
func inferPlan(t *testing.T, platform string, files []deployment.ManagedFile, aliases []deployment.Alias) deployment.Plan {
	t.Helper()
	plan, err := deployment.NewPlan(deployment.PlanInput{
		RepositoryRoot: "/repo", Platform: platform, Files: files, Aliases: aliases,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return plan
}

// inferFile freezes one managed file entry.
func inferFile(t *testing.T, group, target, source string, kind deployment.FileKind, layer deployment.Layer) deployment.ManagedFile {
	t.Helper()
	file, err := deployment.NewManagedFile(deployment.ManagedFile{
		Scope: deployment.NewScope(group), Layer: layer, Kind: kind,
		SourceAbsolutePath: "/repo/" + source, SourceRepositoryPath: source, TargetRelativePath: target,
	})
	if err != nil {
		t.Fatalf("managed file: %v", err)
	}
	return file
}

// inferAlias freezes one alias entry.
func inferAlias(t *testing.T, aliasPath, canonical string) deployment.Alias {
	t.Helper()
	alias, err := deployment.NewAlias(deployment.Alias{
		Scope: deployment.NewScope(""), Platform: "linux",
		AliasRelativePath: aliasPath, CanonicalTargetRelativePath: canonical,
	})
	if err != nil {
		t.Fatalf("alias: %v", err)
	}
	return alias
}

// inferCheck asserts the frozen ownership of one inference.
func inferCheck(t *testing.T, inference Inference, group, source string, kind deployment.FileKind, layer deployment.Layer) {
	t.Helper()
	if inference.Scope().Group != group {
		t.Fatalf("scope = %q, want %q", inference.Scope().Group, group)
	}
	if inference.SourceRepositoryPath() != source {
		t.Fatalf("source = %q, want %q", inference.SourceRepositoryPath(), source)
	}
	if inference.Kind() != kind {
		t.Fatalf("kind = %v, want %v", inference.Kind(), kind)
	}
	if inference.Layer() != layer {
		t.Fatalf("layer = %v, want %v", inference.Layer(), layer)
	}
}

// inferBase carries the shared inputs of one inference.
func inferBase(t *testing.T, target string) InferInput {
	t.Helper()
	return InferInput{Target: target, Home: "/home", Plan: inferPlan(t, "linux", nil, nil)}
}

func testInferUnmanagedRoot(t *testing.T) {
	inference, err := InferOwnership(inferBase(t, "a.conf"))
	if err != nil {
		t.Fatalf("infer: %v", err)
	}
	inferCheck(t, inference, "", "a.conf", deployment.FileOrdinary, deployment.LayerBase)
	if inference.TargetAbsolutePath() != "/home/a.conf" || inference.SourceAbsolutePath() != "/repo/a.conf" {
		t.Fatalf("paths = %q %q, want canonical joins", inference.TargetAbsolutePath(), inference.SourceAbsolutePath())
	}
}

func testInferExplicit(t *testing.T) {
	request := Request{Group: "apps", GroupSet: true, Platform: "linux", PlatformSet: true, Secret: true, SecretSet: true}
	inference, err := InferOwnership(InferInput{Request: request, Target: "app.conf", Home: "/home", Plan: inferPlan(t, "linux", nil, nil)})
	if err != nil {
		t.Fatalf("infer: %v", err)
	}
	inferCheck(t, inference, "apps", "apps/app.conf", deployment.FileSecret, deployment.LayerLinux)
}

func testInferManagedOrdinary(t *testing.T) {
	plan := inferPlan(t, "linux", []deployment.ManagedFile{inferFile(t, "apps", "a.conf", "apps/a.conf", deployment.FileOrdinary, deployment.LayerBase)}, nil)
	inference, err := InferOwnership(InferInput{Target: "a.conf", Home: "/home", Plan: plan})
	if err != nil {
		t.Fatalf("infer: %v", err)
	}
	inferCheck(t, inference, "apps", "apps/a.conf", deployment.FileOrdinary, deployment.LayerBase)
}

func testInferManagedSecret(t *testing.T) {
	plan := inferPlan(t, "linux", []deployment.ManagedFile{inferFile(t, "", "token", "token", deployment.FileSecret, deployment.LayerBase)}, nil)
	inference, err := InferOwnership(InferInput{Request: Request{Secret: true, SecretSet: true}, Target: "token", Home: "/home", Plan: plan})
	if err != nil {
		t.Fatalf("infer: %v", err)
	}
	inferCheck(t, inference, "", "token", deployment.FileSecret, deployment.LayerBase)
}

func testInferStorageConflict(t *testing.T) {
	plan := inferPlan(t, "linux", []deployment.ManagedFile{inferFile(t, "", "a.conf", "a.conf", deployment.FileOrdinary, deployment.LayerBase)}, nil)
	_, err := InferOwnership(InferInput{Request: Request{Secret: true, SecretSet: true}, Target: "a.conf", Home: "/home", Plan: plan})
	if err == nil {
		t.Fatal("an explicit secret flag must conflict with a managed ordinary file")
	}
}

func testInferAlias(t *testing.T) {
	plan := inferPlan(t, "linux", nil, []deployment.Alias{inferAlias(t, "bin/tool", "files/tool")})
	_, err := InferOwnership(InferInput{Target: "bin/tool", Home: "/home", Plan: plan})
	if err == nil {
		t.Fatal("an alias target must be rejected")
	}
}

func testInferInactivePlatform(t *testing.T) {
	_, err := InferOwnership(InferInput{Request: Request{Platform: "darwin", PlatformSet: true}, Target: "a.conf", Home: "/home", Plan: inferPlan(t, "linux", nil, nil)})
	if err == nil {
		t.Fatal("an inactive explicit platform must be rejected")
	}
}

func testInferDefaults(t *testing.T) {
	inference, err := InferOwnership(InferInput{Target: "dir/app.conf", Home: "/home", Plan: inferPlan(t, "linux", nil, nil)})
	if err != nil {
		t.Fatalf("infer: %v", err)
	}
	inferCheck(t, inference, "", "dir/app.conf", deployment.FileOrdinary, deployment.LayerBase)
}
