package pathsafe

import (
	"path/filepath"
	"slices"
	"strings"
)

// ProtectedTree reports whether target equals or descends into the protected
// tree, checking the relation in both directions. Each relation is evaluated
// twice: once with canonical native absolute
// path segments compared by string equality, and once with the portable
// NFC-plus-EqualFold segment equivalence. The trees collide when either
// comparison overlaps, so case-only or NFC/NFD aliases are rejected even on a
// case-sensitive host. No lowercase key is ever derived.
func ProtectedTree(target, protected string) bool {
	targetSegments := segmentsOf(target)
	protectedSegments := segmentsOf(protected)
	return nativeOverlap(targetSegments, protectedSegments) ||
		PortableOverlap(targetSegments, protectedSegments)
}

// Equal reports whether two canonical absolute paths name the same native
// location by comparing their cleaned segment lists with string equality. It is
// the native building block of the overlap check.
func Equal(a, b string) bool {
	return slices.Equal(segmentsOf(a), segmentsOf(b))
}

// Contains reports whether parent is a strict native ancestor of child by
// comparing their cleaned segment lists.
func Contains(parent, child string) bool {
	return nativeAncestor(segmentsOf(parent), segmentsOf(child))
}

// nativeOverlap reports a collision when the two segment lists are equal or one
// is a strict native prefix of the other.
func nativeOverlap(target, protected []string) bool {
	return slices.Equal(target, protected) ||
		nativeAncestor(protected, target) ||
		nativeAncestor(target, protected)
}

// portableOverlap reports a collision when the two segment lists are portably
// equivalent or one is a strict portable prefix of the other.
func PortableOverlap(target, protected []string) bool {
	return PathsEquivalent(target, protected) ||
		IsParentEquivalent(protected, target) ||
		IsParentEquivalent(target, protected)
}

// nativeAncestor reports whether parent is a strict native prefix of child:
// shorter in length and every leading segment equal by string comparison.
func nativeAncestor(parent, child []string) bool {
	if len(parent) >= len(child) {
		return false
	}
	for index, segment := range parent {
		if child[index] != segment {
			return false
		}
	}
	return true
}

// segmentsOf returns the cleaned native path segments of path, dropping the
// leading separator so two absolute paths compare by their real components.
func segmentsOf(path string) []string {
	cleaned := filepath.Clean(path)
	raw := strings.Split(cleaned, string(filepath.Separator))
	segments := make([]string, 0, len(raw))
	for _, part := range raw {
		if part != "" {
			segments = append(segments, part)
		}
	}
	return segments
}
