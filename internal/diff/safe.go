// Package diff owns the output-safe diff records of PLAN.md Section 9.6:
// tagged SafeRecord values carrying only precomputed printable unified-diff
// lines, ordinary-file sizes and hashes, or secret classification. No ANSI
// sequence, terminal width, writer, raw secret byte, or go-difflib type
// crosses the package boundary.
package diff

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/reconcile"
	"github.com/pmezard/go-difflib/difflib"
)

// Tag names the safe category of one diff record.
type Tag int

const (
	// TagNone marks equal content on both sides; only metadata such as a
	// mode correction may remain to report.
	TagNone Tag = iota
	// TagText marks a printable unified diff computed from both sides.
	TagText
	// TagBinary marks a binary or oversized ordinary file; the record
	// carries sizes and hashes only.
	TagBinary
	// TagSecret marks a differing secret; the record carries no content,
	// size, hash, or SOPS metadata.
	TagSecret
)

// String returns the stable lowercase name of one safe difference category.
func (tag Tag) String() string {
	switch tag {
	case TagText:
		return "text"
	case TagBinary:
		return "binary"
	case TagSecret:
		return "secret"
	default:
		return "none"
	}
}

// ParseTag maps a stable lowercase name to its safe difference category.
func ParseTag(name string) Tag {
	switch name {
	case "text":
		return TagText
	case "binary":
		return TagBinary
	case "secret":
		return TagSecret
	default:
		return TagNone
	}
}

// Valid reports whether tag is one of the supported constants.
func (t Tag) Valid() bool { return t >= TagNone && t <= TagSecret }

// SafeRecordInput carries the renderable fields of one safe record.
type SafeRecordInput struct {
	TargetPath  string
	Tag         Tag
	SourceLabel string
	TargetLabel string
	Lines       string
	SourceSize  int64
	TargetSize  int64
	SourceHash  deployment.Digest
	TargetHash  deployment.Digest
}

// NewSafeRecord freezes one safe record over the given fields, so the
// inspection and CLI renderers can build records across the boundary.
func NewSafeRecord(input SafeRecordInput) SafeRecord {
	return SafeRecord{
		targetPath:  input.TargetPath,
		tag:         input.Tag,
		sourceLabel: input.SourceLabel,
		targetLabel: input.TargetLabel,
		lines:       input.Lines,
		sourceSize:  input.SourceSize,
		targetSize:  input.TargetSize,
		sourceHash:  input.SourceHash,
		targetHash:  input.TargetHash,
	}
}

// SafeRecord is one immutable, output-safe diff record for a destination.
// Text records carry precomputed printable unified-diff lines, binary records
// carry ordinary-file sizes and hashes, and secret records carry no payload
// at all (PLAN.md Sections 9.6 and 12.4).
type SafeRecord struct {
	targetPath  string
	tag         Tag
	sourceLabel string
	targetLabel string
	lines       string
	sourceSize  int64
	targetSize  int64
	sourceHash  deployment.Digest
	targetHash  deployment.Digest
}

func (r SafeRecord) TargetPath() string  { return r.targetPath }
func (r SafeRecord) Tag() Tag            { return r.tag }
func (r SafeRecord) SourceLabel() string { return r.sourceLabel }
func (r SafeRecord) TargetLabel() string { return r.targetLabel }
func (r SafeRecord) Lines() string       { return r.lines }
func (r SafeRecord) SourceSize() int64   { return r.sourceSize }
func (r SafeRecord) TargetSize() int64   { return r.targetSize }
func (r SafeRecord) SourceHash() deployment.Digest {
	return r.sourceHash
}
func (r SafeRecord) TargetHash() deployment.Digest {
	return r.targetHash
}

// maxTextBytes caps one diff side at 1 MiB; larger content is binary for
// output purposes (PLAN.md Section 9.6).
const maxTextBytes = 1 << 20

const unifiedContextLines = 3

