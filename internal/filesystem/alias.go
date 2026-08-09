package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// AliasSpec is the desired relative payload of one alias and whether the
// caller has confirmed replacing an occupied path (PLAN.md Section 5.4).
type AliasSpec struct {
	Payload   string
	Overwrite bool
}

// AliasRealization names the outcome of one alias operation.
type AliasRealization int

const (
	// AliasCreated is a freshly installed relative link.
	AliasCreated AliasRealization = iota
	// AliasExact is an existing link carrying the exact payload.
	AliasExact
	// AliasReplaced is an occupied entry atomically replaced by a fresh
	// relative link.
	AliasReplaced
)

// AliasDriftError reports an existing alias path whose entry is not the
// desired relative link and was not confirmed for replacement.
type AliasDriftError struct {
	Path    string
	Payload string
}

func (e *AliasDriftError) Error() string {
	return fmt.Sprintf("filesystem: alias path %s does not point to %s", e.Path, e.Payload)
}

// aliasOutcome classifies the frozen entry against the desired payload.
type aliasOutcome int

const (
	outcomeCreate aliasOutcome = iota
	outcomeExact
	outcomeOccupied
	outcomeUnsupported
)

// classifyAlias names the realization a frozen entry requires.
func classifyAlias(precondition Precondition, spec AliasSpec) (aliasOutcome, error) {
	switch kind := precondition.Target().Kind(); kind {
	case KindAbsent:
		return outcomeCreate, nil
	case KindSymlink:
		live, err := readLinkPayload(targetPath(precondition.Destination()))
		if err != nil {
			return 0, err
		}
		if live == spec.Payload {
			return outcomeExact, nil
		}
		return outcomeOccupied, nil
	case KindFile:
		return outcomeOccupied, nil
	default:
		return outcomeUnsupported, nil
	}
}

// RealizeAlias creates, verifies, or replaces one relative symlink entry
// without ever following a final referent. A missing destination creates
// the link; an existing symlink with the exact payload is a no-op; any
// other occupied entry requires a confirmed overwrite and is replaced
// atomically; directories and special entries fail with manual
// intervention.
func (r *Replacer) RealizeAlias(ctx context.Context, precondition Precondition, spec AliasSpec) (AliasRealization, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := precondition.Revalidate(); err != nil {
		return 0, err
	}
	outcome, err := classifyAlias(precondition, spec)
	if err != nil {
		return 0, err
	}
	switch outcome {
	case outcomeCreate:
		return AliasCreated, r.commitAlias(ctx, precondition, spec.Payload)
	case outcomeExact:
		return AliasExact, nil
	case outcomeOccupied:
		return r.replaceOccupied(ctx, precondition, spec)
	default:
		return 0, fmt.Errorf("filesystem: alias path %s requires manual intervention", targetPath(precondition.Destination()))
	}
}

// replaceOccupied handles an existing symlink or file at the alias path:
// drift when replacement was not confirmed, otherwise a fresh relative link
// renamed over the entry.
func (r *Replacer) replaceOccupied(ctx context.Context, precondition Precondition, spec AliasSpec) (AliasRealization, error) {
	if !spec.Overwrite {
		return 0, &AliasDriftError{Path: targetPath(precondition.Destination()), Payload: spec.Payload}
	}
	if err := r.commitAlias(ctx, precondition, spec.Payload); err != nil {
		return 0, err
	}
	return AliasReplaced, nil
}

// commitAlias prepares a uniquely named relative symlink, revalidates the
// destination, renames it into place, and makes the parent directory
// durable. Only the rename or a barrier failure can publish a partial
// result (PLAN.md Section 7.2 steps 10-11).
func (r *Replacer) commitAlias(ctx context.Context, precondition Precondition, payload string) error {
	temp, err := prepareAliasLink(filepath.Dir(targetPath(precondition.Destination())), payload)
	if err != nil {
		return err
	}
	defer func() { _ = r.remove(temp) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := precondition.Revalidate(); err != nil {
		return err
	}
	if err := r.rename(temp, targetPath(precondition.Destination())); err != nil {
		return err
	}
	return r.syncer.Sync(ctx, filepath.Dir(targetPath(precondition.Destination())))
}

// prepareAliasLink reserves a unique name in dir, then creates a relative
// symlink with the payload there. The prepared link is never opened, so no
// referent is followed.
func prepareAliasLink(dir, payload string) (string, error) {
	file, err := os.CreateTemp(dir, ".alias-*")
	if err != nil {
		return "", fmt.Errorf("filesystem: reserve alias temp %s: %w", dir, err)
	}
	temp := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(temp)
		return "", err
	}
	if err := os.Remove(temp); err != nil {
		return "", err
	}
	if err := os.Symlink(payload, temp); err != nil {
		return "", fmt.Errorf("filesystem: prepare alias %s: %w", temp, err)
	}
	return temp, nil
}
