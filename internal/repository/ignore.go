package repository

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const ignoreFileName = ".catteryignore"

type ignoreMatcher struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	expression      *regexp.Regexp
	directoriesOnly bool
}

func loadIgnoreMatcher(root string) (ignoreMatcher, error) {
	content, err := os.ReadFile(filepath.Join(root, ignoreFileName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ignoreMatcher{}, nil
		}
		return ignoreMatcher{}, err
	}
	return parseIgnoreMatcher(string(content))
}

func parseIgnoreMatcher(content string) (ignoreMatcher, error) {
	matcher := ignoreMatcher{}
	for _, line := range strings.Split(content, "\n") {
		pattern := strings.TrimSpace(line)
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		directoriesOnly := strings.HasSuffix(pattern, "/")
		pattern = strings.TrimSuffix(strings.TrimPrefix(pattern, "/"), "/")
		if pattern == "" {
			continue
		}
		expression, err := regexp.Compile(ignorePatternExpression(pattern))
		if err != nil {
			return ignoreMatcher{}, err
		}
		matcher.patterns = append(matcher.patterns, ignorePattern{expression: expression, directoriesOnly: directoriesOnly})
	}
	return matcher, nil
}

func (matcher ignoreMatcher) ignores(path string, isDirectory bool) bool {
	path = strings.Trim(filepath.ToSlash(path), "/")
	if path == "" {
		return false
	}
	for _, pattern := range matcher.patterns {
		if pattern.directoriesOnly {
			if matchesDirectory(pattern.expression, path, isDirectory) {
				return true
			}
			continue
		}
		if pattern.expression.MatchString(path) {
			return true
		}
	}
	return false
}

func matchesDirectory(expression *regexp.Regexp, path string, isDirectory bool) bool {
	if isDirectory && expression.MatchString(path) {
		return true
	}
	for {
		parent := filepath.ToSlash(filepath.Dir(path))
		if parent == "." {
			return false
		}
		if expression.MatchString(parent) {
			return true
		}
		path = parent
	}
}

func ignorePatternExpression(pattern string) string {
	prefix := "^"
	if !strings.Contains(pattern, "/") {
		prefix = "^(?:.*/)?"
	}
	var expression strings.Builder
	expression.WriteString(prefix)
	runes := []rune(pattern)
	for index := 0; index < len(runes); index++ {
		character := runes[index]
		switch character {
		case '*':
			if index+1 < len(runes) && runes[index+1] == '*' {
				index++
				if index+1 < len(runes) && runes[index+1] == '/' {
					index++
					expression.WriteString("(?:.*/)?")
					continue
				}
				expression.WriteString(".*")
				continue
			}
			expression.WriteString("[^/]*")
		case '?':
			expression.WriteString("[^/]")
		default:
			expression.WriteString(regexp.QuoteMeta(string(character)))
		}
	}
	expression.WriteString("$")
	return expression.String()
}
