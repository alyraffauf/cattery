package repository

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

const routesFixture = `version = 1

[symlinks.all]
".config/ghostty/config" = [".example/config"]

[symlinks.darwin]
".bashrc" = ["Bashrc"]
`

func TestPlanCompilation(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"linux and darwin golden values", testPlanGolden},
		{"determinism permutations", testPlanDeterminism},
		{"route payload determinism", testPlanPayloadDeterminism},
		{"selection filters scopes", testPlanSelection},
		{"invalid unselected scopes", testPlanInvalidUnselected},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

type goldenScenario struct {
	platform deployment.Layer
	files    []deployment.ManagedFile
	aliases  []deployment.Alias
}

func testPlanGolden(t *testing.T) {
	root := compileFixture(t)
	scenarios := []goldenScenario{
		{deployment.LayerLinux, []deployment.ManagedFile{
			expectFile(root, fileWant{scope: deployment.NewScope(""), layer: deployment.LayerBase, repoPath: ".bashrc", target: ".bashrc"}),
			expectFile(root, fileWant{scope: deployment.NewScope("atuin"), layer: deployment.LayerBase, repoPath: "atuin/.config/atuin/config.toml", target: ".config/atuin/config.toml"}),
			expectFile(root, fileWant{scope: deployment.NewScope(""), layer: deployment.LayerLinux, repoPath: "_linux/.config/extra", target: ".config/extra", exec: 0o111}),
			expectFile(root, fileWant{scope: deployment.NewScope(""), layer: deployment.LayerLinux, repoPath: "_linux/.config/ghostty/config", target: ".config/ghostty/config"}),
			expectFile(root, fileWant{scope: deployment.NewScope("ghostty"), layer: deployment.LayerBase, repoPath: "ghostty/Brewfile", target: "Brewfile", exec: 0o111}),
		}, []deployment.Alias{
			expectAlias(aliasWant{scope: deployment.NewScope(""), platform: "linux", destination: ".example/config", canonical: ".config/ghostty/config"}),
		}},
		{deployment.LayerDarwin, []deployment.ManagedFile{
			expectFile(root, fileWant{scope: deployment.NewScope(""), layer: deployment.LayerBase, repoPath: ".bashrc", target: ".bashrc"}),
			expectFile(root, fileWant{scope: deployment.NewScope("atuin"), layer: deployment.LayerBase, repoPath: "atuin/.config/atuin/config.toml", target: ".config/atuin/config.toml"}),
			expectFile(root, fileWant{scope: deployment.NewScope(""), layer: deployment.LayerDarwin, repoPath: "_darwin/.config/ghostty/config", target: ".config/ghostty/config"}),
			expectFile(root, fileWant{scope: deployment.NewScope("ghostty"), layer: deployment.LayerBase, repoPath: "ghostty/Brewfile", target: "Brewfile", exec: 0o111}),
		}, []deployment.Alias{
			expectAlias(aliasWant{scope: deployment.NewScope(""), platform: "darwin", destination: ".example/config", canonical: ".config/ghostty/config"}),
			expectAlias(aliasWant{scope: deployment.NewScope(""), platform: "darwin", destination: "Bashrc", canonical: ".bashrc"}),
		}},
	}
	for _, scenario := range scenarios {
		t.Run(string(scenario.platform), func(t *testing.T) {
			plan, err := Compile(CompileInput{Platform: scenario.platform, RepositoryRoot: root, HomeRoot: "/home/u"})
			if err != nil {
				t.Fatal(err)
			}
			assertPlan(t, plan, goldenWant(root, scenario))
		})
	}
}

func goldenWant(root string, scenario goldenScenario) deployment.Plan {
	return mustPlan(deployment.PlanInput{
		RepositoryRoot: root,
		Platform:       string(scenario.platform),
		Groups:         []string{"atuin", "ghostty"},
		Files:          scenario.files,
		Aliases:        scenario.aliases,
		Hooks: []deployment.Hook{
			expectHook(root, hookWant{scope: deployment.NewScope("ghostty"), phase: deployment.HookAfter, name: "finish.sh"}),
			expectHook(root, hookWant{scope: deployment.NewScope(""), phase: deployment.HookBefore, name: "install.sh"}),
			expectHook(root, hookWant{scope: deployment.NewScope("atuin"), phase: deployment.HookBefore, name: "init.sh"}),
		},
	})
}

