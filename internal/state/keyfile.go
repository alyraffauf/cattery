package state

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// stateKeyFileName is the 32-byte keyed-hash secret beside the database
// It is created on demand, never during Acquire.
const stateKeyFileName = "hash.key"

// keyByteLength is the fixed size of the keyed-hash secret.
const keyByteLength = 32

// KeyFile owns the 32-byte keyed-hash secret stored beside the database.
// Construction is side-effect free; every filesystem effect happens in Create
// or Read, which callers invoke only when a secret baseline requires the key.
type KeyFile struct {
	path string
}

// NewKeyFile binds a KeyFile to path.
func NewKeyFile(path string) *KeyFile {
	return &KeyFile{path: path}
}

// Path returns the path the key file is bound to.
func (keyFile *KeyFile) Path() string {
	return keyFile.path
}

// Create generates a fresh 32-byte key from crypto/rand and creates the file
// exclusively (O_EXCL) with mode 0600, syncing the file and its parent
// directory. A partial or duplicate creation is left on disk for strict
// validation to catch later; it is never silently overwritten.
func (keyFile *KeyFile) Create() ([32]byte, error) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return [32]byte{}, fmt.Errorf("state: read randomness for hash key: %w", err)
	}
	handle, err := createKeyHandle(keyFile.path)
	if err != nil {
		return [32]byte{}, err
	}
	if err := finishKeyFile(handle, key); err != nil {
		return [32]byte{}, err
	}
	return key, nil
}

// Read loads the stored key after strict validation: a final regular file of
// exactly 32 bytes with mode 0600, never a symlink or special entry.
func (keyFile *KeyFile) Read() ([32]byte, error) {
	handle, err := os.OpenFile(keyFile.path, os.O_RDONLY|syscall.O_NOFOLLOW, stateFileMode)
	if err != nil {
		if os.IsNotExist(err) {
			return [32]byte{}, keyMissingError{path: keyFile.path}
		}
		return [32]byte{}, fmt.Errorf("state: inspect hash key %q: %w", keyFile.path, err)
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return [32]byte{}, fmt.Errorf("state: inspect hash key %q: %w", keyFile.path, err)
	}
	if err := verifyPrivateFile(keyFile.path, info); err != nil {
		return [32]byte{}, err
	}
	contents, err := io.ReadAll(handle)
	if err != nil {
		return [32]byte{}, fmt.Errorf("state: read hash key %q: %w", keyFile.path, err)
	}
	return decodeKeyContents(keyFile.path, contents)
}

func decodeKeyContents(path string, contents []byte) ([32]byte, error) {
	if len(contents) != keyByteLength {
		return [32]byte{}, keyMalformedError{path: path}
	}
	var key [32]byte
	copy(key[:], contents)
	return key, nil
}

// createKeyHandle opens the key file with exclusive creation and forces mode
// 0600 so an unusual umask cannot narrow it.
func createKeyHandle(path string) (*os.File, error) {
	handle, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, stateFileMode)
	if err != nil {
		return nil, fmt.Errorf("state: create hash key %q: %w", path, err)
	}
	info, err := handle.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = handle.Close()
		if err == nil {
			err = errNotRegular(path, info.Mode())
		}
		return nil, fmt.Errorf("state: validate hash key %q: %w", path, err)
	}
	if err := handle.Chmod(stateFileMode); err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("state: set hash key mode %q: %w", path, err)
	}
	return handle, nil
}

// finishKeyFile writes the key, syncs the file and its parent directory, and
// closes the handle. Any failure closes the handle and leaves the partial file
// for strict validation to catch.
func finishKeyFile(handle *os.File, key [32]byte) error {
	written, err := handle.Write(key[:])
	if err != nil {
		_ = handle.Close()
		return fmt.Errorf("state: write hash key %q: %w", handle.Name(), err)
	}
	if written != len(key) {
		_ = handle.Close()
		return fmt.Errorf("state: write hash key %q: short write of %d bytes", handle.Name(), written)
	}
	if err := handle.Sync(); err != nil {
		_ = handle.Close()
		return fmt.Errorf("state: sync hash key %q: %w", handle.Name(), err)
	}
	if err := handle.Close(); err != nil {
		return fmt.Errorf("state: close hash key %q: %w", handle.Name(), err)
	}
	if err := syncParentDirectory(handle.Name()); err != nil {
		return err
	}
	return nil
}

// syncParentDirectory syncs the parent directory so a crash cannot lose the
// just-created directory entry.
func syncParentDirectory(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("state: open hash key parent: %w", err)
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return fmt.Errorf("state: sync hash key parent: %w", err)
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("state: close hash key parent: %w", err)
	}
	return nil
}

// keyMissingError signals an absent hash.key. Its Is method lets recovery
// detect the missing case regardless of the recorded path.
type keyMissingError struct {
	path string
}

func (e keyMissingError) Error() string {
	return fmt.Sprintf("state: hash key %q is missing", e.path)
}

func (e keyMissingError) Is(target error) bool {
	_, matched := target.(keyMissingError)
	return matched
}

// keyMalformedError signals a key file whose length or contents cannot be a
// valid 32-byte key. Its Is method lets recovery detect the malformed case.
type keyMalformedError struct {
	path string
}

func (e keyMalformedError) Error() string {
	return fmt.Sprintf("state: hash key %q is malformed; restore it or reset state", e.path)
}

func (e keyMalformedError) Is(target error) bool {
	_, matched := target.(keyMalformedError)
	return matched
}
