package repository

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/pathsafe"
)

// Candidate is one regular-file base-layer source candidate; SourceRepoPath
// is repository-relative and SourceAbsPath is absolute.
type Candidate struct {
	Scope          deployment.Scope
	Layer          deployment.Layer
	Kind           deployment.FileKind
	SourceRepoPath string
	SourceAbsPath  string
	ExecutableBits fs.FileMode
}

// HookCandidate is one raw regular-file child of a scope _hooks tree; hook
// semantics are validated by hook discovery, not by the scanner.
type HookCandidate struct {
	Scope          deployment.Scope
	Phase          deployment.HookPhase
	Name           string
	AbsolutePath   string
	RepositoryPath string
}

// ScanResult is the deterministic output of one repository scan.
type ScanResult struct {
	Groups []string
	Files  []Candidate
	Hooks  []HookCandidate
}

// Scan returns the base-layer candidates, raw hook candidates, and group
// names of the repository at root in deterministic order. Symlinks and
// special entries are rejected.
func Scan(root string) (ScanResult, error) {
	scanner := scopeScanner{repoRoot: root, rootTree: true}
	if err := scanner.scanScopeRoot(); err != nil {
		return ScanResult{}, err
	}
	if err := checkGroupCollisions(scanner.groups); err != nil {
		return ScanResult{}, err
	}
	return ScanResult{Groups: scanner.groups, Files: scanner.files, Hooks: scanner.hooks}, nil
}

type scopeScanner struct {
	repoRoot  string
	scopeRoot string
	scope     deployment.Scope
	rootTree  bool
	groups    []string
	files     []Candidate
	hooks     []HookCandidate
}

func (scanner *scopeScanner) scanScopeRoot() error {
	entries, err := os.ReadDir(filepath.Join(scanner.repoRoot, scanner.scopeRoot))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := scanner.scanEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func (scanner *scopeScanner) scanEntry(entry os.DirEntry) error {
	control := ClassifyRoot(entry.Name())
	switch {
	// Root dot-directories do not promote to groups: pathsafe.GroupName
	// rejects leading-"." names, so they must route to ordinary scanning
	// (e.g. an ungrouped HOME tree rooted at a dot-directory) rather than
	// being treated as a group.
	case scanner.rootTree && control == ControlNone && entry.IsDir() && !strings.HasPrefix(entry.Name(), "."):
		return scanner.beginGroup(entry)
	case control == ControlNone:
		return scanner.scanOrdinary(entry)
	case control == ControlSecrets:
		return scanner.scanSecrets(entry)
	case control == ControlHooks:
		return scanner.scanHooks(entry)
	case control == ControlMetadata:
		if scanner.rootTree {
			return nil
		}
		return scanner.scanOrdinary(entry)
	default:
		return nil
	}
}

func (scanner *scopeScanner) beginGroup(entry os.DirEntry) error {
	name := entry.Name()
	if err := pathsafe.GroupName(name); err != nil {
		return err
	}
	scanner.groups = append(scanner.groups, name)
	previousScopeRoot, previousScope := scanner.scopeRoot, scanner.scope
	scanner.scopeRoot = filepath.Join(scanner.scopeRoot, name)
	scanner.scope = deployment.NewScope(name)
	scanner.rootTree = false
	err := scanner.scanScopeRoot()
	scanner.scopeRoot, scanner.scope, scanner.rootTree = previousScopeRoot, previousScope, true
	return err
}

func (scanner *scopeScanner) scanOrdinary(entry os.DirEntry) error {
	if entry.IsDir() {
		return scanner.walkTree(entry.Name(), deployment.FileOrdinary)
	}
	if !entry.Type().IsRegular() {
		return scanner.newNonRegularSourceError(entry.Name())
	}
	return scanner.addFileAt(entry.Name(), entry, deployment.FileOrdinary)
}

func (scanner *scopeScanner) scanSecrets(entry os.DirEntry) error {
	if !entry.IsDir() {
		return fmt.Errorf("repository: control %q is not a directory", entry.Name())
	}
	return scanner.walkTree(entry.Name(), deployment.FileSecret)
}

func (scanner *scopeScanner) scanHooks(entry os.DirEntry) error {
	if !entry.IsDir() {
		return fmt.Errorf("repository: control %q is not a directory", entry.Name())
	}
	for _, phase := range []deployment.HookPhase{deployment.HookBefore, deployment.HookAfter} {
		if err := scanner.scanHookPhase(entry.Name(), phase); err != nil {
			return err
		}
	}
	return nil
}

func (scanner *scopeScanner) scanHookPhase(hooksDir string, phase deployment.HookPhase) error {
	path := filepath.Join(scanner.repoRoot, scanner.scopeRoot, hooksDir, string(phase))
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		// Missing hook phases are optional; a non-directory phase is ignored like
		// an absent phase because hook discovery only consumes regular children.
		return nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			scanner.hooks = append(scanner.hooks, scanner.hookCandidate(hooksDir, phase, entry.Name()))
		}
	}
	return nil
}

func (scanner *scopeScanner) walkTree(relative string, kind deployment.FileKind) error {
	entries, err := os.ReadDir(filepath.Join(scanner.repoRoot, scanner.scopeRoot, relative))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := scanner.walkEntry(relative, entry, kind); err != nil {
			return err
		}
	}
	return nil
}

func (scanner *scopeScanner) walkEntry(parent string, entry os.DirEntry, kind deployment.FileKind) error {
	path := filepath.Join(parent, entry.Name())
	if entry.IsDir() {
		return scanner.walkTree(path, kind)
	}
	if !entry.Type().IsRegular() {
		return scanner.newNonRegularSourceError(path)
	}
	return scanner.addFileAt(path, entry, kind)
}

func (scanner *scopeScanner) addFileAt(relative string, entry os.DirEntry, kind deployment.FileKind) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}
	path := filepath.Join(scanner.scopeRoot, relative)
	scanner.files = append(scanner.files, Candidate{
		Scope:          scanner.scope,
		Layer:          deployment.LayerBase,
		Kind:           kind,
		SourceRepoPath: path,
		SourceAbsPath:  filepath.Join(scanner.repoRoot, path),
		ExecutableBits: info.Mode() & deployment.ExecutableBitMask,
	})
	return nil
}

func (scanner *scopeScanner) hookCandidate(hooks string, phase deployment.HookPhase, name string) HookCandidate {
	path := filepath.Join(scanner.scopeRoot, hooks, string(phase), name)
	return HookCandidate{
		Scope:          scanner.scope,
		Phase:          phase,
		Name:           name,
		AbsolutePath:   filepath.Join(scanner.repoRoot, path),
		RepositoryPath: path,
	}
}

func (scanner *scopeScanner) newNonRegularSourceError(relative string) error {
	return fmt.Errorf("repository: non-regular source entry %q", filepath.Join(scanner.scopeRoot, relative))
}

func checkGroupCollisions(groups []string) error {
	for first := 0; first < len(groups); first++ {
		if match := duplicateGroupIndex(groups, first); match >= 0 {
			return fmt.Errorf("repository: group %q collides with group %q under portable equivalence", groups[first], groups[match])
		}
	}
	return nil
}

func duplicateGroupIndex(groups []string, first int) int {
	for second := first + 1; second < len(groups); second++ {
		if pathsafe.SegmentsEquivalent(groups[first], groups[second]) {
			return second
		}
	}
	return -1
}
