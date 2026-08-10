package apply

import (
	"context"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/reconcile"
)

// Revalidate re-captures every selected source and target after before
// hooks and compares each against the evaluated facts. Any mismatch stops the
// apply with zero executor or managed-row
// change.
func (service *Service) Revalidate(ctx context.Context, candidates Candidates) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, candidate := range candidates.All() {
		if err := service.revalidateOne(candidate, candidates.Home()); err != nil {
			return err
		}
	}
	return nil
}

// revalidateOne re-captures one candidate's source and target preconditions.
func (service *Service) revalidateOne(candidate Candidate, home string) error {
	if candidate.record.Entry == reconcile.PlanEntryFile {
		fresh, err := reconcile.CaptureSource(candidate.record.File, service.secrets)
		if err != nil {
			return failure.New(failure.Operational, "apply: re-capture source "+candidate.record.File.SourceRepositoryPath, err)
		}
		if err := sourceStable(candidate, fresh.Snapshot()); err != nil {
			return err
		}
	}
	target, err := reconcile.CaptureTarget(reconcile.Destination{Root: home, Relative: candidate.record.TargetPath})
	if err != nil {
		return failure.New(failure.Operational, "apply: re-capture target "+candidate.record.TargetPath, err)
	}
	return targetStable(candidate, target)
}

// sourceStable requires the re-captured source to match the evaluated
// facts exactly: identity, type, stored bytes, and executable bits.
func sourceStable(candidate Candidate, fresh reconcile.SourceSnapshot) error {
	before := candidate.record.Source.Snapshot()
	switch {
	case !pathsafe.SameIdentity(fresh.Identity(), before.Identity()):
		return failure.New(failure.Operational, "apply: source identity changed during apply: "+before.Path(), nil)
	case fresh.Kind() != before.Kind():
		return failure.New(failure.Operational, "apply: source type changed during apply: "+before.Path(), nil)
	case fresh.Semantic() != before.Semantic() || fresh.Storage() != before.Storage():
		return failure.New(failure.Operational, "apply: source content changed during apply: "+before.Path(), nil)
	case fresh.Executable() != before.Executable():
		return failure.New(failure.Operational, "apply: source mode changed during apply: "+before.Path(), nil)
	}
	return nil
}

// targetStable requires the re-captured target to match the evaluated facts
// exactly: identity, kind, parent identity, and for regular files the
// stored bytes and mode.
func targetStable(candidate Candidate, fresh reconcile.TargetSnapshot) error {
	before := candidate.record.Target
	path := candidate.record.TargetPath
	switch {
	case fresh.Kind() != before.Kind():
		return failure.New(failure.Operational, "apply: target type changed during apply: "+path, nil)
	case before.Kind() != reconcile.KindAbsent && !pathsafe.SameIdentity(fresh.Identity(), before.Identity()):
		return failure.New(failure.Operational, "apply: target identity changed during apply: "+path, nil)
	case before.Kind() != reconcile.KindAbsent && !pathsafe.SameIdentity(fresh.Parent(), before.Parent()):
		return failure.New(failure.Operational, "apply: target parent changed during apply: "+path, nil)
	case fresh.Kind() == reconcile.KindFile && (fresh.Digest() != before.Digest() || fresh.Mode() != before.Mode()):
		return failure.New(failure.Operational, "apply: target content changed during apply: "+path, nil)
	}
	return nil
}
