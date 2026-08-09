package reconcile

import "github.com/alyraffauf/cattery/internal/state"

// StateSnapshot is the immutable evaluation snapshot of every file and alias
// row of one canonical repository pair.
type StateSnapshot struct {
	repositoryRoot string
	homePath       string
	files          []FileState
	aliases        []AliasState
}

func (s StateSnapshot) RepositoryRoot() string { return s.repositoryRoot }
func (s StateSnapshot) HomePath() string       { return s.homePath }

func cloneRecords[T any](rows []T, clone func(*T)) []T {
	if rows == nil {
		return nil
	}
	out := make([]T, len(rows))
	copy(out, rows)
	for index := range out {
		clone(&out[index])
	}
	return out
}

// AllFiles returns a defensive copy of the file records.
func (s StateSnapshot) AllFiles() []FileState {
	return cloneRecords(s.files, func(record *FileState) { record.retiredAt = state.CloneTimestamp(record.retiredAt) })
}

// AllAliases returns a defensive copy of the alias records.
func (s StateSnapshot) AllAliases() []AliasState {
	return cloneRecords(s.aliases, func(record *AliasState) { record.retiredAt = state.CloneTimestamp(record.retiredAt) })
}
