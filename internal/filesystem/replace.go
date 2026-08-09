package filesystem

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// TempFile is the write path of one temporary replacement entry: exact
// bytes, final mode, and the entry name for rename and cleanup. *os.File
// satisfies it; tests inject fakes to fail the write and mode steps.
type TempFile interface {
	Write(p []byte) (int, error)
	Chmod(mode fs.FileMode) error
	Name() string
}

// ReplacementSpec is the validated content and final mode of one planned
// replacement. The caller freezes and validates the bytes before Replace;
// Replace never inspects state.
type ReplacementSpec struct {
	Content []byte
	Mode    fs.FileMode
}

// ReplaceResult records publication facts independently of the returned
// error. Renamed is true once the old destination may have been replaced;
// DirectorySynced distinguishes a fully durable result from a partial one.
type ReplaceResult struct {
	Renamed         bool
	DirectorySynced bool
}

// ReplaceError preserves the publication facts when Replace returns an
// error. Unwrap keeps existing errors.As/errors.Is callers working.
type ReplaceError struct {
	Result ReplaceResult
	Cause  error
}

func (e *ReplaceError) Error() string { return e.Cause.Error() }
func (e *ReplaceError) Unwrap() error { return e.Cause }

// temporaryFile pairs the write path with the sync/close lifecycle of one
// open replacement entry.
type temporaryFile struct {
	temp   TempFile
	handle SyncHandle
}

// prepare writes the exact validated bytes once, applies the final mode,
// commits the entry (sync then close), and revalidates the on-disk result so
// no renamed entry can carry wrong bytes or mode (PLAN.md Section 7.2 steps
// 5-9).
func (file *temporaryFile) prepare(ctx context.Context, spec ReplacementSpec) error {
	if _, err := file.temp.Write(spec.Content); err != nil {
		return fmt.Errorf("filesystem: write temporary file: %w", err)
	}
	if err := file.temp.Chmod(spec.Mode); err != nil {
		return fmt.Errorf("filesystem: mode temporary file: %w", err)
	}
	if err := CommitFile(ctx, file.handle); err != nil {
		return err
	}
	return validateReplacement(file.temp.Name(), TokenOfContent(spec.Content), spec.Mode)
}

// validateReplacement re-checks the on-disk temporary entry: it must be a
// regular file carrying exactly the intended bytes and mode.
func validateReplacement(name string, token ContentToken, mode fs.FileMode) error {
	facts, err := CaptureTarget(name)
	if err != nil {
		return err
	}
	if facts.Kind() != KindFile {
		return fmt.Errorf("filesystem: replacement is not a regular file at %s", name)
	}
	if facts.Token() != token {
		return fmt.Errorf("filesystem: replacement content mismatch at %s", name)
	}
	if facts.Mode() != mode {
		return fmt.Errorf("filesystem: replacement mode mismatch at %s", name)
	}
	return nil
}

// Replacer performs the atomic same-directory replacement of one target:
// create, write once, mode, sync, close, revalidate, rename, and a directory
// durability barrier. Dependencies are fields so tests inject failures at
// every boundary.
type Replacer struct {
	create func(dir, pattern string) (TempFile, SyncHandle, error)
	rename func(oldPath, newPath string) error
	remove func(path string) error
	syncer *DirectorySyncer
}

// NewReplacer returns a replacer backed by os.CreateTemp, os.Rename,
// os.Remove, and a real directory syncer.
func NewReplacer() *Replacer {
	return &Replacer{
		create: func(dir, pattern string) (TempFile, SyncHandle, error) {
			file, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, nil, err
			}
			return file, file, nil
		},
		rename: os.Rename,
		remove: os.Remove,
		syncer: NewDirectorySyncer(),
	}
}

// Replace performs the atomic replacement (PLAN.md Section 7.2): the exact
// validated bytes are written once to a same-directory temporary entry,
// committed, revalidated, and renamed over the target, then the parent
// directory is made durable. Failure before the rename leaves the old target
// intact and removes the temporary entry; only a rename or barrier failure
// can publish a partial result.
func (r *Replacer) Replace(ctx context.Context, precondition Precondition, spec ReplacementSpec) error {
	_, err := r.ReplaceResult(ctx, precondition, spec)
	return err
}

// ReplaceResult performs Replace while exposing partial durability facts. The
// error-only Replace method above preserves the original package seam.
func (r *Replacer) ReplaceResult(ctx context.Context, precondition Precondition, spec ReplacementSpec) (ReplaceResult, error) {
	file, err := r.prepareReplacement(ctx, precondition, spec)
	if err != nil {
		return ReplaceResult{}, err
	}
	published, err := r.publish(ctx, &file, precondition)
	if err != nil {
		return published, &ReplaceError{Result: published, Cause: err}
	}
	return published, nil
}

func (r *Replacer) prepareReplacement(ctx context.Context, precondition Precondition, spec ReplacementSpec) (temporaryFile, error) {
	if err := ctx.Err(); err != nil {
		return temporaryFile{}, err
	}
	if err := ensureParents(precondition.Destination().Root, precondition.Destination().Relative); err != nil {
		return temporaryFile{}, err
	}
	if err := precondition.Revalidate(); err != nil {
		return temporaryFile{}, err
	}
	dir := filepath.Dir(targetPath(precondition.Destination()))
	temp, handle, err := r.create(dir, ".replacement-*")
	if err != nil {
		return temporaryFile{}, err
	}
	file := temporaryFile{temp: temp, handle: handle}
	if err := file.prepare(ctx, spec); err != nil {
		r.discard(&file)
		return temporaryFile{}, err
	}
	return file, nil
}

// publish makes the committed entry the target: it renames the temporary
// entry over the destination, then makes the parent directory durable.
// Cancellation before the rename removes the entry; a rename or barrier
// failure is the only partial result.
func (r *Replacer) publish(ctx context.Context, file *temporaryFile, precondition Precondition) (ReplaceResult, error) {
	var result ReplaceResult
	destination := precondition.Destination()
	if err := ctx.Err(); err != nil {
		r.discard(file)
		return result, err
	}
	// This is intentionally the last destination check before publication.
	if err := validatePublication(precondition); err != nil {
		r.discard(file)
		return result, err
	}
	if err := r.rename(file.temp.Name(), targetPath(destination)); err != nil {
		r.discard(file)
		return result, err
	}
	result.Renamed = true
	return r.syncPublication(ctx, destination, result)
}

func validatePublication(precondition Precondition) error {
	destination := precondition.Destination()
	if err := walkParentsValid(destination.Root, destination.Relative); err != nil {
		return err
	}
	return precondition.Revalidate()
}

func (r *Replacer) syncPublication(ctx context.Context, destination Destination, result ReplaceResult) (ReplaceResult, error) {
	_, err := r.syncer.SyncResult(ctx, filepath.Dir(targetPath(destination)))
	if err == nil {
		result.DirectorySynced = true
	}
	return result, err
}

// discard removes a temporary entry that must not reach the target. The
// descriptor is already closed by prepare's commit.
func (r *Replacer) discard(file *temporaryFile) {
	_ = r.remove(file.temp.Name())
}
