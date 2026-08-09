package filesystem

import (
	"context"
	"fmt"
	"io/fs"
	"os"
)

// OrdinaryTargetMode derives the mode for an ordinary target: read/write
// bits are preserved from an existing entry or default to 0644 for a new
// one, and executable bits always come from the source (PLAN.md Section 4.6).
func OrdinaryTargetMode(existing fs.FileMode, sourceExec fs.FileMode, absent bool) fs.FileMode {
	if absent {
		return 0o644 | sourceExec
	}
	return existing&0o666 | sourceExec
}

// SecretTargetMode derives the exact secret mode: 0600, or 0700 when the
// encrypted source is executable (PLAN.md Section 4.5).
func SecretTargetMode(sourceExec fs.FileMode) fs.FileMode {
	if sourceExec&0o111 != 0 {
		return 0o700
	}
	return 0o600
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
	content, err := readTargetContent(targetPath(precondition.Destination()))
	if err != nil {
		return err
	}
	return r.Replace(ctx, precondition, ReplacementSpec{Content: content, Mode: desired})
}

func readTargetContent(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("filesystem: read target %s: %w", path, err)
	}
	return content, nil
}
