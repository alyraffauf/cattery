// Package filesystem provides a test-only tree builder for materializing
// files, directories, symlinks, and hard links beneath a temporary root.
// Production code must not import this package.
package filesystem

import (
	"os"
	"path/filepath"
)

// EntryType enumerates the filesystem entries a fixture can materialize.
type EntryType int

const (
	EntryFile EntryType = iota
	EntryDir
	EntrySymlink
	EntryHardLink
)

// Entry records one fixture entry before it is materialized beneath Root.
type Entry struct {
	Path       string
	Type       EntryType
	Content    []byte
	Mode       os.FileMode
	LinkTarget string
}

// Tree is a fluent builder that records entries beneath Root.
type Tree struct {
	Root    string
	entries []Entry
}

// New returns a Tree anchored at root.
func New(root string) *Tree {
	return &Tree{Root: root}
}

// File records a regular file at path with content and mode.
func (builder *Tree) File(path string, content []byte, mode os.FileMode) *Tree {
	builder.entries = append(builder.entries, Entry{
		Path: path, Type: EntryFile, Content: content, Mode: mode,
	})
	return builder
}

// Dir records a directory at path. Mode is advisory; the process umask
// governs the created bits and no mode is forced after creation.
func (builder *Tree) Dir(path string, mode os.FileMode) *Tree {
	builder.entries = append(builder.entries, Entry{
		Path: path, Type: EntryDir, Mode: mode,
	})
	return builder
}

// Symlink records a symlink at path pointing at target.
func (builder *Tree) Symlink(path, target string) *Tree {
	builder.entries = append(builder.entries, Entry{
		Path: path, Type: EntrySymlink, LinkTarget: target,
	})
	return builder
}

// HardLink records a hard link at path sharing the existing entry.
func (builder *Tree) HardLink(path, existing string) *Tree {
	builder.entries = append(builder.entries, Entry{
		Path: path, Type: EntryHardLink, LinkTarget: existing,
	})
	return builder
}

// Materialize writes every recorded entry beneath Root in dependency order
// (directories, regular files, symlinks, hard links).
func (builder *Tree) Materialize() error {
	if err := builder.materializeDirectories(); err != nil {
		return err
	}
	if err := builder.materializeFiles(); err != nil {
		return err
	}
	if err := builder.materializeSymlinks(); err != nil {
		return err
	}
	if err := builder.materializeHardLinks(); err != nil {
		return err
	}
	return nil
}

func (builder *Tree) materializeDirectories() error {
	for _, entry := range builder.entriesByType(EntryDir) {
		if err := os.MkdirAll(builder.resolve(entry.Path), entry.Mode); err != nil {
			return err
		}
	}
	return nil
}

func (builder *Tree) materializeFiles() error {
	for _, entry := range builder.entriesByType(EntryFile) {
		if err := builder.writeFile(entry); err != nil {
			return err
		}
	}
	return nil
}

func (builder *Tree) writeFile(entry Entry) error {
	full := builder.resolve(entry.Path)
	if err := builder.ensureParent(full); err != nil {
		return err
	}
	if err := os.WriteFile(full, entry.Content, entry.Mode); err != nil {
		return err
	}
	return os.Chmod(full, entry.Mode)
}

func (builder *Tree) materializeSymlinks() error {
	for _, entry := range builder.entriesByType(EntrySymlink) {
		full := builder.resolve(entry.Path)
		if err := builder.ensureParent(full); err != nil {
			return err
		}
		if err := os.Symlink(entry.LinkTarget, full); err != nil {
			return err
		}
	}
	return nil
}

func (builder *Tree) materializeHardLinks() error {
	for _, entry := range builder.entriesByType(EntryHardLink) {
		source := builder.resolve(entry.LinkTarget)
		full := builder.resolve(entry.Path)
		if err := builder.ensureParent(full); err != nil {
			return err
		}
		if err := os.Link(source, full); err != nil {
			return err
		}
	}
	return nil
}

func (builder *Tree) entriesByType(want EntryType) []Entry {
	var matched []Entry
	for _, entry := range builder.entries {
		if entry.Type == want {
			matched = append(matched, entry)
		}
	}
	return matched
}

func (builder *Tree) ensureParent(full string) error {
	return os.MkdirAll(filepath.Dir(full), 0o777)
}

func (builder *Tree) resolve(path string) string {
	return filepath.Join(builder.Root, path)
}
