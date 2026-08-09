package deployment

import (
	"io/fs"
	"testing"
)

func TestPlanContract(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"constructor copies caller slices", testPlanCopiesInputs},
		{"accessor copy cannot mutate plan", testAccessorCopyIsolation},
		{"zero plan yields nil accessors", testZeroPlanAccessors},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func samplePlanCandidate() Plan {
	return Plan{
		RepositoryRoot: "/repo",
		Platform:       "linux",
		Groups:         []string{"atuin", "zsh"},
		Files: []ManagedFile{
			{
				Scope: NewScope("atuin"), Layer: LayerBase, Kind: FileOrdinary,
				SourceAbsolutePath: "/repo/atuin/c", SourceRepositoryPath: "atuin/c",
				TargetRelativePath: ".config/atuin/c", SourceExecutableBits: fs.FileMode(0o644),
			},
		},
		Aliases: []Alias{
			{
				Scope: NewScope(""), Platform: "linux",
				CanonicalTargetRelativePath: ".bashrc", AliasRelativePath: ".bash_aliases",
			},
		},
		Hooks: []Hook{
			{
				Scope: NewScope(""), Phase: HookBefore, Name: "install",
				AbsolutePath: "/repo/_hooks/install.sh", RepositoryPath: "_hooks/install.sh",
			},
		},
	}
}

func testPlanCopiesInputs(t *testing.T) {
	candidate := samplePlanCandidate()
	groups := candidate.Groups
	files := candidate.Files
	aliases := candidate.Aliases
	hooks := candidate.Hooks
	plan := NewPlan(candidate)
	groups = append(groups, " mutated")
	files = append(files, ManagedFile{TargetRelativePath: "x"})
	aliases = append(aliases, Alias{AliasRelativePath: "x"})
	hooks = append(hooks, Hook{Name: "x"})
	if len(plan.AllGroups()) != 2 {
		t.Fatalf("groups leaked: %v", plan.AllGroups())
	}
	if len(plan.AllFiles()) != 1 {
		t.Fatalf("files leaked: %v", plan.AllFiles())
	}
	if len(plan.AllAliases()) != 1 {
		t.Fatalf("aliases leaked: %v", plan.AllAliases())
	}
	if len(plan.AllHooks()) != 1 {
		t.Fatalf("hooks leaked: %v", plan.AllHooks())
	}
}

func testAccessorCopyIsolation(t *testing.T) {
	plan := NewPlan(samplePlanCandidate())
	groups := plan.AllGroups()
	files := plan.AllFiles()
	aliases := plan.AllAliases()
	hooks := plan.AllHooks()
	groups[0] = "corrupted"
	files[0] = ManagedFile{TargetRelativePath: "corrupted"}
	aliases[0] = Alias{AliasRelativePath: "corrupted"}
	hooks[0] = Hook{Name: "corrupted"}
	again := NewPlan(samplePlanCandidate())
	if plan.AllGroups()[0] == "corrupted" || again.AllGroups()[0] == "corrupted" {
		t.Fatal("accessor copy leaked into plan")
	}
	if plan.AllFiles()[0].TargetRelativePath == "corrupted" {
		t.Fatal("file accessor copy leaked into plan")
	}
	if plan.AllAliases()[0].AliasRelativePath == "corrupted" {
		t.Fatal("alias accessor copy leaked into plan")
	}
	if plan.AllHooks()[0].Name == "corrupted" {
		t.Fatal("hook accessor copy leaked into plan")
	}
}

func testZeroPlanAccessors(t *testing.T) {
	var plan Plan
	if got := plan.AllGroups(); got != nil {
		t.Fatalf("zero plan groups = %v, want nil", got)
	}
	if got := plan.AllFiles(); got != nil {
		t.Fatalf("zero plan files = %v, want nil", got)
	}
	if got := plan.AllAliases(); got != nil {
		t.Fatalf("zero plan aliases = %v, want nil", got)
	}
	if got := plan.AllHooks(); got != nil {
		t.Fatalf("zero plan hooks = %v, want nil", got)
	}
}
