package filesystem

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// ordinaryNewFileMode is the default for a new ordinary target (Section 7.1);
// source executable bits are OR-combined onto it.
const ordinaryNewFileMode fs.FileMode = 0o644

// ordinaryReadWriteBits masks the read/write bits preserved from an existing target.
const ordinaryReadWriteBits fs.FileMode = 0o666

// OrdinaryTargetMode derives the mode for an ordinary target: read/write
// bits are preserved from an existing entry or default to 0644 for a new
// one, and executable bits always come from the source (PLAN.md Section 4.6).
func OrdinaryTargetMode(existing fs.FileMode, sourceExec fs.FileMode, absent bool) fs.FileMode {
	if absent {
		return ordinaryNewFileMode | sourceExec
	}
	return existing&ordinaryReadWriteBits | sourceExec
}

// SecretTargetMode derives the exact secret mode: 0600, or 0700 when the
// encrypted source is executable (PLAN.md Section 4.5). The policy itself
// lives in deployment so file and reconcile layers cannot drift apart.
func SecretTargetMode(sourceExec fs.FileMode) fs.FileMode {
	return deployment.SecretTargetMode(sourceExec)
}

// ApplyMode rematerializes the target even when it has one link. This avoids a
// chmod race and makes mode-only corrections obey the same identity and atomic
// publication rules as content changes.
func (r *Replacer) ApplyMode(ctx context.Context, precondition Precondition, desired fs.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := precondition.Revalidate(); err != nil {
		return err
	}
	content, err := readTargetContent(precondition)
	if err != nil {
		return err
	}
	return r.Replace(ctx, precondition, ReplacementSpec{Content: content, Mode: desired})
}

// readTargetContent reads the destination bytes bound to the frozen
// precondition identity. Opening the target and re-stating the descriptor
// closes the Lstat-to-read TOCTOU gap: a swap between Revalidate and the
// read either changes the inode (SameFileInfo fails) or the rename-based
// publication in Replace still rejects a stale identity.
func readTargetContent(precondition Precondition) ([]byte, error) {
	path := targetPath(precondition.Destination())
	handle, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("filesystem: read target %s: %w", path, err)
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return nil, fmt.Errorf("filesystem: read target %s: %w", path, err)
	}
	if !precondition.Target().Identity().SameFileInfo(info) {
		return nil, fmt.Errorf("filesystem: target identity changed at %s", path)
	}
	content, err := io.ReadAll(handle)
	if err != nil {
		return nil, fmt.Errorf("filesystem: read target %s: %w", path, err)
	}
	return content, nil
}
