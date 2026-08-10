package filesystem

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/alyraffauf/cattery/internal/pathsafe"
)

// TargetFacts freezes the read-only facts of one destination entry: kind,
// identity, content token, permission mode, and symlink payload.
type TargetFacts struct {
	path     string
	kind     EntryKind
	identity pathsafe.Identity
	token    ContentToken
	mode     fs.FileMode
	payload  string
}

// Kind returns the frozen entry kind.
func (f TargetFacts) Kind() EntryKind { return f.kind }

// Identity returns the frozen filesystem identity; zero when absent.
func (f TargetFacts) Identity() pathsafe.Identity { return f.identity }

// Token returns the frozen content token of a regular file.
func (f TargetFacts) Token() ContentToken { return f.token }

// Mode returns the frozen permission bits.
func (f TargetFacts) Mode() fs.FileMode { return f.mode }

// Payload returns the exact referent of a symlink entry.
func (f TargetFacts) Payload() string { return f.payload }

// MatchesIdentityAndMode reports whether an opened entry still matches a
// captured identity and permission mode.
func MatchesIdentityAndMode(identity pathsafe.Identity, info os.FileInfo, mode fs.FileMode) bool {
	return identity.SameFileInfo(info) && info.Mode().Perm() == mode
}

// KindOfIdentity classifies an existing identity without touching the path.
func KindOfIdentity(identity pathsafe.Identity) EntryKind {
	mode := identity.Mode()
	switch {
	case mode.IsDir():
		return KindDirectory
	case mode&os.ModeSymlink != 0:
		return KindSymlink
	case mode.IsRegular():
		return KindFile
	}
	return KindSpecial
}

// CaptureTarget freezes the facts of path without mutating it. A missing
// path freezes as KindAbsent; any other stat failure is reported.
func CaptureTarget(path string) (TargetFacts, error) {
	identity, err := pathsafe.FilesystemIdentity(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return TargetFacts{path: path, kind: KindAbsent}, nil
		}
		return TargetFacts{}, err
	}
	facts := TargetFacts{
		path: path, kind: KindOfIdentity(identity),
		identity: identity, mode: identity.Mode().Perm(),
	}
	if facts.kind == KindFile {
		token, err := readFileToken(path)
		if err != nil {
			return TargetFacts{}, err
		}
		facts.token = token
	}
	if facts.kind == KindSymlink {
		payload, err := readLinkPayload(path)
		if err != nil {
			return TargetFacts{}, err
		}
		facts.payload = payload
	}
	return facts, nil
}

func readFileToken(path string) (ContentToken, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return ContentToken{}, fmt.Errorf("filesystem: read target %s: %w", path, err)
	}
	return TokenOfContent(content), nil
}

func readLinkPayload(path string) (string, error) {
	payload, err := os.Readlink(path)
	if err != nil {
		return "", fmt.Errorf("filesystem: read link %s: %w", path, err)
	}
	return payload, nil
}

// Revalidate re-captures the frozen path and reports the first differing
// fact; an absent entry fails when another kind now occupies the path.
func (f TargetFacts) Revalidate() error {
	live, err := CaptureTarget(f.path)
	if err != nil {
		return err
	}
	if live.kind != f.kind {
		return fmt.Errorf("filesystem: target kind changed at %s", f.path)
	}
	if f.kind == KindAbsent {
		return nil
	}
	if !pathsafe.SameIdentity(f.identity, live.identity) {
		return fmt.Errorf("filesystem: target identity changed at %s", f.path)
	}
	if f.kind == KindFile && f.token != live.token {
		return fmt.Errorf("filesystem: target content changed at %s", f.path)
	}
	if f.mode != live.mode {
		return fmt.Errorf("filesystem: target mode changed at %s", f.path)
	}
	if f.kind == KindSymlink && f.payload != live.payload {
		return fmt.Errorf("filesystem: link payload changed at %s", f.path)
	}
	return nil
}
