package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// ResolvePlatform merges the base scan with the platform layer tree.
func ResolvePlatform(root string, baseScan ScanResult, platformLayer deployment.Layer) ([]deployment.ManagedFile, error) {
	if !platformLayer.Valid() {
		return nil, fmt.Errorf("repository: unknown platform layer %q", platformLayer)
	}
	platformResolver := platformResolver{root: root, base: baseScan, platform: platformLayer}
	platformRootView, err := scanLayerTree(root, deployment.NewScope(""), platformLayer)
	if err != nil {
		return nil, err
	}
	records, err := resolveScopeFiles(platformResolver.base, deployment.NewScope(""), platformRootView)
	if err != nil {
		return nil, err
	}
	for _, group := range baseScan.Groups {
		groupRecords, err := platformResolver.resolveScope(deployment.NewScope(group), platformRootView)
		if err != nil {
			return nil, err
		}
		records = append(records, groupRecords...)
	}
	deployment.SortFiles(records)
	return records, nil
}

type platformResolver struct {
	root     string
	base     ScanResult
	platform deployment.Layer
}

func (resolver *platformResolver) resolveScope(scope deployment.Scope, platformRootView layerView) ([]deployment.ManagedFile, error) {
	// A platform file at the group name replaces the entire group subtree.
	if _, replaced := platformRootView.files[scope.Group]; replaced {
		return nil, nil
	}
	platformView, err := scanLayerTree(resolver.root, scope, resolver.platform)
	if err != nil {
		return nil, err
	}
	return resolveScopeFiles(resolver.base, scope, platformView)
}

type layerView struct {
	files map[string]Candidate
	dirs  map[string]struct{}
}

// covers reports whether any base-layer entry suppresses the platform layer
// for target. A file entry at any ancestor path prefix covers the target: a
// single platform FILE at an ancestor suppresses the entire base subtree
// beneath it. That is why covers walks prefixes of the target rather than only
// exact file/dir matches.
func (view layerView) covers(target string) bool {
	if _, ok := view.files[target]; ok {
		return true
	}
	if _, ok := view.dirs[target]; ok {
		return true
	}
	segments := strings.Split(target, "/")
	for length := 1; length < len(segments); length++ {
		prefix := strings.Join(segments[:length], "/")
		if _, ok := view.files[prefix]; ok {
			return true
		}
	}
	return false
}

func resolveScopeFiles(base ScanResult, scope deployment.Scope, platform layerView) ([]deployment.ManagedFile, error) {
	merged := layerView{files: map[string]Candidate{}, dirs: map[string]struct{}{}}
	for _, candidate := range base.Files {
		if candidate.Scope != scope {
			continue
		}
		target, err := baseTarget(candidate)
		if err != nil {
			return nil, err
		}
		if platform.covers(target) {
			continue
		}
		if err := mergeCandidate(merged.files, target, candidate); err != nil {
			return nil, err
		}
	}
	for target, candidate := range platform.files {
		if err := mergeCandidate(merged.files, target, candidate); err != nil {
			return nil, err
		}
	}
	return recordsFor(merged.files)
}

func mergeCandidate(files map[string]Candidate, target string, candidate Candidate) error {
	existing, ok := files[target]
	if ok && existing.Kind != candidate.Kind {
		return fmt.Errorf("repository: ordinary and secret sources collide at %q", target)
	}
	files[target] = candidate
	return nil
}

// representableRootSecretTarget reports whether target can be produced by a
// root-scope secret source under Section 2.1: a root secret must target either
// a dot-prefixed tree (ungrouped HOME tree) or a single non-underscore segment.
// Multi-segment non-dot paths (e.g. bin/...) and leading-underscore targets are
// not representable at the root layer and require a group or a platform overlay.
func representableRootSecretTarget(target string) bool {
	first := strings.Split(target, "/")[0]
	return strings.HasPrefix(first, ".") ||
		(!strings.Contains(target, "/") && !strings.HasPrefix(first, "_"))
}

