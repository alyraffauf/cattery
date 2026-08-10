package add

import (
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestAddPlan(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"sorts items by target path", testBuildPlanSorts},
		{"execution order is sequential", testBuildPlanExecutionOrder},
		{"rejects invalid item", testBuildPlanRejectsInvalid},
		{"dry run records planned targets", testDryRunPlanned},
		{"dry run flags secrets", testDryRunFlagsSecret},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testBuildPlanSorts(t *testing.T) {
	plan, err := BuildPlan([]ItemPlanInput{
		planItem("zeta"), planItem("alpha"), planItem("mid"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "mid", "zeta"}
	for index, item := range plan.Items() {
		if item.TargetRelativePath() != want[index] {
			t.Fatalf("item %d = %q, want %q", index, item.TargetRelativePath(), want[index])
		}
	}
}

func testBuildPlanExecutionOrder(t *testing.T) {
	plan, err := BuildPlan([]ItemPlanInput{planItem("b"), planItem("a")})
	if err != nil {
		t.Fatal(err)
	}
	order := plan.ExecutionOrder()
	if len(order) != 2 || order[0] != 0 || order[1] != 1 {
		t.Fatalf("execution order = %v, want [0 1]", order)
	}
}

func testBuildPlanRejectsInvalid(t *testing.T) {
	invalid := ItemPlanInput{Layer: deployment.LayerBase, Kind: deployment.FileOrdinary}
	if _, err := BuildPlan([]ItemPlanInput{invalid}); err == nil {
		t.Fatal("BuildPlan accepted an invalid item")
	}
}

func testDryRunPlanned(t *testing.T) {
	plan := mustPlan(t, []ItemPlanInput{planItem("b"), planItem("a")})
	result := DryRun(plan)
	if result.Summary.Planned != 2 {
		t.Fatalf("planned = %d, want 2", result.Summary.Planned)
	}
	if result.Items[0].Target != "a" || result.Items[1].Target != "b" {
		t.Fatalf("targets = %q %q, want sorted", result.Items[0].Target, result.Items[1].Target)
	}
	if result.Items[0].Status != StatusPlanned {
		t.Fatalf("status = %q, want %q", result.Items[0].Status, StatusPlanned)
	}
}

func testDryRunFlagsSecret(t *testing.T) {
	item := planItem("creds")
	item.Kind = deployment.FileSecret
	plan := mustPlan(t, []ItemPlanInput{planItem("readme"), item})
	result := DryRun(plan)
	if result.Items[0].Target != "creds" {
		t.Fatal("dry run did not include the secret item")
	}
}

func mustPlan(t *testing.T, items []ItemPlanInput) BatchPlan {
	t.Helper()
	plan, err := BuildPlan(items)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func planItem(name string) ItemPlanInput {
	return ItemPlanInput{
		Layer: deployment.LayerBase, Kind: deployment.FileOrdinary,
		TargetAbsolutePath:   "/home/user/" + name,
		TargetRelativePath:   name,
		SourceRepositoryPath: name,
		SourceAbsolutePath:   "/repo/" + name,
	}
}
