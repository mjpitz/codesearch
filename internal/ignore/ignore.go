// Package ignore loads .gitignore / .codesearchignore (gitignore syntax) and
// walks the repo skipping ignored paths, oversized files, and binary content.
package ignore

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	gi "github.com/sabhiram/go-gitignore"
)

// IgnoreFileName is the per-repo ignore file read in addition to .gitignore.
const IgnoreFileName = ".codesearchignore"

// defaultIgnoreLines are always applied so the index never includes itself
// or the .git directory, regardless of what the user puts in their ignore
// file.
var defaultIgnoreLines = []string{
	".git/",
	".codesearch/",
}

// Matcher wraps a compiled gitignore ruleset scoped to a directory. relDir is
// repo-relative with forward slashes; "" means the repo root.
type Matcher struct {
	inner  *gi.GitIgnore
	relDir string
}

// Load reads root-level .gitignore and .codesearchignore (if present) and
// combines them with the built-in defaults. Missing files are not errors.
func Load(root string) (*Matcher, error) {
	lines := append([]string(nil), defaultIgnoreLines...)

	for _, name := range []string{".gitignore", IgnoreFileName} {
		fileLines, err := readLines(filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		lines = append(lines, fileLines...)
	}

	return &Matcher{inner: gi.CompileIgnoreLines(lines...), relDir: ""}, nil
}

// dirMatcher loads .gitignore and .codesearchignore from the given directory
// and returns a matcher scoped to it (relDir is dir relative to root).
// Returns nil when neither file contributes any rules.
func dirMatcher(root, dir string) *Matcher {
	var lines []string
	for _, name := range []string{".gitignore", IgnoreFileName} {
		fileLines, _ := readLines(filepath.Join(dir, name))
		lines = append(lines, fileLines...)
	}
	if len(lines) == 0 {
		return nil
	}

	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		rel = ""
	}

	return &Matcher{inner: gi.CompileIgnoreLines(lines...), relDir: rel}
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines, scanner.Err()
}

// rel returns repoRel converted to a path relative to the matcher's directory,
// along with true if repoRel is within that directory. For the root matcher
// (relDir == "") every path is in scope.
func (m *Matcher) rel(repoRel string) (string, bool) {
	if m.relDir == "" {
		return repoRel, true
	}
	prefix := m.relDir + "/"
	if !strings.HasPrefix(repoRel, prefix) {
		return "", false
	}
	return strings.TrimPrefix(repoRel, prefix), true
}

// Match reports whether a repo-relative path is ignored by this matcher's
// rules. Intended for the root matcher returned by Load.
func (m *Matcher) Match(repoRel string) bool {
	sub, ok := m.rel(repoRel)
	if !ok {
		return false
	}
	return m.inner.MatchesPath(sub)
}