func baseTarget(candidate Candidate) (string, error) {
	target := candidate.SourceRepoPath
	if !candidate.Scope.IsRoot() {
		target = strings.TrimPrefix(target, candidate.Scope.Group+"/")
	}
	if candidate.Kind != deployment.FileSecret {
		return target, nil
	}
	target = strings.TrimPrefix(target, SecretDirectoryName+"/")
	if candidate.Scope.IsRoot() && !representableRootSecretTarget(target) {
		return "", fmt.Errorf("repository: root secret target %q is not representable at the root layer", target)
	}
	return target, nil
}

func recordsFor(merged map[string]Candidate) ([]deployment.ManagedFile, error) {
	records := make([]deployment.ManagedFile, 0, len(merged))
	for target, candidate := range merged {
		record, err := deployment.NewManagedFile(deployment.ManagedFile{
			Scope: candidate.Scope, Layer: candidate.Layer, Kind: candidate.Kind,
			SourceAbsolutePath: candidate.SourceAbsPath, SourceRepositoryPath: candidate.SourceRepoPath,
			TargetRelativePath: target, SourceExecutableBits: candidate.ExecutableBits,
		})
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func scanLayerTree(root string, scope deployment.Scope, platform deployment.Layer) (layerView, error) {
	relative := filepath.Join(scope.Group, "_"+string(platform))
	info, err := os.Lstat(filepath.Join(root, relative))
	if err != nil {
		if os.IsNotExist(err) {
			return layerView{}, nil
		}
		return layerView{}, err
	}
	if !info.IsDir() {
		return layerView{}, fmt.Errorf("repository: platform layer %q is not a directory", relative)
	}
	walker := layerWalker{
		absolute: filepath.Join(root, relative), relative: relative, scope: scope, layer: platform,
		view: layerView{files: map[string]Candidate{}, dirs: map[string]struct{}{}},
	}
	if err := walker.walk("", deployment.FileOrdinary); err != nil {
		return layerView{}, err
	}
	return walker.view, nil
}

type layerWalker struct {
	absolute string
	relative string
	scope    deployment.Scope
	layer    deployment.Layer
	view     layerView
}

func (walker *layerWalker) walk(relativePath string, fileKind deployment.FileKind) error {
	entries, err := os.ReadDir(filepath.Join(walker.absolute, relativePath))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		entryKind, skip, err := classifyEntry(relativePath, entry, fileKind)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		if err := walker.visit(filepath.Join(relativePath, entry.Name()), entry, entryKind); err != nil {
			return err
		}
	}
	return nil
}

func classifyEntry(relative string, entry os.DirEntry, kind deployment.FileKind) (deployment.FileKind, bool, error) {
	if relative != "" {
		return kind, false, nil
	}
	switch ClassifyPlatformLayer(entry.Name()) {
	case ControlSecrets:
		if !entry.IsDir() {
			return kind, false, fmt.Errorf("repository: control %q is not a directory", entry.Name())
		}
		return deployment.FileSecret, false, nil
	case ControlIgnoredUnderscore:
		return kind, true, nil
	default:
		return kind, false, nil
	}
}

func (walker *layerWalker) visit(path string, entry os.DirEntry, kind deployment.FileKind) error {
	target, err := walker.target(path, kind)
	if err != nil {
		return err
	}
	if entry.IsDir() {
		if target != "" {
			walker.view.dirs[target] = struct{}{}
		}
		return walker.walk(path, kind)
	}
	if !entry.Type().IsRegular() {
		return fmt.Errorf("repository: non-regular source entry %q", filepath.Join(walker.relative, path))
	}
	info, err := entry.Info()
	if err != nil {
		return err
	}
	candidate := Candidate{
		Scope: walker.scope, Layer: walker.layer, Kind: kind,
		SourceRepoPath: filepath.Join(walker.relative, path), SourceAbsPath: filepath.Join(walker.absolute, path),
		ExecutableBits: info.Mode() & deployment.ExecutableBitMask,
	}
	return mergeCandidate(walker.view.files, target, candidate)
}

func (walker *layerWalker) target(path string, kind deployment.FileKind) (string, error) {
	if kind != deployment.FileSecret {
		return path, nil
	}
	target := strings.TrimPrefix(strings.TrimPrefix(path, SecretDirectoryName), "/")
	if walker.scope.IsRoot() && !representableRootSecretTarget(target) {
		return "", fmt.Errorf("repository: root secret target %q is not representable at the root layer", target)
	}
	return target, nil
}