func testPlanDeterminism(t *testing.T) {
	root := compileFixture(t)
	first, err := Compile(CompileInput{
		Platform: deployment.LayerLinux, RepositoryRoot: root, HomeRoot: "/home/u",
		Selected: []string{"atuin", "ghostty"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compile(CompileInput{
		Platform: deployment.LayerLinux, RepositoryRoot: root, HomeRoot: "/home/u",
		Selected: []string{"ghostty", "atuin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPlan(t, first, second)
}

func testPlanSelection(t *testing.T) {
	root := compileFixture(t)
	plan, err := Compile(CompileInput{
		Platform: deployment.LayerLinux, RepositoryRoot: root, HomeRoot: "/home/u",
		Selected: []string{"atuin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := mustPlan(deployment.PlanInput{
		RepositoryRoot: root,
		Platform:       "linux",
		Groups:         []string{"atuin"},
		Files: []deployment.ManagedFile{
			expectFile(root, fileWant{scope: deployment.NewScope("atuin"), layer: deployment.LayerBase,
				repoPath: "atuin/.config/atuin/config.toml", target: ".config/atuin/config.toml"}),
		},
		Hooks: []deployment.Hook{
			expectHook(root, hookWant{scope: deployment.NewScope(""), phase: deployment.HookBefore, name: "install.sh"}),
			expectHook(root, hookWant{scope: deployment.NewScope("atuin"), phase: deployment.HookBefore, name: "init.sh"}),
		},
	})
	assertPlan(t, plan, want)
}

func testPlanInvalidUnselected(t *testing.T) {
	root := compileFixture(t)
	writeFile(t, filepath.Join(root, ".config", "x"), 0o644)
	writeFile(t, filepath.Join(root, "ghostty", ".config", "x"), 0o644)
	_, err := Compile(CompileInput{
		Platform: deployment.LayerLinux, RepositoryRoot: root, HomeRoot: "/home/u",
		Selected: []string{"atuin"},
	})
	if err == nil {
		t.Fatal("collision in an unselected group was accepted")
	}
	_, err = Compile(CompileInput{
		Platform: deployment.LayerLinux, RepositoryRoot: root, HomeRoot: "/home/u",
		Selected: []string{"missing"},
	})
	if err == nil {
		t.Fatal("unknown selected group was accepted")
	}
}

func compileFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".bashrc"), 0o644)
	writeFile(t, filepath.Join(root, ".config", "ghostty", "config"), 0o644)
	writeFile(t, filepath.Join(root, "_linux", ".config", "ghostty", "config"), 0o644)
	writeFile(t, filepath.Join(root, "_linux", ".config", "extra"), 0o755)
	writeFile(t, filepath.Join(root, "_darwin", ".config", "ghostty", "config"), 0o644)
	writeFile(t, filepath.Join(root, "_hooks", "before", "install.sh"), 0o755)
	writeFile(t, filepath.Join(root, "atuin", ".config", "atuin", "config.toml"), 0o644)
	writeFile(t, filepath.Join(root, "atuin", "_hooks", "before", "init.sh"), 0o755)
	writeFile(t, filepath.Join(root, "ghostty", "Brewfile"), 0o755)
	writeFile(t, filepath.Join(root, "ghostty", "_hooks", "after", "finish.sh"), 0o755)
	writeRoutes(t, root, routesFixture)
	return root
}

func writeRoutes(t *testing.T, root string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "_routes.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertPlan(t *testing.T, got deployment.Plan, want deployment.Plan) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan = %+v, want %+v", got, want)
	}
}

func mustPlan(input deployment.PlanInput) deployment.Plan {
	plan, err := deployment.NewPlan(input)
	if err != nil {
		panic(err)
	}
	return plan
}

type fileWant struct {
	scope    deployment.Scope
	layer    deployment.Layer
	repoPath string
	target   string
	exec     fs.FileMode
}

func expectFile(root string, want fileWant) deployment.ManagedFile {
	return deployment.ManagedFile{
		Scope: want.scope, Layer: want.layer, Kind: deployment.FileOrdinary,
		SourceAbsolutePath: filepath.Join(root, want.repoPath), SourceRepositoryPath: want.repoPath,
		TargetRelativePath: want.target, SourceExecutableBits: want.exec,
	}
}

type aliasWant struct {
	scope       deployment.Scope
	platform    string
	destination string
	canonical   string
}

func expectAlias(want aliasWant) deployment.Alias {
	return deployment.Alias{
		Scope: want.scope, Platform: want.platform,
		AliasRelativePath: want.destination, CanonicalTargetRelativePath: want.canonical,
	}
}

type hookWant struct {
	scope deployment.Scope
	phase deployment.HookPhase
	name  string
}

func expectHook(root string, want hookWant) deployment.Hook {
	return deployment.Hook{
		Scope: want.scope, Phase: want.phase, Name: want.name,
		AbsolutePath:   filepath.Join(root, want.scope.Group, "_hooks", string(want.phase), want.name),
		RepositoryPath: filepath.Join(want.scope.Group, "_hooks", string(want.phase), want.name),
	}
}
