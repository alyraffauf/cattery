package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/secrets"
)

const (
	sourceGrowthDetectionSlack int64 = 1
	jsonNullLiteral                  = "null"
)

// SourceObservation pairs a frozen source snapshot with the exact bytes read
// during capture. The bytes are retained for the write phase and can be
// explicitly cleared when the observation is no longer needed.
type SourceObservation struct {
	snapshot SourceSnapshot
	bytes    []byte
	secret   bool
	client   *secrets.Client
	relative string
}

// CaptureSource captures a regular source without following a final symlink.
// It validates the source before opening it, on the opened handle, and after
// reading it so a replacement or in-place mutation cannot be accepted.
func CaptureSource(file deployment.ManagedFile, client *secrets.Client) (SourceObservation, error) {
	if err := requireSourceFile(file); err != nil {
		return SourceObservation{}, err
	}
	return captureVerified(file, client)
}

func requireSourceFile(file deployment.ManagedFile) error {
	if file.SourceAbsolutePath == "" {
		return fmt.Errorf("reconcile: source capture requires an absolute path")
	}
	if !file.Kind.Valid() {
		return fmt.Errorf("reconcile: source has invalid kind %q", file.Kind)
	}
	return nil
}

func captureVerified(file deployment.ManagedFile, client *secrets.Client) (SourceObservation, error) {
	before, err := pathsafe.FilesystemIdentity(file.SourceAbsolutePath)
	if err != nil {
		return SourceObservation{}, fmt.Errorf("reconcile: inspect source %s: %w", file.SourceAbsolutePath, err)
	}
	if err := requireRegularSource(file.SourceAbsolutePath, before); err != nil {
		return SourceObservation{}, err
	}

	data, after, err := readVerifiedSource(file.SourceAbsolutePath, before)
	if err != nil {
		clear(data)
		return SourceObservation{}, err
	}
	if err := validateCapture(captureInput{file: file, before: before, after: after, data: data}); err != nil {
		clear(data)
		return SourceObservation{}, err
	}

	return SourceObservation{
		snapshot: sourceFacts(file, before, data),
		bytes:    data,
		secret:   file.Kind == deployment.FileSecret,
		client:   client,
		relative: file.SourceRepositoryPath,
	}, nil
}

type captureInput struct {
	file          deployment.ManagedFile
	before, after pathsafe.Identity
	data          []byte
}

func validateCapture(input captureInput) error {
	read := sourceRead{path: input.file.SourceAbsolutePath, before: input.before, after: input.after, data: input.data}
	if err := checkStableSource(read); err != nil {
		return err
	}
	if input.file.Kind == deployment.FileSecret {
		return checkSecretEnvelope(input.file.SourceAbsolutePath, input.data)
	}
	return nil
}

func requireRegularSource(path string, identity pathsafe.Identity) error {
	if filesystem.KindOfIdentity(identity) != KindFile {
		return fmt.Errorf("reconcile: source %s is not a regular file", path)
	}
	return nil
}

// readVerifiedSource performs Lstat, open/fstat, bounded read, fstat again,
// and a final Lstat. Reading at most one byte beyond the initial size catches
// growth without allowing an unbounded source into memory.
func readVerifiedSource(path string, before pathsafe.Identity) ([]byte, pathsafe.Identity, error) {
	handle, err := openSource(path, before)
	if err != nil {
		return nil, pathsafe.Identity{}, err
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, before.Size()+sourceGrowthDetectionSlack))
	if err != nil {
		return data, pathsafe.Identity{}, fmt.Errorf("reconcile: read source %s: %w", path, err)
	}
	readBack, err := handle.Stat()
	if err != nil {
		return data, pathsafe.Identity{}, fmt.Errorf("reconcile: re-stat source %s: %w", path, err)
	}
	if err := checkReadback(readback{path: path, before: before, info: readBack, data: data}); err != nil {
		return data, pathsafe.Identity{}, err
	}
	after, err := pathsafe.FilesystemIdentity(path)
	if err != nil {
		return data, pathsafe.Identity{}, fmt.Errorf("reconcile: source %s vanished while reading: %w", path, err)
	}
	return data, after, nil
}

type readback struct {
	path   string
	before pathsafe.Identity
	info   os.FileInfo
	data   []byte
}

