package secretlifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"

	applicationrepository "github.com/alyraffauf/cattery/internal/application/repository"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/filesystem"
)

// List returns repository metadata without invoking SOPS or reading HOME.
func (service *Service) List(_ context.Context, request Request) (Result, error) {
	_, selected, err := service.resolveAndSelect(request)
	if err != nil {
		return Result{}, err
	}
	return resultWithStatus(selected, ""), nil
}

// Verify decrypts each selected source independently and exposes no content.
func (service *Service) Verify(ctx context.Context, request Request) (Result, error) {
	identity, selected, err := service.resolveAndSelect(request)
	if err != nil {
		return Result{}, err
	}
	service.deps.Secrets.SetDirectory(identity.Root)
	result := Result{Items: make([]Item, 0, len(selected))}
	var failures []error
	for _, source := range selected {
		status := "verified"
		if err := service.verifyOne(ctx, identity, source); err != nil {
			status = "failed"
			failures = append(failures, err)
		}
		result.Items = append(result.Items, itemOf(source, status))
	}
	return result, errors.Join(failures...)
}

func (service *Service) verifyOne(ctx context.Context, identity applicationrepository.RepositoryIdentity, selected selectedSource) error {
	content, _, err := readSource(identity.Root, selected)
	if err != nil {
		return itemFailure("verify", selected, err)
	}
	defer clearBytes(content)
	plaintext, err := service.deps.Secrets.Decrypt(ctx, content, selected.source.Candidate.SourceRepoPath)
	if err != nil {
		return itemFailure("verify", selected, err)
	}
	clearBytes(plaintext)
	return nil
}

// Reencrypt validates fresh ciphertext for every item and publishes it only
// when Yes is set. Preview is the default and reports a Difference result.
func (service *Service) Reencrypt(ctx context.Context, request Request) (Result, error) {
	if request.Yes && request.DryRun {
		return Result{}, failure.New(failure.InvalidInput, "secrets reencrypt: --yes and --dry-run cannot be combined", nil)
	}
	identity, selected, err := service.resolveAndSelect(request)
	if err != nil {
		return Result{}, err
	}
	service.deps.Secrets.SetDirectory(identity.Root)
	result := Result{Items: make([]Item, 0, len(selected))}
	var failures []error
	for _, source := range selected {
		status, err := service.reencryptOne(ctx, identity, source, request.Yes)
		if err != nil {
			status = "failed"
			failures = append(failures, err)
		}
		result.Items = append(result.Items, itemOf(source, status))
	}
	if len(failures) > 0 {
		return result, errors.Join(failures...)
	}
	if !request.Yes && len(selected) > 0 {
		return result, failure.New(failure.Difference, "secrets reencrypt: changes pending", nil)
	}
	return result, nil
}

func (service *Service) reencryptOne(ctx context.Context, identity applicationrepository.RepositoryIdentity, selected selectedSource, publish bool) (string, error) {
	ciphertext, frozen, err := readSource(identity.Root, selected)
	if err != nil {
		return "failed", itemFailure("reencrypt", selected, err)
	}
	defer clearBytes(ciphertext)

	reencrypted, err := service.rotate(ctx, selected, ciphertext)
	if err != nil {
		return "failed", err
	}
	defer clearBytes(reencrypted)
	if !publish {
		return "planned", nil
	}

	mode := frozen.Target().Mode()
	published, err := service.deps.Writer.ReplaceResult(ctx, frozen, filesystem.ReplacementSpec{Content: reencrypted, Mode: mode})
	if err != nil || !published.Renamed || !published.DirectorySynced {
		return "failed", itemFailure("reencrypt", selected, err)
	}
	if _, err := service.deps.Baselines.RefreshSecretSourceHash(identity.Root, identity.Home,
		selected.source.Candidate.SourceRepoPath, deployment.RawStorage(reencrypted)); err != nil {
		return "failed", itemFailure("refresh state", selected, err)
	}
	return "reencrypted", nil
}

func (service *Service) rotate(ctx context.Context, selected selectedSource, ciphertext []byte) ([]byte, error) {
	path := selected.source.Candidate.SourceRepoPath
	plaintext, err := service.deps.Secrets.Decrypt(ctx, ciphertext, path)
	if err != nil {
		return nil, itemFailure("reencrypt", selected, err)
	}
	defer clearBytes(plaintext)

	reencrypted, err := service.deps.Secrets.Encrypt(ctx, plaintext, path)
	if err != nil {
		return nil, itemFailure("reencrypt", selected, err)
	}
	validated, err := service.deps.Secrets.Decrypt(ctx, reencrypted, path)
	if err != nil {
		clearBytes(reencrypted)
		return nil, itemFailure("validate", selected, err)
	}
	equal := bytes.Equal(plaintext, validated)
	clearBytes(validated)
	if !equal {
		clearBytes(reencrypted)
		return nil, itemFailure("validate", selected, errors.New("round-trip plaintext mismatch"))
	}
	return reencrypted, nil
}

func readSource(root string, selected selectedSource) ([]byte, filesystem.Precondition, error) {
	path := selected.source.Candidate.SourceRepoPath
	frozen, err := filesystem.Freeze(filesystem.Destination{Root: root, Relative: path})
	if err != nil {
		return nil, frozen, err
	}
	content, err := filesystem.ReadFrozen(frozen)
	return content, frozen, err
}

func resultWithStatus(selected []selectedSource, status string) Result {
	result := Result{Items: make([]Item, 0, len(selected))}
	for _, source := range selected {
		result.Items = append(result.Items, itemOf(source, status))
	}
	return result
}

func itemFailure(operation string, selected selectedSource, cause error) error {
	path := selected.source.Candidate.SourceRepoPath
	if cause == nil {
		cause = fs.ErrInvalid
	}
	kind, ok := failure.HasKind(cause)
	if !ok {
		kind = failure.Operational
	}
	return failure.New(kind, fmt.Sprintf("secrets %s: %q", operation, path), cause)
}

func clearBytes(content []byte) {
	for index := range content {
		content[index] = 0
	}
}
