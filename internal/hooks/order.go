package hooks

import (
	"sort"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// SortBefore orders before hooks into repository scope, then groups
// lexically, then names bytewise. After hooks keep their relative order.
func SortBefore(hooks []deployment.Hook) {
	sort.SliceStable(hooks, func(first, second int) bool {
		firstHook, secondHook := hooks[first], hooks[second]
		if firstHook.Phase != secondHook.Phase {
			return firstHook.Phase == deployment.HookBefore
		}
		if firstHook.Phase == deployment.HookBefore {
			return LessBefore(firstHook, secondHook)
		}
		return false
	})
}

// SortAfter orders after hooks into groups lexically, then repository scope
// last, then names bytewise. Before hooks keep their relative order.
func SortAfter(hooks []deployment.Hook) {
	sort.SliceStable(hooks, func(first, second int) bool {
		firstHook, secondHook := hooks[first], hooks[second]
		if firstHook.Phase != secondHook.Phase {
			return firstHook.Phase == deployment.HookBefore
		}
		if firstHook.Phase == deployment.HookAfter {
			return LessAfter(firstHook, secondHook)
		}
		return false
	})
}

// LessBefore reports whether a precedes b among before-phase hooks: repository
// scope first, then groups bytewise, then names bytewise.
func LessBefore(a, b deployment.Hook) bool {
	if a.Scope.Group != b.Scope.Group {
		if a.Scope.Group == "" {
			return true
		}
		if b.Scope.Group == "" {
			return false
		}
		return a.Scope.Group < b.Scope.Group
	}
	return a.Name < b.Name
}

// LessAfter reports whether a precedes b among after-phase hooks: groups
// bytewise first, repository scope last, then names bytewise.
func LessAfter(a, b deployment.Hook) bool {
	if a.Scope.Group != b.Scope.Group {
		if a.Scope.Group == "" {
			return false
		}
		if b.Scope.Group == "" {
			return true
		}
		return a.Scope.Group < b.Scope.Group
	}
	return a.Name < b.Name
}
