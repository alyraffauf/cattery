package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AliasSpec is the desired relative payload of one alias and whether the
// caller has confirmed replacing an occupied path.
type AliasSpec struct {
	Payload   string
	Overwrite bool
}

type aliasDecision struct {
	spec    AliasSpec
	outcome aliasOutcome
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
		if precondition.Target().Payload() == spec.Payload {
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
	if err := validateAliasPayload(spec.Payload); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := precondition.revalidateBeforeCreatingParent(); err != nil {
		return 0, err
	}
	outcome, err := classifyAlias(precondition, spec)
	if err != nil {
		return 0, err
	}
	return r.realizeOutcome(ctx, precondition, aliasDecision{spec: spec, outcome: outcome})
}

func (r *Replacer) realizeOutcome(ctx context.Context, precondition Precondition, decision aliasDecision) (AliasRealization, error) {
	switch decision.outcome {
	case outcomeCreate:
		return AliasCreated, r.commitAlias(ctx, precondition, decision.spec.Payload)
	case outcomeExact:
		if err := precondition.Revalidate(); err != nil {
			return 0, err
		}
		return AliasExact, nil
	case outcomeOccupied:
		return r.replaceOccupied(ctx, precondition, decision.spec)
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
// result.
func (r *Replacer) commitAlias(ctx context.Context, precondition Precondition, payload string) error {
	if err := r.prepareAliasParent(precondition); err != nil {
		return err
	}
	temp, err := prepareAliasLink(filepath.Dir(targetPath(precondition.Destination())), payload)
	if err != nil {
		return err
	}
	defer func() { _ = r.remove(temp) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.publishAlias(ctx, temp, precondition)
}

func (r *Replacer) publishAlias(ctx context.Context, temp string, precondition Precondition) error {
	if err := precondition.Revalidate(); err != nil {
		return err
	}
	if err := walkParentsValid(precondition.Destination().Root, precondition.Destination().Relative); err != nil {
		return err
	}
	if err := r.rename(temp, targetPath(precondition.Destination())); err != nil {
		return err
	}
	return r.syncer.Sync(ctx, filepath.Dir(targetPath(precondition.Destination())))
}

func (r *Replacer) prepareAliasParent(precondition Precondition) error {
	destination := precondition.Destination()
	if err := ensureParents(destination.Root, destination.Relative); err != nil {
		return err
	}
	return walkParentsValid(destination.Root, destination.Relative)
}

// validateAliasPayload accepts only the exact lexical form that may be stored
// in a symlink. In particular, cleaned or absolute paths are not equivalent.
func validateAliasPayload(payload string) error {
	if payload == "" {
		return fmt.Errorf("filesystem: empty alias payload")
	}
	if strings.HasPrefix(payload, "/") {
		return fmt.Errorf("filesystem: absolute alias payload %q", payload)
	}
	for _, segment := range strings.Split(payload, "/") {
		if segment == "" || segment == "." {
			return fmt.Errorf("filesystem: invalid alias payload segment %q", segment)
		}
	}
	return nil
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
