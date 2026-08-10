package filesystem

import (
	"io/fs"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// ordinaryNewFileMode is the default for a new ordinary target;
// source executable bits are OR-combined onto it.
const ordinaryNewFileMode fs.FileMode = 0o644

// ordinaryReadWriteBits masks the read/write bits preserved from an existing target.
const ordinaryReadWriteBits fs.FileMode = 0o666

// OrdinaryTargetMode derives the mode for an ordinary target: read/write
// bits are preserved from an existing entry or default to 0644 for a new
// one, and executable bits always come from the source.
func OrdinaryTargetMode(existing fs.FileMode, sourceExec fs.FileMode, absent bool) fs.FileMode {
	if absent {
		return ordinaryNewFileMode | sourceExec
	}
	return existing&ordinaryReadWriteBits | sourceExec
}

// SecretTargetMode derives the exact secret mode: 0600, or 0700 when the
// encrypted source is executable. The policy itself
// lives in deployment so file and reconcile layers cannot drift apart.
func SecretTargetMode(sourceExec fs.FileMode) fs.FileMode {
	return deployment.SecretTargetMode(sourceExec)
}
