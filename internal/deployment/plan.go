package deployment

// Plan is the immutable, validated deployment plan compiled from a repository
// for one platform. Slice fields are defensively copied on construction and
// on every read, so callers can never mutate a Plan through slices they held
// before construction or hold after an accessor returns.
type Plan struct {
	RepositoryRoot string
	Platform       string
	Groups         []string
	Files          []ManagedFile
	Aliases        []Alias
	Hooks          []Hook
}

// NewPlan constructs a Plan from candidate, defensively copying every input
// slice so the caller's source slices cannot mutate the plan later.
func NewPlan(candidate Plan) Plan {
	return Plan{
		RepositoryRoot: candidate.RepositoryRoot,
		Platform:       candidate.Platform,
		Groups:         copyStrings(candidate.Groups),
		Files:          copyFiles(candidate.Files),
		Aliases:        copyAliases(candidate.Aliases),
		Hooks:          copyHooks(candidate.Hooks),
	}
}

// AllGroups returns a defensive copy of the plan's group list.
func (p Plan) AllGroups() []string {
	return copyStrings(p.Groups)
}

// AllFiles returns a defensive copy of the plan's file list.
func (p Plan) AllFiles() []ManagedFile {
	return copyFiles(p.Files)
}

// AllAliases returns a defensive copy of the plan's alias list.
func (p Plan) AllAliases() []Alias {
	return copyAliases(p.Aliases)
}

// AllHooks returns a defensive copy of the plan's hook list.
func (p Plan) AllHooks() []Hook {
	return copyHooks(p.Hooks)
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
