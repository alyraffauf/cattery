package state

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestHashKeyFile(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"create and read round-trip", testKeyFileRoundTrip},
		{"exclusive creation rejects a duplicate", testKeyFileExclusiveCreate},
		{"creation rejects a symlink", testKeyFileSymlinkCreate},
		{"concurrent creation lets one winner win", testKeyFileConcurrentCreate},
		{"read reports a missing file", testKeyFileMissing},
		{"read rejects malformed lengths", testKeyFileMalformedLength},
		{"read rejects wrong modes", testKeyFileWrongMode},
		{"read rejects symlink and directory entries", testKeyFileNonRegular},
		{"read detects an interrupted write", testKeyFileInterruptedWrite},
		{"errors never expose key bytes", testKeyFileNoDiagnostics},
		{"store acquisition defers key creation", testKeyFileDeferred},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testKeyFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hash.key")
	keyFile := NewKeyFile(path)
	key, err := keyFile.Create()
	requireNoError(t, err)
	stored, err := os.ReadFile(path)
	requireNoError(t, err)
	if !bytes.Equal(stored, key[:]) {
		t.Fatalf("stored %d bytes, want the generated key", len(stored))
	}
	info, err := os.Lstat(path)
	requireNoError(t, err)
	if info.Mode().Perm() != stateFileMode {
		t.Fatalf("mode = %v, want %v", info.Mode().Perm(), stateFileMode)
	}
	loaded, err := keyFile.Read()
	requireNoError(t, err)
	if loaded != key {
		t.Fatal("Read returned a different key")
	}
}

func testKeyFileExclusiveCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hash.key")
	keyFile := NewKeyFile(path)
	first, err := keyFile.Create()
	requireNoError(t, err)
	second, err := keyFile.Create()
	if err == nil {
		t.Fatal("second Create succeeded against an existing file")
	}
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("second Create error = %v, want a file-exists error", err)
	}
	if second != [32]byte{} {
		t.Fatal("failed Create returned key bytes")
	}
	loaded, err := keyFile.Read()
	requireNoError(t, err)
	if loaded != first {
		t.Fatal("existing key changed")
	}
}

func testKeyFileSymlinkCreate(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("untouched"), stateFileMode); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	path := filepath.Join(directory, "hash.key")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := NewKeyFile(path).Create(); err == nil {
		t.Fatal("Create followed a symlink")
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(contents) != "untouched" {
		t.Fatalf("target changed to %q", contents)
	}
}

func testKeyFileConcurrentCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hash.key")
	keyFile := NewKeyFile(path)
	const workers = 8
	var group sync.WaitGroup
	successes := 0
	var lock sync.Mutex
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := keyFile.Create(); err == nil {
				lock.Lock()
				successes++
				lock.Unlock()
			}
		}()
	}
	group.Wait()
	if successes != 1 {
		t.Fatalf("successful creates = %d, want 1", successes)
	}
}

func testKeyFileMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hash.key")
	keyFile := NewKeyFile(path)
	if _, err := keyFile.Read(); err == nil {
		t.Fatal("Read succeeded on a missing file")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("Read created the key file: %v", err)
	}
}

func testKeyFileMalformedLength(t *testing.T) {
	for _, size := range []int{0, 1, 31, 33, 64} {
		path := filepath.Join(t.TempDir(), "hash.key")
		if err := os.WriteFile(path, make([]byte, size), stateFileMode); err != nil {
			t.Fatalf("seed %d bytes: %v", size, err)
		}
		if _, err := NewKeyFile(path).Read(); err == nil {
			t.Fatalf("Read accepted a %d-byte key file", size)
		}
	}
}

func testKeyFileWrongMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hash.key")
	if err := os.WriteFile(path, make([]byte, 32), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewKeyFile(path).Read(); err == nil {
		t.Fatal("Read accepted a 0644 key file")
	}
}

func testKeyFileNonRegular(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, make([]byte, 32), stateFileMode); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	link := filepath.Join(directory, "hash.key")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("seed symlink: %v", err)
	}
	if _, err := NewKeyFile(link).Read(); err == nil {
		t.Fatal("Read accepted a symlink")
	}
	directoryEntry := filepath.Join(directory, "dir-key")
	if err := os.Mkdir(directoryEntry, stateDirectoryMode); err != nil {
		t.Fatalf("seed directory: %v", err)
	}
	if _, err := NewKeyFile(directoryEntry).Read(); err == nil {
		t.Fatal("Read accepted a directory")
	}
}

func testKeyFileInterruptedWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hash.key")
	if err := os.WriteFile(path, []byte("torn"), stateFileMode); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewKeyFile(path).Read(); err == nil {
		t.Fatal("Read accepted a partially written key file")
	}
}

func testKeyFileNoDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hash.key")
	keyFile := NewKeyFile(path)
	key, err := keyFile.Create()
	requireNoError(t, err)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	_, readErr := keyFile.Read()
	if readErr == nil {
		t.Fatal("Read accepted a widened mode")
	}
	_, createErr := keyFile.Create()
	if createErr == nil {
		t.Fatal("second Create succeeded")
	}
	for _, diagnostic := range []string{readErr.Error(), createErr.Error()} {
		if bytes.Contains([]byte(diagnostic), key[:]) {
			t.Fatalf("diagnostic %q exposed the key", diagnostic)
		}
	}
}

func testKeyFileDeferred(t *testing.T) {
	deps := tempDependencies(t)
	store := NewStore(deps)
	if err := store.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer store.Close()
	if _, err := os.Lstat(filepath.Join(catteryDirFor(t, deps), stateKeyFileName)); !os.IsNotExist(err) {
		t.Fatalf("Acquire created hash.key: %v", err)
	}
}
