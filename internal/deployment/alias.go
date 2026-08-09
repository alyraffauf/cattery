package deployment

import "fmt"

// Alias describes one explicitly declared symlink pointing at a canonical
// target. Aliases are the only symlinks Cattery deploys; they are never the
// primary deployment strategy.
type Alias struct {
	Scope                       Scope
	Platform                    string
	CanonicalTargetRelativePath string
	AliasRelativePath           string
}

// NewAlias validates candidate field-by-field and returns it on success.
func NewAlias(candidate Alias) (Alias, error) {
	if err := validateAlias(candidate); err != nil {
		return Alias{}, err
	}
	return candidate, nil
}

func validateAlias(alias Alias) error {
	if alias.AliasRelativePath == "" {
		return fmt.Errorf("deployment: alias has empty alias relative path")
	}
	if alias.Platform == "" {
		return fmt.Errorf("deployment: alias %q has empty platform", alias.AliasRelativePath)
	}
	if alias.CanonicalTargetRelativePath == "" {
		return fmt.Errorf("deployment: alias %q has empty canonical target", alias.AliasRelativePath)
	}
	return nil
}
