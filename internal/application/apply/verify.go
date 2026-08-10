package apply

import (
	"context"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/reconcile"
)

// Verify re-snapshots every selected source, target, and alias after hooks
// and downgrades any record whose facts no longer match its source to
// partial (PLAN.md Section 11.5). Verification never rewrites drift and
// performs no baseline or state commit; equality baselines were already
// established per durable write.
func (service *Service) Verify(ctx context.Context, records []ItemResult, candidates Candidates) ([]ItemResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	byPath := candidatesByPath(candidates)
	verified := append([]ItemResult(nil), records...)
	for index := range verified {
		if err := (verificationContext{service: service, context: ctx, candidates: byPath, home: candidates.Home()}).verify(&verified[index]); err != nil {
			return nil, err
		}
	}
	return verified, nil
}

type verificationContext struct {
	service    *Service
	context    context.Context
	candidates map[string]Candidate
	home       string
}

func (verification verificationContext) verify(record *ItemResult) error {
	if record.Status != StatusCompleted {
		return nil
	}
	candidate, ok := verification.candidates[record.TargetPath]
	if !ok || candidate.record.Entry == reconcile.PlanEntryNone {
		return nil
	}
	equal, err := verification.service.verifyOne(verification.context, candidate, verification.home)
	if err != nil {
		return err
	}
	if !equal {
		record.Status = StatusPartial
	}
	return nil
}

// verifyOne re-snapshots one candidate's source and target and reports
// whether the deployed target still matches the source facts.
func (service *Service) verifyOne(ctx context.Context, candidate Candidate, home string) (bool, error) {
	if candidate.record.Entry == reconcile.PlanEntryFile {
		equal, err := service.verifySource(ctx, candidate)
		if err != nil {
			return false, err
		}
		if !equal {
			return false, nil
		}
	}
	target, err := reconcile.CaptureTarget(reconcile.Destination{Root: home, Relative: candidate.record.TargetPath})
	if err != nil {
		return false, failure.New(failure.Operational, "apply: verify target "+candidate.record.TargetPath, err)
	}
	if candidate.record.Entry == reconcile.PlanEntryAlias {
		return service.verifyAlias(candidate, target)
	}
	return service.verifyFileTarget(ctx, candidate, target)
}

// verifySource reports whether the re-captured source still matches the
// evaluated storage and content facts exactly.
func (service *Service) verifySource(ctx context.Context, candidate Candidate) (bool, error) {
	fresh, err := reconcile.CaptureSource(candidate.record.File, service.secrets)
	if err != nil {
		return false, failure.New(failure.Operational, "apply: re-capture source "+candidate.record.File.SourceRepositoryPath, err)
	}
	snapshot := fresh.Snapshot()
	before := candidate.record.Source.Snapshot()
	if snapshot.Semantic() != before.Semantic() || snapshot.Storage() != before.Storage() {
		return false, nil
	}
	return true, nil
}

// verifyAlias requires the target to be a symlink carrying the exact
// derived payload.
func (service *Service) verifyAlias(candidate Candidate, target reconcile.TargetSnapshot) (bool, error) {
	payload, err := pathsafe.RelativeAliasPayload(
		candidate.record.Alias.CanonicalTargetRelativePath,
		candidate.record.Alias.AliasRelativePath,
	)
	if err != nil {
		return false, err
	}
	return target.Kind() == reconcile.KindSymlink && target.Payload() == payload, nil
}

// verifyFileTarget compares the regular target bytes with the source
// content: the retained ordinary digest or a SOPS recheck of the decrypted
// plaintext.
func (service *Service) verifyFileTarget(ctx context.Context, candidate Candidate, target reconcile.TargetSnapshot) (bool, error) {
	if target.Kind() != reconcile.KindFile {
		return false, nil
	}
	if candidate.record.File.Kind == deployment.FileSecret {
		plaintext, err := service.secrets.Decrypt(ctx, candidate.record.Source.Bytes(), candidate.record.File.SourceRepositoryPath)
		if err != nil {
			return false, failure.New(failure.Operational, "apply: verify secret "+candidate.record.TargetPath, err)
		}
		defer clearBytes(plaintext)()
		return target.Digest() == deployment.Ordinary(plaintext), nil
	}
	return target.Digest() == candidate.record.Source.Snapshot().Semantic(), nil
}
