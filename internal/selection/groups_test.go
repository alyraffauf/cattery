package selection

import (
	"slices"
	"testing"
)

func TestGroupSelection(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"no arguments select root plus current", testGroupsNoArguments},
		{"no arguments add active state-only groups", testGroupsActiveStateOnly},
		{"no arguments exclude retired-only groups", testGroupsRetiredOnlyExcluded},
		{"explicit arguments are sorted", testGroupsExplicitOrder},
		{"root-only repository stays root-only", testGroupsRootOnly},
		{"case variants are exact", testGroupsCaseVariants},
		{"current groups validate explicitly", testGroupsCurrentExplicit},
		{"active rows validate explicit names", testGroupsActiveExplicit},
		{"retired rows validate explicit names", testGroupsRetiredExplicit},
		{"deleted groups are explicit-only", testGroupsDeletedExplicit},
		{"inactive-platform rows keep groups alive", testGroupsInactivePlatform},
		{"duplicate arguments are rejected", testGroupsDuplicates},
		{"unknown arguments are rejected", testGroupsUnknown},
		{"validate rejects state-only names", testGroupsValidateStrict},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testGroupsNoArguments(t *testing.T) {
	selected, err := CompiledOnly([]string{"apps", "dotfiles"}, nil)
	if err != nil {
		t.Fatalf("CompiledOnly: %v", err)
	}
	if !selected.Root || selected.Groups != nil {
		t.Fatalf("CompiledOnly no-args = %+v, want root plus all current", selected)
	}
	expanded, err := CompiledAndPersisted([]string{"apps"}, PersistedGroups{Active: []string{"ghost"}, All: []string{"ghost"}}, nil)
	if err != nil {
		t.Fatalf("CompiledAndPersisted: %v", err)
	}
	if !expanded.Root || !slices.Equal(expanded.Groups, []string{"apps", "ghost"}) {
		t.Fatalf("CompiledAndPersisted no-args = %+v, want root plus current and active state-only", expanded)
	}
}

func testGroupsActiveStateOnly(t *testing.T) {
	selected, err := CompiledAndPersisted([]string{"apps"}, PersistedGroups{Active: []string{"ghost"}, All: []string{"ghost"}}, nil)
	if err != nil {
		t.Fatalf("CompiledAndPersisted: %v", err)
	}
	if !selected.Root || !slices.Equal(selected.Groups, []string{"apps", "ghost"}) {
		t.Fatalf("selection = %+v, want root plus apps and ghost", selected)
	}
}

func testGroupsRetiredOnlyExcluded(t *testing.T) {
	selected, err := CompiledAndPersisted([]string{"apps"}, PersistedGroups{Active: nil, All: []string{"apps", "gone"}}, nil)
	if err != nil {
		t.Fatalf("CompiledAndPersisted: %v", err)
	}
	if !selected.Root || !slices.Equal(selected.Groups, []string{"apps"}) {
		t.Fatalf("selection = %+v, want retired-only groups excluded", selected)
	}
}

func testGroupsExplicitOrder(t *testing.T) {
	only, err := CompiledOnly([]string{"a", "b", "c"}, []string{"c", "a"})
	if err != nil {
		t.Fatalf("CompiledOnly: %v", err)
	}
	if only.Root || !slices.Equal(only.Groups, []string{"a", "c"}) {
		t.Fatalf("CompiledOnly selection = %+v, want sorted [a c]", only)
	}
	both, err := CompiledAndPersisted([]string{"a", "b", "c"}, PersistedGroups{All: []string{"c", "a"}}, []string{"c", "a"})
	if err != nil {
		t.Fatalf("CompiledAndPersisted: %v", err)
	}
	if both.Root || !slices.Equal(both.Groups, []string{"a", "c"}) {
		t.Fatalf("CompiledAndPersisted selection = %+v, want sorted [a c]", both)
	}
}

func testGroupsRootOnly(t *testing.T) {
	only, err := CompiledOnly(nil, nil)
	if err != nil {
		t.Fatalf("CompiledOnly: %v", err)
	}
	if !only.Root || only.Groups != nil {
		t.Fatalf("CompiledOnly selection = %+v, want root only", only)
	}
	persisted, err := CompiledAndPersisted(nil, PersistedGroups{}, nil)
	if err != nil {
		t.Fatalf("CompiledAndPersisted: %v", err)
	}
	if !persisted.Root || persisted.Groups != nil {
		t.Fatalf("CompiledAndPersisted selection = %+v, want root only", persisted)
	}
	if _, err := CompiledOnly(nil, []string{"apps"}); err == nil {
		t.Fatal("a root-only repository must reject explicit groups")
	}
}

