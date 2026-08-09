package state

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestStateLock(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"acquire and release", testLockAcquireRelease},
		{"contention fails immediately", testLockContention},
		{"writes process id", testLockWritesProcessID},
		{"rejects symlink lock file", testRejectsSymlinkLock},
		{"rejects non-regular lock file", testRejectsNonRegularLock},
		{"rejects wrong lock mode", testRejectsWrongLockMode},
		{"release is idempotent", testReleaseIsIdempotent},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testLockAcquireRelease(t *testing.T) {
	lock := NewLock(tempLockPath(t))
	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	second := NewLock(lock.Path())
	if err := second.Acquire(); err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
	_ = second.Release()
}

func testLockContention(t *testing.T) {
	path := tempLockPath(t)
	first := NewLock(path)
	if err := first.Acquire(); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer first.Release()
	second := NewLock(path)
	if err := second.Acquire(); err == nil {
		t.Fatal("second Acquire succeeded while the lock was held")
	}
}

func testLockWritesProcessID(t *testing.T) {
	path := tempLockPath(t)
	lock := NewLock(path)
	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lock.Release()
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := strconv.Itoa(os.Getpid())
	if string(bytes) != want {
		t.Fatalf("lock contents = %q, want PID %q", string(bytes), want)
	}
	if modeOf(t, path) != stateFileMode {
		t.Fatalf("lock mode %o, want %o", modeOf(t, path), stateFileMode)
	}
}

func testRejectsSymlinkLock(t *testing.T) {
	target := filepath.Join(prepareCatteryDirectory(t), stateLockFileName)
	if err := os.Symlink("/dev/null", target); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := NewLock(target).Acquire(); err == nil {
		t.Fatal("Acquire accepted a symlink lock file")
	}
}

func testRejectsNonRegularLock(t *testing.T) {
	target := filepath.Join(prepareCatteryDirectory(t), stateLockFileName)
	if err := os.Mkdir(target, stateFileMode); err != nil {
		t.Fatal(err)
	}
	if err := NewLock(target).Acquire(); err == nil {
		t.Fatal("Acquire accepted a non-regular lock entry")
	}
}

func testRejectsWrongLockMode(t *testing.T) {
	target := filepath.Join(prepareCatteryDirectory(t), stateLockFileName)
	createFile(t, target, 0o644)
	if err := NewLock(target).Acquire(); err == nil {
		t.Fatal("Acquire accepted a lock file with the wrong mode")
	}
}

func testReleaseIsIdempotent(t *testing.T) {
	lock := NewLock(tempLockPath(t))
	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("third Release: %v", err)
	}
}

func tempLockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(prepareCatteryDirectory(t), stateLockFileName)
}

func prepareCatteryDirectory(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), catteryDirectoryName)
	if err := os.MkdirAll(directory, stateDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, stateDirectoryMode); err != nil {
		t.Fatal(err)
	}
	return directory
}
