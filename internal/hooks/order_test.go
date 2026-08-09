package hooks

import (
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestHookOrdering(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"before puts repository first", testOrderBeforeRepositoryFirst},
		{"after puts repository last", testOrderAfterRepositoryLast},
		{"groups sort lexically", testOrderGroupLexical},
		{"names tie-break bytewise", testOrderNameTies},
		{"phases are independent", testOrderPhaseIndependence},
		{"single scope sorts by name", testOrderSingleScope},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testOrderBeforeRepositoryFirst(t *testing.T) {
	hooks := []deployment.Hook{
		orderHook("atuin", deployment.HookBefore, "b.sh"),
		orderHook("", deployment.HookBefore, "a.sh"),
		orderHook("ghostty", deployment.HookBefore, "a.sh"),
	}
	SortBefore(hooks)
	assertOrder(t, hooks, []deployment.Hook{
		orderHook("", deployment.HookBefore, "a.sh"),
		orderHook("atuin", deployment.HookBefore, "b.sh"),
		orderHook("ghostty", deployment.HookBefore, "a.sh"),
	})
}

func testOrderAfterRepositoryLast(t *testing.T) {
	hooks := []deployment.Hook{
		orderHook("atuin", deployment.HookAfter, "b.sh"),
		orderHook("", deployment.HookAfter, "a.sh"),
		orderHook("ghostty", deployment.HookAfter, "a.sh"),
	}
	SortAfter(hooks)
	assertOrder(t, hooks, []deployment.Hook{
		orderHook("atuin", deployment.HookAfter, "b.sh"),
		orderHook("ghostty", deployment.HookAfter, "a.sh"),
		orderHook("", deployment.HookAfter, "a.sh"),
	})
}

func testOrderGroupLexical(t *testing.T) {
	hooks := []deployment.Hook{
		orderHook("zsh", deployment.HookAfter, "x.sh"),
		orderHook("atuin", deployment.HookAfter, "x.sh"),
		orderHook("ghostty", deployment.HookAfter, "x.sh"),
	}
	SortAfter(hooks)
	assertOrder(t, hooks, []deployment.Hook{
		orderHook("atuin", deployment.HookAfter, "x.sh"),
		orderHook("ghostty", deployment.HookAfter, "x.sh"),
		orderHook("zsh", deployment.HookAfter, "x.sh"),
	})
}

func testOrderNameTies(t *testing.T) {
	hooks := []deployment.Hook{
		orderHook("atuin", deployment.HookBefore, "install.sh"),
		orderHook("atuin", deployment.HookBefore, "finish.sh"),
		orderHook("atuin", deployment.HookBefore, "clone.sh"),
	}
	SortBefore(hooks)
	assertOrder(t, hooks, []deployment.Hook{
		orderHook("atuin", deployment.HookBefore, "clone.sh"),
		orderHook("atuin", deployment.HookBefore, "finish.sh"),
		orderHook("atuin", deployment.HookBefore, "install.sh"),
	})
}

func testOrderPhaseIndependence(t *testing.T) {
	hooks := []deployment.Hook{
		orderHook("", deployment.HookAfter, "z.sh"),
		orderHook("", deployment.HookBefore, "a.sh"),
		orderHook("atuin", deployment.HookAfter, "a.sh"),
	}
	SortBefore(hooks)
	assertOrder(t, hooks, []deployment.Hook{
		orderHook("", deployment.HookBefore, "a.sh"),
		orderHook("", deployment.HookAfter, "z.sh"),
		orderHook("atuin", deployment.HookAfter, "a.sh"),
	})
}

func testOrderSingleScope(t *testing.T) {
	hooks := []deployment.Hook{
		orderHook("", deployment.HookAfter, "mid.sh"),
		orderHook("", deployment.HookAfter, "early.sh"),
		orderHook("", deployment.HookAfter, "late.sh"),
	}
	SortAfter(hooks)
	assertOrder(t, hooks, []deployment.Hook{
		orderHook("", deployment.HookAfter, "early.sh"),
		orderHook("", deployment.HookAfter, "late.sh"),
		orderHook("", deployment.HookAfter, "mid.sh"),
	})
}

func orderHook(scope string, phase deployment.HookPhase, name string) deployment.Hook {
	return deployment.Hook{
		Scope: deployment.NewScope(scope), Phase: phase, Name: name,
		AbsolutePath: "/abs/" + name, RepositoryPath: name,
	}
}

func assertOrder(t *testing.T, got []deployment.Hook, want []deployment.Hook) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("hooks = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("hook %d = %+v, want %+v", index, got[index], want[index])
		}
	}
}
