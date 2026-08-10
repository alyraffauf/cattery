package routes

import (
	"fmt"
	"strings"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// Activate resolves the route declarations active for one platform against
// the scope's resolved canonical targets. The active union is SectionAll plus
// the platform section; every canonical key must name a managed file or a
// managed directory (proved by a managed descendant) in the same scope, and a
// repeated alias destination anywhere in the active union is a validation
// error. The returned records are sorted by alias destination.
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
		if !managed(declaration.Canonical) {
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

func canonicalSet(canonical []string) func(string) bool {
	managed := make(map[string]bool, len(canonical))
	for _, path := range canonical {
		managed[path] = true
	}
	return func(candidate string) bool {
		if managed[candidate] {
			return true
		}
		for path := range managed {
			if strings.HasPrefix(path, candidate+"/") {
				return true
			}
		}
		return false
	}
}
