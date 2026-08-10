package pathsafe

import "os"

// Identity is an immutable, read-only token capturing the filesystem facts of a
// single path at capture time: its mode type, size, and the stat identity used
// by os.SameFile plus the modification time of a regular file. It is only
// constructed by FilesystemIdentity and is safe to compare across goroutines.
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
// snapshot. Two hard links to one inode compare equal; a zero-value Identity
// (for example after a capture failure) never matches.
func SameIdentity(a, b Identity) bool {
	if a.info == nil || b.info == nil {
		return false
	}
	return sameSnapshot(a.info, b.info)
}

// SameFileInfo reports whether info names the same object snapshot as the
// captured identity. It is used when a path is opened after an Lstat snapshot.
func (i Identity) SameFileInfo(info os.FileInfo) bool {
	if i.info == nil || info == nil {
		return false
	}
	return sameSnapshot(i.info, info)
}

// sameSnapshot reports whether two stats name the same object snapshot.
// os.SameFile compares device and inode, which a filesystem may recycle when
// an entry is removed and recreated; a regular file's mtime lives on the inode,
// so a recycled inode carries a new mtime. Directories are excluded because
// creating entries inside them changes the directory itself.
func sameSnapshot(a, b os.FileInfo) bool {
	if !os.SameFile(a, b) {
		return false
	}
	if a.Mode().IsRegular() && b.Mode().IsRegular() && !a.ModTime().Equal(b.ModTime()) {
		return false
	}
	return true
}
