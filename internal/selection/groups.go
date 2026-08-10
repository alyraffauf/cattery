package selection

import (
	"fmt"
	"slices"
	"sort"
)

// Selection is one validated group selection. Root reports whether root scope
// is included; Groups lists the sorted unique group names. A nil Groups with
// Root set means root scope plus every current group, mirroring the compiler
// convention that an empty selection filters nothing.
type Selection struct {
	Root   bool
	Groups []string
}

// PersistedGroups carries the distinct group names of a canonical repository
// pair's persisted rows, read once by the caller from the state store: Active
// names groups with at least one active file or alias row, All names groups
// with any row at all, active or retired. Layers are not considered, so rows
// of the inactive platform keep their group alive.
type PersistedGroups struct {
	Active []string
	All    []string
}

// CompiledOnly validates explicit group arguments against the current compiled
// groups. No arguments select root scope plus every current
// group; unknown and duplicate arguments are errors.
func CompiledOnly(current []string, arguments []string) (Selection, error) {
	if len(arguments) == 0 {
		return Selection{Root: true}, nil
	}
	if err := rejectUnknown(arguments, current); err != nil {
		return Selection{}, err
	}
	if err := rejectDuplicates(arguments); err != nil {
		return Selection{}, err
	}
	return Selection{Groups: sortedUnique(arguments)}, nil
}

// CompiledAndPersisted expands and validates a group selection against the
// current groups and the persisted rows. No arguments select
// root scope plus every current and active state-only group; an explicit name
// may exist in the current plan or any active/retired state row. Unknown and
// duplicate arguments are errors.
func CompiledAndPersisted(current []string, persisted PersistedGroups, arguments []string) (Selection, error) {
	if len(arguments) == 0 {
		return Selection{Root: true, Groups: sortedUnique(concat(current, persisted.Active))}, nil
	}
	known := concat(current, persisted.All)
	if err := rejectUnknown(arguments, known); err != nil {
		return Selection{}, err
	}
	if err := rejectDuplicates(arguments); err != nil {
		return Selection{}, err
	}
	return Selection{Groups: sortedUnique(arguments)}, nil
}

func rejectUnknown(arguments, known []string) error {
	for _, argument := range arguments {
		if !slices.Contains(known, argument) {
			return fmt.Errorf("selection: unknown group %q", argument)
		}
	}
	return nil
}

func rejectDuplicates(arguments []string) error {
	seen := make(map[string]bool, len(arguments))
	for _, argument := range arguments {
		if seen[argument] {
			return fmt.Errorf("selection: duplicate group %q", argument)
		}
		seen[argument] = true
	}
	return nil
}

func concat(first, second []string) []string {
	return append(append([]string(nil), first...), second...)
}

func sortedUnique(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	unique := make(map[string]bool, len(items))
	for _, item := range items {
		unique[item] = true
	}
	names := make([]string, 0, len(unique))
	for name := range unique {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
