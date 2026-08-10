package apply

import (
	"context"

	"github.com/alyraffauf/cattery/internal/application/evaluation"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
)

// Preflight verifies the external dependencies the selected candidates
// require: SOPS is probed only when a secret candidate needs on-demand
// decryption (PLAN.md Sections 9.1 and 11.5). No version probing, state
// registration, prompt, hook, or mutation occurs.
func (service *Service) Preflight(ctx context.Context, candidates Candidates) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !needsSOPS(candidates) {
		return nil
	}
	if service.probe == nil {
		return failure.New(failure.Dependency, "apply: dependency probe is unavailable", nil)
	}
	return service.probe.Probe(ctx)
}

// needsSOPS reports whether any candidate requires SOPS before decisions: a
// secret record whose raw storage changed or which is unbaselined over a
// regular target must decrypt on demand.
func needsSOPS(candidates Candidates) bool {
	for _, candidate := range candidates.All() {
		if candidate.record.File.Kind == deployment.FileSecret && evaluation.SecretDecryptionNeeded(candidate.record) {
			return true
		}
	}
	return false
}
