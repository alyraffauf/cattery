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
		{"constructor validates input", testPlanValidatesInput},
		{"accessor copy cannot mutate plan", testAccessorCopyIsolation},
		{"zero plan yields nil accessors", testZeroPlanAccessors},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func samplePlanCandidate() PlanInput {
	return PlanInput{
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
	plan := mustPlan(candidate)
	groups[0] = " mutated"
	files[0] = ManagedFile{TargetRelativePath: "x"}
	aliases[0] = Alias{AliasRelativePath: "x"}
	hooks[0] = Hook{Name: "x"}
	if len(plan.Groups()) != 2 {
		t.Fatalf("groups leaked: %v", plan.Groups())
	}
	if len(plan.Files()) != 1 {
		t.Fatalf("files leaked: %v", plan.Files())
	}
	if len(plan.Aliases()) != 1 {
		t.Fatalf("aliases leaked: %v", plan.Aliases())
	}
	if len(plan.Hooks()) != 1 {
		t.Fatalf("hooks leaked: %v", plan.Hooks())
	}
}

func testPlanValidatesInput(t *testing.T) {
	candidate := samplePlanCandidate()
	candidate.Files[0].Kind = "invalid"
	if _, err := NewPlan(candidate); err == nil {
		t.Fatal("invalid managed file must be rejected")
	}
}

func testAccessorCopyIsolation(t *testing.T) {
	plan := mustPlan(samplePlanCandidate())
	groups := plan.Groups()
	files := plan.Files()
	aliases := plan.Aliases()
	hooks := plan.Hooks()
	groups[0] = "corrupted"
	files[0] = ManagedFile{TargetRelativePath: "corrupted"}
	aliases[0] = Alias{AliasRelativePath: "corrupted"}
	hooks[0] = Hook{Name: "corrupted"}
	again := mustPlan(samplePlanCandidate())
	if plan.Groups()[0] == "corrupted" || again.Groups()[0] == "corrupted" {
		t.Fatal("accessor copy leaked into plan")
	}
	if plan.Files()[0].TargetRelativePath == "corrupted" {
		t.Fatal("file accessor copy leaked into plan")
	}
	if plan.Aliases()[0].AliasRelativePath == "corrupted" {
		t.Fatal("alias accessor copy leaked into plan")
	}
	if plan.Hooks()[0].Name == "corrupted" {
		t.Fatal("hook accessor copy leaked into plan")
	}
}

func mustPlan(input PlanInput) Plan {
	plan, err := NewPlan(input)
	if err != nil {
		panic(err)
	}
	return plan
}

func testZeroPlanAccessors(t *testing.T) {
	var plan Plan
	if got := plan.Groups(); got != nil {
		t.Fatalf("zero plan groups = %v, want nil", got)
	}
	if got := plan.Files(); got != nil {
		t.Fatalf("zero plan files = %v, want nil", got)
	}
	if got := plan.Aliases(); got != nil {
		t.Fatalf("zero plan aliases = %v, want nil", got)
	}
	if got := plan.Hooks(); got != nil {
		t.Fatalf("zero plan hooks = %v, want nil", got)
	}
}