func checkReadback(input readback) error {
	if err := checkHandle(input.path, input.before, input.info); err != nil {
		return err
	}
	if int64(len(input.data)) != input.info.Size() {
		return fmt.Errorf("reconcile: source %s changed size while reading", input.path)
	}
	return nil
}

func openSource(path string, before pathsafe.Identity) (*os.File, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reconcile: open source %s: %w", path, err)
	}
	opened, err := handle.Stat()
	if err != nil {
		handle.Close()
		return nil, fmt.Errorf("reconcile: stat open source %s: %w", path, err)
	}
	if err := checkHandle(path, before, opened); err != nil {
		handle.Close()
		return nil, err
	}
	return handle, nil
}

func checkHandle(path string, before pathsafe.Identity, info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("reconcile: source %s is no longer a regular file", path)
	}
	if info.Size() != before.Size() {
		return fmt.Errorf("reconcile: source %s changed size while opening", path)
	}
	if info.Mode().Perm()&deployment.ExecutableBitMask != before.Mode().Perm()&deployment.ExecutableBitMask {
		return fmt.Errorf("reconcile: source %s executable bits changed while reading", path)
	}
	return nil
}

type sourceRead struct {
	path          string
	before, after pathsafe.Identity
	data          []byte
}

func checkStableSource(read sourceRead) error {
	if !pathsafe.SameIdentity(read.before, read.after) {
		return fmt.Errorf("reconcile: source %s replaced while reading", read.path)
	}
	if filesystem.KindOfIdentity(read.after) != KindFile {
		return fmt.Errorf("reconcile: source %s is no longer a regular file", read.path)
	}
	if read.after.Size() != int64(len(read.data)) {
		return fmt.Errorf("reconcile: source %s changed size while reading", read.path)
	}
	if read.after.Mode().Perm()&deployment.ExecutableBitMask != read.before.Mode().Perm()&deployment.ExecutableBitMask {
		return fmt.Errorf("reconcile: source %s executable bits changed while reading", read.path)
	}
	return nil
}

func checkSecretEnvelope(path string, data []byte) error {
	var envelope struct {
		Sops json.RawMessage `json:"sops"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("reconcile: secret source %s is not a sops JSON document: %w", path, err)
	}
	if len(envelope.Sops) == 0 || string(envelope.Sops) == jsonNullLiteral {
		return fmt.Errorf("reconcile: secret source %s lacks sops metadata", path)
	}
	return nil
}

func sourceFacts(file deployment.ManagedFile, identity pathsafe.Identity, data []byte) SourceSnapshot {
	snapshot := SourceSnapshot{
		path: file.SourceAbsolutePath, identity: identity, kind: KindFile,
		token: filesystem.TokenOfContent(data), executable: identity.Mode().Perm() & deployment.ExecutableBitMask,
	}
	if file.Kind == deployment.FileOrdinary {
		snapshot.semantic = deployment.Ordinary(data)
	} else {
		snapshot.storage = deployment.RawStorage(data)
	}
	return snapshot
}

// Snapshot returns the immutable facts captured from the source.
func (observation SourceObservation) Snapshot() SourceSnapshot { return observation.snapshot }

// Bytes returns the captured bytes. Callers must not modify them and must
// call Clear when the observation is no longer needed.
func (observation SourceObservation) Bytes() []byte { return observation.bytes }

// KeyedSecretSemantic decrypts a secret only when semantic comparison is requested.
// The plaintext is cleared before returning, including when hashing succeeds.
func (observation *SourceObservation) KeyedSecretSemantic(ctx context.Context, key [32]byte) (digest deployment.Digest, err error) {
	if !observation.secret {
		return deployment.Digest{}, fmt.Errorf("reconcile: keyed semantics require a secret source %s", observation.snapshot.path)
	}
	if observation.client == nil || len(observation.bytes) == 0 {
		return deployment.Digest{}, fmt.Errorf("reconcile: secret source %s cannot decrypt", observation.snapshot.path)
	}
	plaintext, err := observation.client.Decrypt(ctx, observation.bytes, observation.relative)
	if err != nil {
		return deployment.Digest{}, err
	}
	defer clear(plaintext)
	return deployment.SecretSemantic(plaintext, key), nil
}

// Clear zeroes and releases the retained capture buffer.
func (observation *SourceObservation) Clear() {
	clear(observation.bytes)
	observation.bytes = nil
}
