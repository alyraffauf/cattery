// Package filesystem provides a test-only tree builder for materializing
// files, directories, symlinks, hard links, and injected mutation points
// beneath a temporary root. Production code must not import this package.
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

// Mutation is a hook invoked at its Path after entries materialize. The hook
// receives the materialized absolute path beneath Root.
type Mutation struct {
	Path  string
	Apply func(string) error
}

// Tree is a fluent builder that records entries beneath Root.
type Tree struct {
	Root      string
	entries   []Entry
	mutations []Mutation
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

// Symlink records a symlink at path pointing at target. Dangling targets are
// accepted because os.Symlink stores the target string verbatim.
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

// MutationPoint registers a hook fired at path after entries materialize.
func (builder *Tree) MutationPoint(path string, apply func(string) error) *Tree {
	builder.mutations = append(builder.mutations, Mutation{Path: path, Apply: apply})
	return builder
}

// Entries returns a copy of the recorded entries.
func (builder *Tree) Entries() []Entry {
	return append([]Entry(nil), builder.entries...)
}

// Mutations returns a copy of the recorded mutation hooks.
func (builder *Tree) Mutations() []Mutation {
	return append([]Mutation(nil), builder.mutations...)
}

// Materialize writes every recorded entry beneath Root in dependency order
// (directories, regular files, symlinks, hard links) then fires mutations.
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
	return builder.runMutations()
}

// Cleanup removes the materialized root tree.
func (builder *Tree) Cleanup() error {
	return os.RemoveAll(builder.Root)
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

func (builder *Tree) runMutations() error {
	for _, mutation := range builder.mutations {
		if err := mutation.Apply(builder.resolve(mutation.Path)); err != nil {
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
