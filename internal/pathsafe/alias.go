package pathsafe

import (
	"fmt"
	"strings"
)

// RelativeAliasPayload returns the relative symlink payload from alias's
// parent directory to canonical.
func RelativeAliasPayload(canonical, alias string) (string, error) {
	canonicalSegments, err := Segments(canonical)
	if err != nil {
		return "", err
	}
	aliasSegments, err := Segments(alias)
	if err != nil {
		return "", err
	}
	parent := aliasSegments[:len(aliasSegments)-1]
	common := commonPrefix(canonicalSegments, parent)
	remaining := canonicalSegments[common:]
	if len(remaining) == 0 {
		return "", fmt.Errorf("pathsafe: alias %q descends into canonical %q", alias, canonical)
	}
	backtrack := len(parent) - common
	if backtrack == 0 {
		return strings.Join(remaining, "/"), nil
	}
	return strings.Repeat("../", backtrack) + strings.Join(remaining, "/"), nil
}

func commonPrefix(first, second []string) int {
	shared := 0
	for shared < len(first) && shared < len(second) && first[shared] == second[shared] {
		shared++
	}
	return shared
}
