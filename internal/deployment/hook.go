package deployment

import "fmt"

// HookPhase names the position a hook runs in relative to file deployment.
type HookPhase string

const (
	HookBefore HookPhase = "before"
	HookAfter  HookPhase = "after"
)

// ParseHookPhase converts a raw string into a HookPhase, rejecting unknown values.
func ParseHookPhase(value string) (HookPhase, error) {
	phase := HookPhase(value)
	if !phase.Valid() {
		return "", fmt.Errorf("deployment: unknown hook phase %q", value)
	}
	return phase, nil
}

func (p HookPhase) Valid() bool {
	switch p {
	case HookBefore, HookAfter:
		return true
	}
	return false
}

// Hook describes one validated hook descriptor.
type Hook struct {
	Scope          Scope
	Phase          HookPhase
	Name           string
	AbsolutePath   string
	RepositoryPath string
}

// NewHook validates candidate and returns it on success.
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
