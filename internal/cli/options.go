package cli

// Options carries the command-local option values every adapter maps into
// its service request: the raw repository value and its presence, the raw
// group arguments in command-line order, and the verbosity policy.
type Options struct {
	Repository    string
	RepositorySet bool
	Groups        []string
	Verbose       bool
}

// GroupsCopy returns a defensive copy of the group arguments.
func (o Options) GroupsCopy() []string {
	return append([]string(nil), o.Groups...)
}
