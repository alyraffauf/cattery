// Package filesystem owns the atomic same-directory mutation primitives that
// deploy files and aliases without ever publishing a partially written entry
// freeze, rewrite, revalidate, rename, directory sync.
package filesystem

import (
	"crypto/sha256"
)

// Destination names one HOME-relative target beneath a canonical root.
type Destination struct {
	Root     string
	Relative string
}

// EntryKind names the object type observed at one destination path.
type EntryKind int

const (
	KindAbsent EntryKind = iota
	KindFile
	KindDirectory
	KindSymlink
	KindSpecial
)

// Valid reports whether the kind is one of the known filesystem entry kinds.
func (k EntryKind) Valid() bool { return k >= KindAbsent && k <= KindSpecial }

// ContentToken is an immutable digest of exact bytes; equal tokens prove
// equal content without retaining the bytes themselves.
type ContentToken [32]byte

// TokenOfContent derives the content token of data.
func TokenOfContent(data []byte) ContentToken { return sha256.Sum256(data) }
