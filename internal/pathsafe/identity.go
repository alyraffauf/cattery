package pathsafe

import "os"

// Identity is an immutable, read-only token capturing the filesystem facts of a
// single path at capture time: its mode type, size, and the stat identity used
// by os.SameFile. It is only constructed by FilesystemIdentity and is safe to
// compare across goroutines.
type Identity struct {
	path string
	info os.FileInfo
}

// Path returns the path supplied when the identity was captured.
func (i Identity) Path() string {
	return i.path
}

// Mode returns the captured mode bits, or zero for a zero-value Identity.
func (i Identity) Mode() os.FileMode {
	if i.info == nil {
		return 0
	}
	return i.info.Mode()
}

// Size returns the captured size in bytes, or zero for a zero-value Identity.
func (i Identity) Size() int64 {
	if i.info == nil {
		return 0
	}
	return i.info.Size()
}

// IsDir reports whether the captured entry is a directory.
func (i Identity) IsDir() bool {
	if i.info == nil {
		return false
	}
	return i.info.IsDir()
}

// FilesystemIdentity captures an immutable, read-only snapshot of path using
// os.Lstat, never following a final symlink. A missing or unreachable path
// returns a descriptive PathError wrapping the underlying cause.
func FilesystemIdentity(path string) (Identity, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Identity{}, &PathError{Input: path, Reason: "stat identity", Cause: err}
	}
	return Identity{path: path, info: info}, nil
}

// SameIdentity reports whether two identities name the same filesystem object
// using os.SameFile semantics. Two hard links to one inode compare equal; a
// zero-value Identity (for example after a capture failure) never matches.
func SameIdentity(a, b Identity) bool {
	if a.info == nil || b.info == nil {
		return false
	}
	return os.SameFile(a.info, b.info)
}
