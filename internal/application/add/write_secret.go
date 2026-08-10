package add

import (
	"context"
	"fmt"

	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/secrets"
)

// publishInput bundles the per-secret write inputs under the parameter limit.
type publishInput struct {
	plaintext    []byte
	precondition filesystem.Precondition
	item         ItemPlan
}

// writeSecret encrypts one target's plaintext, validates the candidate
// round-trip, and atomically writes the ciphertext source. The plaintext
// buffer is returned through the outcome so execute can verify the target
// still matches and compute the keyed baseline; on any failure it is cleared
// here so no plaintext leaks past the write.
func (service *Service) writeSecret(ctx context.Context, identity RepositoryIdentity, item ItemPlan) (writeOutcome, error) {
	precondition, err := filesystem.Freeze(filesystem.Destination{Root: identity.Root, Relative: item.SourceRepositoryPath()})
	if err != nil {
		return writeOutcome{}, freezeError(item, err)
	}
	plaintext, err := readValidatedTarget(item.TargetAbsolutePath())
	if err != nil {
		return writeOutcome{}, err
	}
	validated, result, err := service.publishSecret(ctx, publishInput{plaintext: plaintext, precondition: precondition, item: item})
	if err != nil {
		clearBytes(plaintext)
		return writeOutcome{}, err
	}
	return writeOutcome{published: validated, target: plaintext, result: result}, nil
}

// publishSecret encrypts, validates, and publishes one secret source. The
// caller owns the plaintext buffer and clears it after verification.
func (service *Service) publishSecret(ctx context.Context, input publishInput) ([]byte, filesystem.ReplaceResult, error) {
	if service.write.Secrets == nil {
		return nil, filesystem.ReplaceResult{}, fmt.Errorf("add: secret writer is not configured")
	}
	ciphertext, err := service.write.Secrets.Encrypt(ctx, input.plaintext, input.item.SourceRepositoryPath())
	if err != nil {
		return nil, filesystem.ReplaceResult{}, err
	}
	validated, err := service.write.Secrets.ValidateCandidate(ctx, secrets.Candidate{
		Plaintext: input.plaintext, Ciphertext: ciphertext, SourcePath: input.item.SourceRepositoryPath(),
	})
	if err != nil {
		return nil, filesystem.ReplaceResult{}, err
	}
	mode := sourceMode(input.precondition.Target(), input.item.ExecutableBits())
	result, err := service.deps.Writer.ReplaceResult(ctx, input.precondition, filesystem.ReplacementSpec{Content: validated, Mode: mode})
	if err != nil {
		return nil, filesystem.ReplaceResult{}, err
	}
	return validated, result, nil
}

// clearBytes overwrites one buffer with zeros; Go does not guarantee erasure,
// so this is exposure reduction for plaintext that must not outlive the write.
func clearBytes(buffer []byte) {
	for index := range buffer {
		buffer[index] = 0
	}
}
