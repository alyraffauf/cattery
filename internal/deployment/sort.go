package deployment

import (
	"slices"
	"sort"
)

// LessScope reports whether scope a precedes b in bytewise group order. The
// root scope (empty Group) sorts first because the empty string is the
// bytewise minimum.
func LessScope(a, b Scope) bool {
	return a.Group < b.Group
}

// LessManagedFile reports whether file a precedes b. Comparison is bytewise on
// TargetRelativePath, then Layer, then Kind, establishing a total order so
// stable sorts are deterministic across permutations.
func LessManagedFile(a, b ManagedFile) bool {
	if a.TargetRelativePath != b.TargetRelativePath {
		return a.TargetRelativePath < b.TargetRelativePath
	}
	if a.Layer != b.Layer {
		return a.Layer < b.Layer
	}
	return a.Kind < b.Kind
}

// LessAlias reports whether alias a precedes b in bytewise AliasRelativePath
// order.
func LessAlias(a, b Alias) bool {
	return a.AliasRelativePath < b.AliasRelativePath
}

// LessHook reports whether hook a precedes b. Comparison is bytewise on
// (phase, scope.Group, name). The apply orchestrator, not this comparator,
// owns final execution order between phases and scopes.
func LessHook(a, b Hook) bool {
	if a.Phase != b.Phase {
		return a.Phase < b.Phase
	}
	if a.Scope.Group != b.Scope.Group {
		return a.Scope.Group < b.Scope.Group
	}
	return a.Name < b.Name
}

// SortFiles sorts files in place by LessManagedFile, stably.
func SortFiles(files []ManagedFile) {
	sort.SliceStable(files, indexLess(files, LessManagedFile))
}

// SortAliases sorts aliases in place by LessAlias, stably.
func SortAliases(aliases []Alias) {
	sort.SliceStable(aliases, indexLess(aliases, LessAlias))
}

// SortHooks sorts hooks in place by LessHook, stably.
func SortHooks(hooks []Hook) {
	sort.SliceStable(hooks, indexLess(hooks, LessHook))
}

// SortGroups returns a copy of groups sorted in bytewise order with duplicates
// removed.
func SortGroups(groups []string) []string {
	sorted := copyStrings(groups)
	slices.Sort(sorted)
	return compactStrings(sorted)
}

// indexLess adapts a bytewise element comparator into the index-based callback
// shape that sort.SliceStable expects, for any slice element type.
func indexLess[T any](items []T, less func(a, b T) bool) func(int, int) bool {
	return func(i, j int) bool {
		return less(items[i], items[j])
	}
}

func compactStrings(sorted []string) []string {
	if len(sorted) == 0 {
		return sorted
	}
	write := 1
	for read := 1; read < len(sorted); read++ {
		if sorted[read] != sorted[read-1] {
			sorted[write] = sorted[read]
			write++
		}
	}
	return sorted[:write]
}
