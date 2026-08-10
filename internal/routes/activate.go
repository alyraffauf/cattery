package routes

import (
	"fmt"
	"strings"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/pathsafe"
)

// Activate resolves the route declarations active for one platform against
// the scope's resolved canonical file targets. The active union is SectionAll
// plus the platform section; every canonical key must name a managed regular
// file in the same scope, and a repeated alias destination anywhere in the
// active union is a validation error. The returned records are sorted by
// alias destination.
func Activate(config Config, platform deployment.Layer, canonical []string) ([]deployment.Alias, error) {
	if !platform.Valid() {
		return nil, fmt.Errorf("routes: unknown platform %q", platform)
	}
	managed := canonicalSet(canonical)
	var records []deployment.Alias
	for _, declaration := range config.Declarations {
		if !activeSection(declaration.Section, platform) {
			continue
		}
		if !managed[declaration.Canonical] {
			return nil, fmt.Errorf("routes: canonical target %q is not a managed file", declaration.Canonical)
		}
		added, err := recordsForDeclaration(declaration, platform)
		if err != nil {
			return nil, err
		}
		records = append(records, added...)
	}
	if err := rejectDuplicates(records); err != nil {
		return nil, err
	}
	deployment.SortAliases(records)
	return records, nil
}

func recordsForDeclaration(declaration Declaration, platform deployment.Layer) ([]deployment.Alias, error) {
	var records []deployment.Alias
	for _, destination := range declaration.Aliases {
		if destination == declaration.Canonical {
			return nil, fmt.Errorf("routes: alias destination %q equals its canonical target", destination)
		}
		record, err := deployment.NewAlias(deployment.Alias{
			Platform:                    string(platform),
			CanonicalTargetRelativePath: declaration.Canonical,
			AliasRelativePath:           destination,
		})
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func rejectDuplicates(records []deployment.Alias) error {
	seen := map[string]bool{}
	for _, record := range records {
		if seen[record.AliasRelativePath] {
			return fmt.Errorf("routes: duplicate alias destination %q", record.AliasRelativePath)
		}
		seen[record.AliasRelativePath] = true
	}
	return nil
}

func activeSection(section Section, platform deployment.Layer) bool {
	switch platform {
	case deployment.LayerDarwin:
		return section == SectionAll || section == SectionDarwin
	case deployment.LayerLinux:
		return section == SectionAll || section == SectionLinux
	}
	return false
}

func canonicalSet(canonical []string) map[string]bool {
	managed := make(map[string]bool, len(canonical))
	for _, path := range canonical {
		managed[path] = true
	}
	return managed
}

// AliasPayload computes the exact relative symlink payload for the alias at
// destination pointing at canonical (PLAN.md Section 5.4): the payload is
// relative from the alias destination's parent directory, never absolute,
// and never needs to climb above the home root. Both paths must be valid
// HOME-relative paths.
//
// AliasPayload rejects only the case where the canonical segments that remain
// after the common prefix with the alias's parent directory are empty — i.e.
// when canonical is the alias parent or one of its ancestors, which would
// leave no target to point at. A self-referential single-segment alias where
// canonical == alias is not rejected here; that case is caught upstream by
// Activate (which errors when destination == canonical).
func AliasPayload(canonical, alias string) (string, error) {
	canonicalSegments, err := pathsafe.Segments(canonical)
	if err != nil {
		return "", err
	}
	aliasSegments, err := pathsafe.Segments(alias)
	if err != nil {
		return "", err
	}
	parent := aliasSegments[:len(aliasSegments)-1]
	common := commonPrefix(canonicalSegments, parent)
	remaining := canonicalSegments[common:]
	if len(remaining) == 0 {
		return "", fmt.Errorf("routes: alias %q descends into canonical %q", alias, canonical)
	}
	up := len(parent) - common
	if up == 0 {
		return strings.Join(remaining, "/"), nil
	}
	return strings.Repeat("../", up) + strings.Join(remaining, "/"), nil
}

func commonPrefix(first, second []string) int {
	length := min(len(first), len(second))
	common := 0
	for common < length && first[common] == second[common] {
		common++
	}
	return common
}
