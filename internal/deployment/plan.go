package deployment

import "fmt"

// Plan is the immutable deployment plan compiled from a repository for one
// platform.
type Plan struct {
	repositoryRoot string
	platform       string
	groups         []string
	files          []ManagedFile
	aliases        []Alias
	hooks          []Hook
}

// PlanInput contains the validated records used to construct a Plan.
type PlanInput struct {
	RepositoryRoot string
	Platform       string
	Groups         []string
	Files          []ManagedFile
	Aliases        []Alias
	Hooks          []Hook
}

// NewPlan validates and freezes input. The returned Plan does not share any
// slice storage with input or with a later accessor result.
func NewPlan(input PlanInput) (Plan, error) {
	if err := validatePlanInput(input); err != nil {
		return Plan{}, err
	}
	return Plan{
		repositoryRoot: input.RepositoryRoot,
		platform:       input.Platform,
		groups:         copyStrings(input.Groups),
		files:          copyFiles(input.Files),
		aliases:        copyAliases(input.Aliases),
		hooks:          copyHooks(input.Hooks),
	}, nil
}

func validatePlanInput(input PlanInput) error {
	if input.RepositoryRoot == "" {
		return fmt.Errorf("deployment: plan has empty repository root")
	}
	if input.Platform == "" {
		return fmt.Errorf("deployment: plan has empty platform")
	}
	if err := validateFiles(input.Files); err != nil {
		return err
	}
	if err := validateAliases(input.Aliases); err != nil {
		return err
	}
	if err := validateHooks(input.Hooks); err != nil {
		return err
	}
	return nil
}

func validateFiles(files []ManagedFile) error {
	for _, file := range files {
		if err := validateFile(file); err != nil {
			return err
		}
	}
	return nil
}

func validateAliases(aliases []Alias) error {
	for _, alias := range aliases {
		if err := validateAlias(alias); err != nil {
			return err
		}
	}
	return nil
}

func validateHooks(hooks []Hook) error {
	for _, hook := range hooks {
		if err := validateHook(hook); err != nil {
			return err
		}
	}
	return nil
}

func (p Plan) RepositoryRoot() string {
	return p.repositoryRoot
}

func (p Plan) Platform() string {
	return p.platform
}

func (p Plan) Groups() []string {
	return copyStrings(p.groups)
}

func (p Plan) Files() []ManagedFile {
	return copyFiles(p.files)
}

func (p Plan) Aliases() []Alias {
	return copyAliases(p.aliases)
}

func (p Plan) Hooks() []Hook {
	return copyHooks(p.hooks)
}

func copyStrings(items []string) []string {
	return append([]string(nil), items...)
}

func copyFiles(items []ManagedFile) []ManagedFile {
	return append([]ManagedFile(nil), items...)
}

func copyAliases(items []Alias) []Alias {
	return append([]Alias(nil), items...)
}

func copyHooks(items []Hook) []Hook {
	return append([]Hook(nil), items...)
}
