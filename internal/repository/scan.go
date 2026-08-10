package repository

import (
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

// ScanResult is the deterministic output of one repository scan.
type ScanResult struct {
	Groups []string
	Files  []Candidate
}

// Scan returns the base-layer candidates and group names of the repository at
// root in deterministic order. Symlinks and
// special entries are rejected.
func Scan(root string) (ScanResult, error) {
	ignores, err := loadIgnoreMatcher(root)
	if err != nil {
		return ScanResult{}, err
	}
	scanner := scopeScanner{repoRoot: root, rootTree: true, ignores: ignores}
	if err := scanner.scanScopeRoot(); err != nil {
		return ScanResult{}, err
	}
	if err := checkGroupCollisions(scanner.groups); err != nil {
		return ScanResult{}, err
	}
	return ScanResult{Groups: scanner.groups, Files: scanner.files}, nil
}

type scopeScanner struct {
	repoRoot  string
	scopeRoot string
	scope     deployment.Scope
	rootTree  bool
	ignores   ignoreMatcher
	groups    []string
	files     []Candidate
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
	if scanner.ignores.ignores(filepath.Join(scanner.scopeRoot, entry.Name()), entry.IsDir()) {
		return nil
	}
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
	case control == ControlMetadata:
		if scanner.rootTree {
			// Repository metadata is ignored only at the repository root.
			return nil
		}
		return scanner.scanOrdinary(entry)
	default:
		return nil
	}
}

func (scanner *scopeScanner) beginGroup(entry os.DirEntry) error {
	// Save and restore scope state so recursive scanning returns to the parent.
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
	if scanner.ignores.ignores(filepath.Join(scanner.scopeRoot, path), entry.IsDir()) {
		return nil
	}
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