func testGroupsCaseVariants(t *testing.T) {
	if _, err := CompiledOnly([]string{"Apps"}, []string{"apps"}); err == nil {
		t.Fatal("case-variant group must be rejected as unknown")
	}
	if _, err := CompiledAndPersisted([]string{}, PersistedGroups{All: []string{"Apps"}}, []string{"apps"}); err == nil {
		t.Fatal("case-variant state group must be rejected as unknown")
	}
	if _, err := CompiledOnly([]string{"apps"}, []string{"Apps"}); err == nil {
		t.Fatal("case-variant argument must not match the current group")
	}
}

func testGroupsCurrentExplicit(t *testing.T) {
	selected, err := CompiledOnly([]string{"apps"}, []string{"apps"})
	if err != nil {
		t.Fatalf("CompiledOnly: %v", err)
	}
	if selected.Root || !slices.Equal(selected.Groups, []string{"apps"}) {
		t.Fatalf("selection = %+v, want explicit apps only", selected)
	}
}

func testGroupsActiveExplicit(t *testing.T) {
	selected, err := CompiledAndPersisted([]string{}, PersistedGroups{Active: []string{"ghost"}, All: []string{"ghost"}}, []string{"ghost"})
	if err != nil {
		t.Fatalf("CompiledAndPersisted: %v", err)
	}
	if selected.Root || !slices.Equal(selected.Groups, []string{"ghost"}) {
		t.Fatalf("selection = %+v, want explicit ghost only", selected)
	}
}

func testGroupsRetiredExplicit(t *testing.T) {
	selected, err := CompiledAndPersisted([]string{}, PersistedGroups{All: []string{"gone"}}, []string{"gone"})
	if err != nil {
		t.Fatalf("CompiledAndPersisted: %v", err)
	}
	if selected.Root || !slices.Equal(selected.Groups, []string{"gone"}) {
		t.Fatalf("selection = %+v, want explicit gone only", selected)
	}
}

func testGroupsDeletedExplicit(t *testing.T) {
	selected, err := CompiledAndPersisted([]string{}, PersistedGroups{All: []string{"gone"}}, []string{"gone"})
	if err != nil {
		t.Fatalf("CompiledAndPersisted: %v", err)
	}
	if selected.Root || !slices.Equal(selected.Groups, []string{"gone"}) {
		t.Fatalf("selection = %+v, want explicit gone only", selected)
	}
	expanded, err := CompiledAndPersisted([]string{}, PersistedGroups{All: []string{"gone"}}, nil)
	if err != nil {
		t.Fatalf("CompiledAndPersisted: %v", err)
	}
	if !expanded.Root || expanded.Groups != nil {
		t.Fatalf("no-args selection = %+v, want deleted groups excluded", expanded)
	}
}

func testGroupsInactivePlatform(t *testing.T) {
	selected, err := CompiledAndPersisted([]string{}, PersistedGroups{Active: []string{"darwin-only"}}, nil)
	if err != nil {
		t.Fatalf("CompiledAndPersisted: %v", err)
	}
	if !selected.Root || !slices.Equal(selected.Groups, []string{"darwin-only"}) {
		t.Fatalf("selection = %+v, want inactive-platform rows to keep the group active", selected)
	}
	explicit, err := CompiledAndPersisted([]string{}, PersistedGroups{All: []string{"darwin-only"}}, []string{"darwin-only"})
	if err != nil {
		t.Fatalf("CompiledAndPersisted: %v", err)
	}
	if explicit.Root || !slices.Equal(explicit.Groups, []string{"darwin-only"}) {
		t.Fatalf("explicit selection = %+v, want darwin-only", explicit)
	}
}

func testGroupsDuplicates(t *testing.T) {
	if _, err := CompiledOnly([]string{"apps"}, []string{"apps", "apps"}); err == nil {
		t.Fatal("duplicate argument must be rejected")
	}
	if _, err := CompiledAndPersisted([]string{}, PersistedGroups{All: []string{"apps"}}, []string{"apps", "apps"}); err == nil {
		t.Fatal("duplicate argument must be rejected by the persisted path too")
	}
}

func testGroupsUnknown(t *testing.T) {
	if _, err := CompiledOnly([]string{"apps"}, []string{"missing"}); err == nil {
		t.Fatal("unknown argument must be rejected")
	}
	if _, err := CompiledAndPersisted([]string{}, PersistedGroups{All: []string{"apps"}}, []string{"missing"}); err == nil {
		t.Fatal("unknown argument must be rejected by the persisted path too")
	}
}

func testGroupsValidateStrict(t *testing.T) {
	if _, err := CompiledOnly([]string{}, []string{"ghost"}); err == nil {
		t.Fatal("validate selection must reject a group that exists only in state")
	}
}
