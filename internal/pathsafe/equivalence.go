package pathsafe

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// SegmentsEquivalent reports whether two single path segments are portably
// equivalent for repository portability (PLAN.md Section 6.3). Each segment is
// normalized to Unicode NFC and then compared with strings.EqualFold. This
// deliberately treats case-only and NFC/NFD distinctions as collisions even on
// a host filesystem that could store both, preventing common APFS aliases from
// diverging across machines.
//
// EqualFold is a predicate rather than a canonicalizing transform, so callers
// must never derive a lowercase "key" from this comparison; use the pairwise
// helpers below instead.
func SegmentsEquivalent(a, b string) bool {
	return strings.EqualFold(norm.NFC.String(a), norm.NFC.String(b))
}

// PathsEquivalent reports whether two complete segment lists collide: they must
// have the same length and every corresponding segment must be portably
// equivalent.
func PathsEquivalent(aSegments, bSegments []string) bool {
	if len(aSegments) != len(bSegments) {
		return false
	}
	for index, segment := range aSegments {
		if !SegmentsEquivalent(segment, bSegments[index]) {
			return false
		}
	}
	return true
}

// IsParentEquivalent reports whether parentSegments is a portable strict prefix
// of childSegments: the parent must be shorter, and every leading child segment
// must be portably equivalent to the corresponding parent segment.
func IsParentEquivalent(parentSegments, childSegments []string) bool {
	if len(parentSegments) >= len(childSegments) {
		return false
	}
	for index, segment := range parentSegments {
		if !SegmentsEquivalent(segment, childSegments[index]) {
			return false
		}
	}
	return true
}
