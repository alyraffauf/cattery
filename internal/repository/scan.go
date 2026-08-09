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

func (s *scopeScanner) scanScopeRoot() error {
	entries, err := os.ReadDir(filepath.Join(s.repoRoot, s.scopeRoot))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := s.scanEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func (s *scopeScanner) scanEntry(entry os.DirEntry) error {
	control := ClassifyRoot(entry.Name())
	switch {
	case s.rootTree && control == ControlNone && entry.IsDir() && !strings.HasPrefix(entry.Name(), "."):
		return s.beginGroup(entry)
	case control == ControlNone:
		return s.scanOrdinary(entry)
	case control == ControlSecrets:
		return s.scanSecrets(entry)
	case control == ControlHooks:
		return s.scanHooks(entry)
	case control == ControlMetadata:
		if s.rootTree {
			return nil
		}
		return s.scanOrdinary(entry)
	default:
		return nil
	}
}

func (s *scopeScanner) beginGroup(entry os.DirEntry) error {
	name := entry.Name()
	if err := pathsafe.GroupName(name); err != nil {
		return err
	}
	s.groups = append(s.groups, name)
	previousScopeRoot, previousScope := s.scopeRoot, s.scope
	s.scopeRoot = filepath.Join(s.scopeRoot, name)
	s.scope = deployment.NewScope(name)
	s.rootTree = false
	err := s.scanScopeRoot()
	s.scopeRoot, s.scope, s.rootTree = previousScopeRoot, previousScope, true
	return err
}

func (s *scopeScanner) scanOrdinary(entry os.DirEntry) error {
	if entry.IsDir() {
		return s.walkTree(entry.Name(), deployment.FileOrdinary)
	}
	if !entry.Type().IsRegular() {
		return s.nonRegular(entry.Name())
	}
	return s.addFileAt(entry.Name(), entry, deployment.FileOrdinary)
}

func (s *scopeScanner) scanSecrets(entry os.DirEntry) error {
	if !entry.IsDir() {
		return fmt.Errorf("repository: control %q is not a directory", entry.Name())
	}
	return s.walkTree(entry.Name(), deployment.FileSecret)
}

func (s *scopeScanner) scanHooks(entry os.DirEntry) error {
	if !entry.IsDir() {
		return fmt.Errorf("repository: control %q is not a directory", entry.Name())
	}
	for _, phase := range []deployment.HookPhase{deployment.HookBefore, deployment.HookAfter} {
		if err := s.scanHookPhase(entry.Name(), phase); err != nil {
			return err
		}
	}
	return nil
}

func (s *scopeScanner) scanHookPhase(hooks string, phase deployment.HookPhase) error {
	path := filepath.Join(s.repoRoot, s.scopeRoot, hooks, string(phase))
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			s.hooks = append(s.hooks, s.hookCandidate(hooks, phase, entry.Name()))
		}
	}
	return nil
}

func (s *scopeScanner) walkTree(relative string, kind deployment.FileKind) error {
	entries, err := os.ReadDir(filepath.Join(s.repoRoot, s.scopeRoot, relative))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := s.walkEntry(relative, entry, kind); err != nil {
			return err
		}
	}
	return nil
}

func (s *scopeScanner) walkEntry(parent string, entry os.DirEntry, kind deployment.FileKind) error {
	path := filepath.Join(parent, entry.Name())
	if entry.IsDir() {
		return s.walkTree(path, kind)
	}
	if !entry.Type().IsRegular() {
		return s.nonRegular(path)
	}
	return s.addFileAt(path, entry, kind)
}

func (s *scopeScanner) addFileAt(relative string, entry os.DirEntry, kind deployment.FileKind) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}
	path := filepath.Join(s.scopeRoot, relative)
	s.files = append(s.files, Candidate{
		Scope:          s.scope,
		Layer:          deployment.LayerBase,
		Kind:           kind,
		SourceRepoPath: path,
		SourceAbsPath:  filepath.Join(s.repoRoot, path),
		ExecutableBits: info.Mode() & 0o111,
	})
	return nil
}

func (s *scopeScanner) hookCandidate(hooks string, phase deployment.HookPhase, name string) HookCandidate {
	path := filepath.Join(s.scopeRoot, hooks, string(phase), name)
	return HookCandidate{
		Scope:          s.scope,
		Phase:          phase,
		Name:           name,
		AbsolutePath:   filepath.Join(s.repoRoot, path),
		RepositoryPath: path,
	}
}

func (s *scopeScanner) nonRegular(relative string) error {
	return fmt.Errorf("repository: non-regular source entry %q", filepath.Join(s.scopeRoot, relative))
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
