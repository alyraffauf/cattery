// Package pathsafe owns the lexical, canonical, and portable-equivalence
// checks that keep every Cattery destination inside its allowed root. The
// rules cover lexical validation, filesystem containment, and deployment
// collisions.
//
// Validation never silently rewrites unsafe input. A rejected path is reported
// verbatim alongside the reason it was refused, so a caller can surface the
// original user input rather than a cleaned substitute.
package pathsafe

import (
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// PathError reports the original rejected input unchanged and the reason it was
// refused. The optional Cause preserves an underlying filesystem error so that
// errors.Is and errors.As keep working across the package boundary.
type PathError struct {
	Input  string
	Reason string
	Cause  error
}

// Error renders the reason, the verbatim input, and any wrapped cause.
func (e *PathError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Reason + " " + strconv.Quote(e.Input) + ": " + e.Cause.Error()
	}
	return e.Reason + " " + strconv.Quote(e.Input)
}

// Unwrap exposes the wrapped cause to errors.Is and errors.As.
func (e *PathError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Segments validates a HOME-relative destination path and returns its segment
// list. The input must be a non-empty UTF-8 string without NUL bytes, without a
// leading separator or volume prefix, and without empty, ".", or ".."
// segments. Unsafe input is reported verbatim; it is never cleaned into a
// different accepted path.
func Segments(path string) ([]string, error) {
	if reason := lexicalReason(path); reason != "" {
		return nil, &PathError{Input: path, Reason: reason}
	}
	segments := strings.Split(path, "/")
	for _, segment := range segments {
		if reason := segmentReason(segment); reason != "" {
			return nil, &PathError{Input: path, Reason: reason}
		}
	}
	return segments, nil
}

// GroupName validates a single repository group name. A group name is one
// non-empty filesystem segment: it may contain spaces and Unicode, is
// case-sensitive, and may not contain a slash or platform separator, a NUL
// byte, the reserved "." or ".." values, or a leading "." or "_" character.
func GroupName(name string) error {
	if reason := groupNameReason(name); reason != "" {
		return &PathError{Input: name, Reason: reason}
	}
	return nil
}

// lexicalReason returns the first lexical rejection reason for path, or the
// empty string when the path passes the non-segment lexical checks.
func lexicalReason(path string) string {
	switch {
	case path == "":
		return "empty path"
	case !utf8.ValidString(path):
		return "invalid utf-8"
	case strings.ContainsRune(path, '\x00'):
		return "nul byte"
	case filepath.IsAbs(path):
		return "absolute path"
	case filepath.VolumeName(path) != "":
		return "volume prefix"
	}
	return ""
}

// segmentReason returns the rejection reason for a single split segment, or the
// empty string when the segment is acceptable.
func segmentReason(segment string) string {
	switch segment {
	case "":
		return "empty segment"
	case ".":
		return "dot segment"
	case "..":
		return "dot-dot segment"
	}
	return ""
}

// groupNameReason returns the first rejection reason for a group name, or the
// empty string when the name is acceptable.
func groupNameReason(name string) string {
	switch {
	case name == "":
		return "empty group name"
	case !utf8.ValidString(name):
		return "invalid utf-8"
	case strings.ContainsRune(name, '\x00'):
		return "nul byte"
	case strings.ContainsAny(name, "/\\"):
		return "separator in group name"
	case isReservedName(name):
		return "reserved group name"
	case hasReservedPrefix(name):
		return "reserved group prefix"
	}
	return ""
}

func isReservedName(name string) bool {
	return name == "." || name == ".."
}

func hasReservedPrefix(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}
