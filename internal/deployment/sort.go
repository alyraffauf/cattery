package deployment

import "sort"

// lessManagedFile reports whether file a precedes b in a bytewise total order.
func lessManagedFile(a, b ManagedFile) bool {
	if a.TargetRelativePath != b.TargetRelativePath {
		return a.TargetRelativePath < b.TargetRelativePath
	}
	if a.Layer != b.Layer {
		return a.Layer < b.Layer
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Scope.Group != b.Scope.Group {
		return a.Scope.Group < b.Scope.Group
	}
	if a.SourceRepositoryPath != b.SourceRepositoryPath {
		return a.SourceRepositoryPath < b.SourceRepositoryPath
	}
	if a.SourceAbsolutePath != b.SourceAbsolutePath {
		return a.SourceAbsolutePath < b.SourceAbsolutePath
	}
	return a.SourceExecutableBits < b.SourceExecutableBits
}

// lessAlias reports whether alias a precedes b in bytewise AliasRelativePath
// order.
func lessAlias(a, b Alias) bool {
	return a.AliasRelativePath < b.AliasRelativePath
}

// lessHook reports whether hook a precedes b. Comparison is bytewise on
// (phase, scope.Group, name). The apply orchestrator, not this comparator,
// owns final execution order between phases and scopes.
func lessHook(a, b Hook) bool {
	if a.Phase != b.Phase {
		return a.Phase < b.Phase
	}
	if a.Scope.Group != b.Scope.Group {
		return a.Scope.Group < b.Scope.Group
	}
	return a.Name < b.Name
}

// SortFiles sorts files in place by target path, stably.
func SortFiles(files []ManagedFile) {
	sort.SliceStable(files, indexLess(files, lessManagedFile))
}

// SortAliases sorts aliases in place by alias path, stably.
func SortAliases(aliases []Alias) {
	sort.SliceStable(aliases, indexLess(aliases, lessAlias))
}

// SortHooks sorts hooks in place by phase, scope, and name, stably.
func SortHooks(hooks []Hook) {
	sort.SliceStable(hooks, indexLess(hooks, lessHook))
}

func indexLess[T any](items []T, less func(a, b T) bool) func(int, int) bool {
	return func(i, j int) bool {
		return less(items[i], items[j])
	}
}
