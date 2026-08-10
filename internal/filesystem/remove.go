package filesystem

import (
	"context"
	"fmt"
	"path/filepath"
)

// RemoveResult records whether the source was removed and its parent directory
// was made durable.
type RemoveResult struct {
	Removed         bool
	DirectorySynced bool
}

// RemoveResult removes a frozen regular file without following a substituted
// path. It first renames the file to a same-directory tombstone, so a failure
// before the rename leaves the managed source intact.
func (r *Replacer) RemoveResult(ctx context.Context, precondition Precondition) (RemoveResult, error) {
	if err := ctx.Err(); err != nil {
		return RemoveResult{}, err
	}
	if precondition.Target().Kind() != KindFile {
		return RemoveResult{}, fmt.Errorf("filesystem: removal target is not a regular file")
	}
	if err := validatePublication(precondition); err != nil {
		return RemoveResult{}, err
	}
	destination := precondition.Destination()
	directory := filepath.Dir(targetPath(destination))
	tombstone, handle, err := r.create(directory, ".removal-*")
	if err != nil {
		return RemoveResult{}, err
	}
	if err := CommitFile(ctx, handle); err != nil {
		r.discard(&temporaryFile{temp: tombstone, handle: handle})
		return RemoveResult{}, err
	}
	if err := r.remove(tombstone.Name()); err != nil {
		return RemoveResult{}, err
	}
	if err := r.rename(targetPath(destination), tombstone.Name()); err != nil {
		return RemoveResult{}, err
	}
	result := RemoveResult{Removed: true}
	if _, err := r.syncer.SyncResult(ctx, directory); err != nil {
		return result, err
	}
	if err := r.remove(tombstone.Name()); err != nil {
		return result, err
	}
	if _, err := r.syncer.SyncResult(ctx, directory); err != nil {
		return result, err
	}
	result.DirectorySynced = true
	return result, nil
}
