package repository

import (
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// collisionCase is one engine scenario: a plan plus the expected verdict.
type collisionCase struct {
	name    string
	files   []deployment.ManagedFile
	aliases []deployment.Alias
	scope   CollisionScope
	wantErr bool
}

func TestCompiledCollision(t *testing.T) {
	scenarios := validScenarios()
	scenarios = append(scenarios, fileFileScenarios()...)
	scenarios = append(scenarios, fileAliasScenarios()...)
	scenarios = append(scenarios, aliasScenarios()...)
	scenarios = append(scenarios, protectedScenarios()...)
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			err := CheckCollisions(scenario.files, scenario.aliases, scenario.scope)
			if scenario.wantErr && err == nil {
				t.Fatal("collision was accepted")
			}
			if !scenario.wantErr && err != nil {
				t.Fatalf("collision rejected unexpectedly: %v", err)
			}
		})
	}
}

// validScenarios covers plans the engine must accept: disjoint destinations,
// legal shared parents, and the intended alias/canonical identity.
func validScenarios() []collisionCase {
	root := deployment.NewScope("")
	group := deployment.NewScope("atuin")
	return []collisionCase{
		{"disjoint plan accepted",
			files(collisionFile(root, ".config/ghostty/config", ".config/ghostty/config"),
				collisionFile(group, "atuin/Brewfile", "Brewfile")),
			[]deployment.Alias{collisionAlias(root, ".local/share/ghostty/config", ".config/ghostty/config")},
			CollisionScope{}, false},
		{"shared parents are legal",
			files(collisionFile(root, ".config/app/b/c", ".config/app/b/c"),
				collisionFile(root, ".config/app/b/d", ".config/app/b/d")),
			[]deployment.Alias{collisionAlias(root, ".config/app/b/x", ".config/app/b/c")},
			CollisionScope{}, false},
		{"alias/canonical identity exempt",
			files(collisionFile(root, ".config/app/config", ".config/app/config")),
			[]deployment.Alias{collisionAlias(root, ".config/app/config", ".config/app/config")},
			CollisionScope{}, false},
	}
}

// fileFileScenarios covers file/file equality, cross-scope ownership, and
// parent/child and portable-equivalence families.
func fileFileScenarios() []collisionCase {
	root := deployment.NewScope("")
	group := deployment.NewScope("atuin")
	return []collisionCase{
		{"file/file same target",
			files(collisionFile(root, ".config/x", ".config/x"), collisionFile(group, "atuin/.config/x", ".config/x")),
			nil, CollisionScope{}, true},
		{"file/file same scope duplicate",
			files(collisionFile(root, ".config/x", ".config/x"), collisionFile(root, "other/x", ".config/x")),
			nil, CollisionScope{}, true},
		{"file/file parent-child",
			files(collisionFile(root, ".config/a", ".config/a"), collisionFile(root, ".config/a/b", ".config/a/b")),
			nil, CollisionScope{}, true},
		{"file/file child-parent",
			files(collisionFile(root, ".config/a/b", ".config/a/b"), collisionFile(root, ".config/a", ".config/a")),
			nil, CollisionScope{}, true},
		{"file/file case-folded",
			files(collisionFile(root, ".config/Foo", ".config/Foo"), collisionFile(root, ".config/foo", ".config/foo")),
			nil, CollisionScope{}, true},
		{"file/file NFC-NDF",
			files(collisionFile(root, ".config/café", ".config/café"), collisionFile(root, ".config/cafe\u0301", ".config/cafe\u0301")),
			nil, CollisionScope{}, true},
	}
}

// fileAliasScenarios covers alias destinations colliding with file targets in
// both parent/child directions and across scopes.
func fileAliasScenarios() []collisionCase {
	root := deployment.NewScope("")
	group := deployment.NewScope("atuin")
	return []collisionCase{
		{"file/alias destination",
			files(collisionFile(root, ".config/app/config", ".config/app/config")),
			[]deployment.Alias{collisionAlias(root, ".config/app/config", ".config/other")},
			CollisionScope{}, true},
		{"file/alias cross-scope",
			files(collisionFile(root, ".config/x", ".config/x")),
			[]deployment.Alias{collisionAlias(group, ".config/x", "Brewfile")},
			CollisionScope{}, true},
		{"file/alias file is parent",
			files(collisionFile(root, ".config/a", ".config/a")),
			[]deployment.Alias{collisionAlias(root, ".config/a/b", ".config/other")},
			CollisionScope{}, true},
		{"file/alias alias is parent",
			files(collisionFile(root, ".config/a/b", ".config/a/b")),
			[]deployment.Alias{collisionAlias(root, ".config/a", ".config/other")},
			CollisionScope{}, true},
	}
}

// aliasScenarios covers alias/alias equality, portable equivalence, and
// parent/child destinations.
func aliasScenarios() []collisionCase {
	root := deployment.NewScope("")
	group := deployment.NewScope("atuin")
	return []collisionCase{
		{"alias/alias destination",
			nil,
			[]deployment.Alias{collisionAlias(root, ".local/share/x", ".config/a"),
				collisionAlias(group, ".local/share/x", "Brewfile")},
			CollisionScope{}, true},
		{"alias/alias case-folded",
			nil,
			[]deployment.Alias{collisionAlias(root, ".config/A", ".config/a"),
				collisionAlias(root, ".config/a", ".config/b")},
			CollisionScope{}, true},
		{"alias/alias parent-child",
			nil,
			[]deployment.Alias{collisionAlias(root, ".config/a", ".config/b"),
				collisionAlias(root, ".config/a/b", ".config/c")},
			CollisionScope{}, true},
	}
}

// protectedScenarios covers file targets and alias destinations entering a
// protected tree, including portable-equivalent variants.
func protectedScenarios() []collisionCase {
	root := deployment.NewScope("")
	scope := CollisionScope{HomeRoot: "/home/u", Protected: []string{"/home/u/.config/cattery"}}
	return []collisionCase{
		{"protected-tree file",
			files(collisionFile(root, ".config/cattery/x", ".config/cattery/x")),
			nil, scope, true},
		{"protected-tree alias",
			nil,
			[]deployment.Alias{collisionAlias(root, ".config/cattery/y", ".config/other")},
			scope, true},
		{"protected-tree equivalent variant",
			files(collisionFile(root, ".config/Cattery/x", ".config/Cattery/x")),
			nil, scope, true},
	}
}

// collisionFile builds a base-layer ordinary file record for one scope.
func collisionFile(scope deployment.Scope, repoPath, target string) deployment.ManagedFile {
	return deployment.ManagedFile{
		Scope: scope, Layer: deployment.LayerBase, Kind: deployment.FileOrdinary,
		SourceAbsolutePath: "/repo/" + repoPath, SourceRepositoryPath: repoPath,
		TargetRelativePath: target,
	}
}

// collisionAlias builds one platform alias record.
func collisionAlias(scope deployment.Scope, destination, canonical string) deployment.Alias {
	return deployment.Alias{
		Scope: scope, Platform: "linux",
		AliasRelativePath: destination, CanonicalTargetRelativePath: canonical,
	}
}

// files collects file records into one slice.
func files(items ...deployment.ManagedFile) []deployment.ManagedFile {
	return items
}
