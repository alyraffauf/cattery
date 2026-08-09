// Package routes decodes the optional _routes.toml alias declarations that a
// repository root or group may place beside its source tree. The decoder is
// strict: only the documented version, the three documented sections, and
// HOME-relative paths are accepted. Every rejection reports the verbatim
// offending input; pathsafe owns the lexical rules.
package routes

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/pelletier/go-toml/v2"
)

// routeVersion is the single _routes.toml schema version this package accepts.
const routeVersion = 1

// Section names the stratum a route declaration belongs to. The active plan
// unions SectionAll with the host platform section at apply time; this package
// performs no platform activation.
type Section string

const (
	SectionAll    Section = "all"
	SectionDarwin Section = "darwin"
	SectionLinux  Section = "linux"
)

// Declaration is one canonical HOME-relative target plus its alias
// destinations, recorded under a single section of _routes.toml.
type Declaration struct {
	Canonical string
	Aliases   []string
	Section   Section
}

// Config is the decoded view of one _routes.toml file: the schema version and
// every declaration, sorted by canonical target for stable, deterministic
// output.
type Config struct {
	Version      int
	Declarations []Declaration
}

// Decode parses and validates _routes.toml bytes. It rejects unknown fields,
// unsupported or missing versions, unrecognized sections, malformed paths, and
// a repeated alias destination within one section. A canonical target may
// appear in more than one section: the active plan unions `all` with the host
// platform section, so cross-section repetition is resolved at activation time.
func Decode(data []byte) (Config, error) {
	raw, err := decodeRaw(data)
	if err != nil {
		return Config{}, err
	}
	if raw.Version != routeVersion {
		return Config{}, versionError(raw.Version)
	}
	return buildConfig(raw)
}

func versionError(version int) error {
	return fmt.Errorf("routes: unsupported version %d", version)
}

type rawConfig struct {
	Version  int         `toml:"version"`
	Symlinks rawSymlinks `toml:"symlinks"`
}

type rawSymlinks struct {
	All    map[string][]string `toml:"all"`
	Darwin map[string][]string `toml:"darwin"`
	Linux  map[string][]string `toml:"linux"`
}

func decodeRaw(data []byte) (rawConfig, error) {
	var raw rawConfig
	reader := bytes.NewReader(data)
	err := toml.NewDecoder(reader).DisallowUnknownFields().Decode(&raw)
	return raw, err
}

func buildConfig(raw rawConfig) (Config, error) {
	declarations, err := collectDeclarations(raw.Symlinks)
	if err != nil {
		return Config{}, err
	}
	sortDeclarations(declarations)
	return Config{Version: raw.Version, Declarations: declarations}, nil
}

func sortDeclarations(declarations []Declaration) {
	sort.Slice(declarations, byCanonical(declarations))
}

func byCanonical(declarations []Declaration) func(int, int) bool {
	return func(first, second int) bool {
		return declarations[first].Canonical < declarations[second].Canonical
	}
}

func collectDeclarations(syms rawSymlinks) ([]Declaration, error) {
	sections := []routeSection{
		{name: SectionAll, rows: syms.All},
		{name: SectionDarwin, rows: syms.Darwin},
		{name: SectionLinux, rows: syms.Linux},
	}
	var declarations []Declaration
	for _, section := range sections {
		added, err := sectionDeclarations(section)
		if err != nil {
			return nil, err
		}
		declarations = append(declarations, added...)
	}
	return declarations, nil
}

type routeSection struct {
	name Section
	rows map[string][]string
}

func sectionDeclarations(section routeSection) ([]Declaration, error) {
	destinations := map[string]bool{}
	canonicals := sortedKeys(section.rows)
	var declarations []Declaration
	for _, canonical := range canonicals {
		if err := validateCanonical(canonical); err != nil {
			return nil, err
		}
		aliases := section.rows[canonical]
		if err := validateAliases(aliases, destinations); err != nil {
			return nil, err
		}
		declarations = append(declarations, newDeclaration(canonical, aliases, section.name))
	}
	return declarations, nil
}

func newDeclaration(canonical string, aliases []string, name Section) Declaration {
	return Declaration{Canonical: canonical, Aliases: aliases, Section: name}
}

func validateCanonical(canonical string) error {
	if _, err := pathsafe.Segments(canonical); err != nil {
		return fmt.Errorf("routes: %w", err)
	}
	return nil
}

func validateAliases(aliases []string, destinations map[string]bool) error {
	for _, alias := range aliases {
		if _, err := pathsafe.Segments(alias); err != nil {
			return fmt.Errorf("routes: %w", err)
		}
		if destinations[alias] {
			return fmt.Errorf("routes: duplicate alias destination %q", alias)
		}
		destinations[alias] = true
	}
	return nil
}

func sortedKeys(rows map[string][]string) []string {
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
