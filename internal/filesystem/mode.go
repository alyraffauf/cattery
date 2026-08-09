package filesystem

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
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

// ApplyMode adjusts an existing regular target to the desired mode without
// ever mutating an unmanaged inode alias: a singly linked target is chmod'd
// in place, while a multiply linked target is replaced by a fresh
// same-content entry carrying the desired mode.
func (r *Replacer) ApplyMode(ctx context.Context, precondition Precondition, desired fs.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := precondition.Revalidate(); err != nil {
		return err
	}
	path := targetPath(precondition.Destination())
	links, err := linkCount(path)
	if err != nil {
		return err
	}
	if links <= 1 {
		if err := applyChmod(path, desired); err != nil {
			return err
		}
		return r.syncer.Sync(ctx, filepath.Dir(path))
	}
	return r.replaceLinkedTarget(ctx, precondition, desired)
}

// replaceLinkedTarget rewrites a multiply linked target with the same bytes
// and the desired mode so no other link is mutated.
func (r *Replacer) replaceLinkedTarget(ctx context.Context, precondition Precondition, desired fs.FileMode) error {
	content, err := readTargetContent(targetPath(precondition.Destination()))
	if err != nil {
		return err
	}
	return r.Replace(ctx, precondition, ReplacementSpec{Content: content, Mode: desired})
}

// applyChmod applies the desired mode to the target in place.
func applyChmod(path string, mode fs.FileMode) error {
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("filesystem: mode target %s: %w", path, err)
	}
	return nil
}

// linkCount reports how many hard links name the entry; one means no alias.
func linkCount(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("filesystem: stat links %s: %w", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("filesystem: unsupported stat layout for %s", path)
	}
	return uint64(stat.Nlink), nil
}

func readTargetContent(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("filesystem: read target %s: %w", path, err)
	}
	return content, nil
}
