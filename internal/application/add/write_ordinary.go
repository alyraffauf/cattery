package add

import (
	"context"
	"io"
	"io/fs"
	"os"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/filesystem"
)

// writeOutcome carries the published source content and the pre-write target
// content of one source replacement. For an ordinary write both fields hold
// the target bytes; for a secret write published holds the ciphertext and
// target holds the plaintext the caller verifies and then clears.
type writeOutcome struct {
	published []byte
	target    []byte
	result    filesystem.ReplaceResult
}

// writeOrdinary atomically writes one ordinary source from freshly validated
// target bytes. The source mode is 0644 plus exec bits when
// the source is new, or the preserved read/write bits plus exec bits when it
// already exists.
func (service *Service) writeOrdinary(ctx context.Context, identity RepositoryIdentity, item ItemPlan) (writeOutcome, error) {
	precondition, err := filesystem.Freeze(filesystem.Destination{Root: identity.Root, Relative: item.SourceRepositoryPath()})
	if err != nil {
		return writeOutcome{}, freezeError(item, err)
	}
	target, err := readValidatedTarget(item.TargetAbsolutePath())
	if err != nil {
		return writeOutcome{}, err
	}
	mode := sourceMode(precondition.Target(), item.ExecutableBits())
	result, err := service.deps.Writer.ReplaceResult(ctx, precondition, filesystem.ReplacementSpec{Content: target, Mode: mode})
	if err != nil {
		return writeOutcome{}, err
	}
	return writeOutcome{published: target, target: target, result: result}, nil
}

// sourceMode applies the mode policy to the frozen source: a new source takes
// 0644 plus exec bits; an existing source preserves its read/write bits.
func sourceMode(target filesystem.TargetFacts, exec fs.FileMode) fs.FileMode {
	if target.Kind() == filesystem.KindAbsent {
		return filesystem.OrdinaryTargetMode(0, exec, true)
	}
	return filesystem.OrdinaryTargetMode(target.Mode(), exec, false)
}

// readValidatedTarget opens one target, confirms it is a regular file, reads
// it fully, and re-confirms the descriptor is unchanged. This closes the gap
// between preflight's snapshot and the write.
func readValidatedTarget(absolute string) ([]byte, error) {
	handle, err := os.Open(absolute)
	if err != nil {
		return nil, failure.New(failure.Operational, "add: open target "+absolute, err)
	}
	defer handle.Close()
	before, err := handle.Stat()
	if err != nil {
		return nil, failure.New(failure.Operational, "add: stat target "+absolute, err)
	}
	if !before.Mode().IsRegular() {
		return nil, failure.New(failure.InvalidInput, "add: target "+absolute+" is not a regular file", nil)
	}
	content, err := io.ReadAll(handle)
	if err != nil {
		return nil, failure.New(failure.Operational, "add: read target "+absolute, err)
	}
	if err := confirmTargetStable(handle, before, absolute); err != nil {
		return nil, err
	}
	return content, nil
}

// confirmTargetStable re-stats the open descriptor and rejects a target that
// changed kind or identity between the read and the re-stat.
func confirmTargetStable(handle *os.File, before os.FileInfo, absolute string) error {
	after, err := handle.Stat()
	if err != nil {
		return failure.New(failure.Operational, "add: re-stat target "+absolute, err)
	}
	if !os.SameFile(before, after) || !after.Mode().IsRegular() {
		return failure.New(failure.Operational, "add: target "+absolute+" changed while reading", nil)
	}
	return nil
}

// freezeError wraps a source freeze failure with the repository-relative path.
func freezeError(item ItemPlan, err error) error {
	return failure.New(failure.Operational, "add: freeze source "+item.SourceRepositoryPath(), err)
}
