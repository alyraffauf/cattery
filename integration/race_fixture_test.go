package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// RaceFixture extends the executable environment with deterministic
// filesystem and state failure injections for black-box precondition and
// partial-commit tests. No production file is patched.
type RaceFixture struct {
	execEnv
}

// NewRaceFixture builds one race environment over the executable.
func NewRaceFixture(t *testing.T) RaceFixture {
	t.Helper()
	return RaceFixture{execEnv: newExecEnv(t)}
}

// stateDir returns the cattery state directory.
func (race RaceFixture) stateDir() string {
	return filepath.Join(race.home, ".local", "state", "cattery")
}

// fOfdSetlk is the linux open-file-description write lock command.
const fOfdSetlk = 37

// lockStateWrites takes exclusive OFD write locks on the database and its
// WAL-index so a concurrent baseline commit times out and fails. The
// locks are released at cleanup.
func (race RaceFixture) lockStateWrites(t *testing.T) {
	t.Helper()
	files := race.lockFiles(t)
	t.Cleanup(func() {
		unlock := syscall.Flock_t{Type: syscall.F_UNLCK, Whence: 0, Start: 0, Len: 0}
		for _, file := range files {
			_ = fcntlLock(file, unlock)
			_ = file.Close()
		}
	})
}

// lockFiles opens and locks every state file.
func (race RaceFixture) lockFiles(t *testing.T) []*os.File {
	t.Helper()
	var files []*os.File
	lock := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	for _, name := range []string{"state.db", "state.db-shm", "state.db-wal"} {
		file, err := os.OpenFile(filepath.Join(race.stateDir(), name), os.O_RDWR, 0)
		if err != nil {
			closeAll(files)
			t.Fatalf("open %s: %v", name, err)
		}
		if err := fcntlLock(file, lock); err != nil {
			_ = file.Close()
			closeAll(files)
			t.Fatalf("lock %s: %v", name, err)
		}
		files = append(files, file)
	}
	return files
}

// closeAll closes every held file.
func closeAll(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}

// fcntlLock applies one OFD lock command to the file descriptor.
func fcntlLock(file *os.File, lock syscall.Flock_t) error {
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), fOfdSetlk, uintptr(unsafe.Pointer(&lock)))
	if errno != 0 {
		return errno
	}
	return nil
}

// blockTargetParent makes one target's parent directory read-only so the
// replacement rename fails before publication.
func (race RaceFixture) blockTargetParent(t *testing.T, relative string) {
	t.Helper()
	parent := filepath.Dir(filepath.Join(race.home, filepath.FromSlash(relative)))
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
}

// replaceDatabaseWithDirectory replaces the state database with a
// directory so the next open fails deterministically.
func (race RaceFixture) replaceDatabaseWithDirectory(t *testing.T) {
	t.Helper()
	database := filepath.Join(race.stateDir(), "state.db")
	if err := os.Remove(database); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(database, 0o700); err != nil {
		t.Fatal(err)
	}
}

// processHandle tracks one asynchronously started invocation.
type processHandle struct {
	done chan ProcessResult
}

// start launches the binary and returns a handle.
func (race RaceFixture) start(t *testing.T, args ...string) processHandle {
	t.Helper()
	command := exec.Command(race.fixture.Binary, args...)
	command.Env = []string{
		"HOME=" + race.home,
		"XDG_STATE_HOME=" + filepath.Join(race.home, ".local", "state"),
		"PATH=" + os.Getenv("PATH"),
	}
	command.Dir = race.home
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr
	done := make(chan ProcessResult, 1)
	if err := command.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	go func() {
		err := command.Wait()
		code := 0
		if err != nil {
			code = exitCodeOf(err)
		}
		done <- ProcessResult{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
	}()
	return processHandle{done: done}
}

// finish waits for the process outcome.
func (handle processHandle) finish(t *testing.T) ProcessResult {
	t.Helper()
	select {
	case result := <-handle.done:
		return result
	case <-time.After(30 * time.Second):
		t.Fatal("process did not finish")
		return ProcessResult{}
	}
}

// awaitTarget polls until the target carries the given content.
func (race RaceFixture) awaitTarget(t *testing.T, relative, want string) {
	t.Helper()
	path := filepath.Join(race.home, filepath.FromSlash(relative))
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(path)
		if err == nil && string(content) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("target %s never carried %q", relative, want)
}
