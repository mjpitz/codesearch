// Package ignore loads .codesearchignore (gitignore syntax) and walks the
// repo skipping ignored paths, oversized files, and binary content.
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

// IgnoreFileName is the per-repo ignore file read by Load.
const IgnoreFileName = ".codesearchignore"

// defaultIgnoreLines are always applied so the index never includes itself
// or the .git directory, regardless of what the user puts in their ignore
// file.
var defaultIgnoreLines = []string{
	".git/",
	".codesearch/",
}

// Matcher reports whether a repo-relative path is ignored.
type Matcher struct {
	inner *gi.GitIgnore
}

// Load reads root/.codesearchignore (if present) and combines it with the
// built-in defaults. A missing ignore file is not an error.
func Load(root string) (*Matcher, error) {
	lines := append([]string(nil), defaultIgnoreLines...)

	f, err := os.Open(filepath.Join(root, IgnoreFileName))
	switch {
	case err == nil:
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			lines = append(lines, line)
		}
		err = scanner.Err()
		if err != nil {
			return nil, err
		}
	case errors.Is(err, fs.ErrNotExist):
		// fall through with defaults only
	default:
		return nil, err
	}

	return &Matcher{inner: gi.CompileIgnoreLines(lines...)}, nil
}

// Match returns true when relPath (forward-slash, repo-relative) is ignored.
func (m *Matcher) Match(relPath string) bool {
	return m.inner.MatchesPath(relPath)
}
