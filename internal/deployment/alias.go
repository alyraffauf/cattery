package deployment

import "fmt"

// HookPhase names the position a hook runs in relative to file deployment. The
// apply orchestrator, not this type, owns final ordering between phases.
type HookPhase string

const (
	HookBefore HookPhase = "before"
	HookAfter  HookPhase = "after"
)

// ParseHookPhase converts a raw string into a HookPhase, rejecting unknown
// values.
func ParseHookPhase(value string) (HookPhase, error) {
	phase := HookPhase(value)
	if !phase.Valid() {
		return "", fmt.Errorf("deployment: unknown hook phase %q", value)
	}
	return phase, nil
}

// Valid reports whether phase is one of the supported constants.
func (p HookPhase) Valid() bool {
	switch p {
	case HookBefore, HookAfter:
		return true
	}
	return false
}

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

// Hook describes one validated hook descriptor. Final execution order is owned
// by the apply orchestrator; this descriptor carries identity and absolute
// location only.
type Hook struct {
	Scope          Scope
	Phase          HookPhase
	Name           string
	AbsolutePath   string
	RepositoryPath string
}

// NewHook validates candidate field-by-field and returns it on success.
func NewHook(candidate Hook) (Hook, error) {
	if err := validateHook(candidate); err != nil {
		return Hook{}, err
	}
	return candidate, nil
}

func validateHook(hook Hook) error {
	if !hook.Phase.Valid() {
		return fmt.Errorf("deployment: hook has invalid phase")
	}
	if hook.Name == "" {
		return fmt.Errorf("deployment: hook has empty name")
	}
	if hook.AbsolutePath == "" {
		return fmt.Errorf("deployment: hook %q missing absolute path", hook.Name)
	}
	if hook.RepositoryPath == "" {
		return fmt.Errorf("deployment: hook %q missing repository path", hook.Name)
	}
	return nil
}