// textEligible reports whether one side may appear in a unified diff: valid
// UTF-8, at most maxTextBytes, and only newline, tab, or printable runes.
// Controls, carriage return, DEL, bidi and zero-width format runes, and every
// other non-printing rune demote the side to binary instead.
func textEligible(data []byte) bool {
	if len(data) > maxTextBytes {
		return false
	}
	if !utf8.Valid(data) {
		return false
	}
	for _, r := range string(data) {
		if r != '\n' && r != '\t' && !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// Build derives the safe record for one file evaluation. The target bytes
// must be the exact bytes captured beside the target snapshot; the record
// never retains them. Equal content yields TagNone, printable text at most
// maxTextBytes per side yields a TagText unified diff with escaped labels,
// and every other content difference yields TagBinary or TagSecret facts
// (PLAN.md Section 9.6).
func Build(evaluation reconcile.Evaluation, targetBytes []byte) (SafeRecord, error) {
	if evaluation.Entry != reconcile.PlanEntryFile {
		return SafeRecord{}, fmt.Errorf("diff: record requires a file plan entry at %q", evaluation.TargetPath)
	}
	if !evaluation.File.Kind.Valid() {
		return SafeRecord{}, fmt.Errorf("diff: record requires a valid source kind at %q", evaluation.TargetPath)
	}
	record := SafeRecord{
		targetPath:  evaluation.TargetPath,
		sourceLabel: "repo/" + escapeLabel(evaluation.File.SourceRepositoryPath),
		targetLabel: "$HOME/" + escapeLabel(evaluation.TargetPath),
	}
	sourceBytes := evaluation.Source.Bytes()
	if bytes.Equal(sourceBytes, targetBytes) {
		return record, nil
	}
	if evaluation.File.Kind == deployment.FileSecret {
		record.tag = TagSecret
		return record, nil
	}
	if textEligible(sourceBytes) && textEligible(targetBytes) {
		return textRecord(record, sourceBytes, targetBytes)
	}
	return binaryRecord(record, evaluation), nil
}

// textRecord renders one printable unified diff whose headers carry the
// already-escaped labels.
func textRecord(record SafeRecord, sourceBytes, targetBytes []byte) (SafeRecord, error) {
	lines, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        textLines(sourceBytes),
		B:        textLines(targetBytes),
		FromFile: record.sourceLabel,
		ToFile:   record.targetLabel,
		Context:  unifiedContextLines,
	})
	if err != nil {
		return SafeRecord{}, fmt.Errorf("diff: render unified diff: %w", err)
	}
	record.tag = TagText
	record.lines = lines
	return record, nil
}

// binaryRecord fills the size and hash facts of an ordinary file that is
// binary or too large for a unified diff.
func binaryRecord(record SafeRecord, evaluation reconcile.Evaluation) SafeRecord {
	record.tag = TagBinary
	record.sourceSize = evaluation.Source.Snapshot().Identity().Size()
	record.targetSize = evaluation.Target.Identity().Size()
	record.sourceHash = evaluation.Source.Snapshot().Semantic()
	record.targetHash = evaluation.Target.Digest()
	return record
}

// textLines splits one printable side into lines that each keep their own
// trailing newline, without inventing a final empty line.
func textLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	lines := strings.SplitAfter(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	last := len(lines) - 1
	if !strings.HasSuffix(lines[last], "\n") {
		lines[last] += "\n"
	}
	return lines
}

// escapeLabel neutralizes every rune that could forge or reorder diff lines:
// newlines and other non-printing runes become "\x.." escapes, so a hostile
// path can never inject a terminal control or a fabricated header line.
func escapeLabel(label string) string {
	var builder strings.Builder
	for _, r := range label {
		if r == '\t' || unicode.IsPrint(r) {
			builder.WriteRune(r)
			continue
		}
		var encoded [utf8.UTFMax]byte
		width := utf8.EncodeRune(encoded[:], r)
		for _, value := range encoded[:width] {
			builder.WriteString(fmt.Sprintf("\\x%02x", value))
		}
	}
	return builder.String()
}
