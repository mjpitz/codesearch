package ignore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultMaxFileSize is the size cap applied when Walk is called with
	// maxFileSize == 0.
	DefaultMaxFileSize int64 = 1 * 1024 * 1024 // 1 MiB

	binaryProbeSize = 512
)

// Entry is a file accepted by Walk: not ignored, not empty, not oversized,
// and not likely-binary.
type Entry struct {
	Path string // repo-relative, forward slashes
	Info fs.FileInfo
}

// WalkFn is invoked once per accepted entry. Returning an error stops the
// walk; returning filepath.SkipDir from inside fn is not supported (use the
// Matcher to exclude directories).
type WalkFn func(Entry) error

// WalkCache enables incremental walks that prune subtrees whose directory
// mtime hasn't changed since the previous run.
//
// Pre-populate Prev with the directory mtimes recorded by the previous Walk
// and set LastSyncAt to that run's completion time. After Walk returns, New
// holds the current snapshot (to persist for the next run) and Pruned lists
// directories whose entire subtree was skipped. When cache is nil, Walk
// behaves identically to its pre-cache semantics.
type WalkCache struct {
	Prev       map[string]int64
	LastSyncAt time.Time

	New    map[string]int64
	Pruned []string
}

// Walk iterates the filesystem under root, applying git-style ignore semantics:
//
//   - .gitignore and .codesearchignore files are honored at every directory
//     level; patterns are interpreted relative to the directory that holds
//     the ignore file.
//   - Deeper matchers override shallower ones when they have a matching rule
//     (positive or positive-then-negated-within-the-same-file).
//   - A pure negation (e.g. a nested file containing only "!app.log") cannot
//     override a shallower positive match — the underlying library does not
//     surface which negation matched. To re-include a file ignored by an
//     ancestor, repeat the positive pattern alongside the negation in the
//     same ignore file (e.g. "*.log\n!app.log").
//   - Within a single directory, .codesearchignore lines are appended after
//     .gitignore lines, so .codesearchignore rules win via normal gitignore
//     "last matching rule wins" semantics inside one matcher.
//   - A directory evaluated as ignored is pruned; files inside it cannot be
//     re-included (git-compatible).
//
// Additionally, Walk skips empty files, files larger than maxFileSize (or
// DefaultMaxFileSize when 0), and files whose first 512 bytes contain a null
// byte (binary heuristic).
//
// rootMatcher should be the result of Load(root).
//
// When cache is non-nil, Walk prunes any subdirectory whose mtime matches
// cache.Prev[rel] and predates cache.LastSyncAt — unless that directory holds
// a .gitignore or .codesearchignore, which can be edited in place without
// bumping the dir mtime and must always be re-scanned. cache.New records the
// mtimes of every visited directory (including pruned ones), and pruned
// descendants' mtimes are carried forward from cache.Prev so the cache
// doesn't decay. The repo root itself is never prunable.
//
// Paths passed to fn are repo-relative with forward slashes.
func Walk(root string, rootMatcher *Matcher, maxFileSize int64, cache *WalkCache, fn WalkFn) error {
	if maxFileSize == 0 {
		maxFileSize = DefaultMaxFileSize
	}

	stack := []*Matcher{}
	if rootMatcher != nil {
		stack = append(stack, rootMatcher)
	}

	if cache != nil && cache.New == nil {
		cache.New = map[string]int64{}
	}

	return walkDir(root, root, stack, maxFileSize, cache, fn)
}

// isIgnored walks the matcher stack shallowest-to-deepest; the deepest matcher
// that returns a non-nil pattern wins. A pattern is only returned when a
// positive rule matched (go-gitignore does not surface pure-negation hits),
// so pure negations at deeper levels cannot un-ignore files matched by
// ancestors — see Walk's doc for the recommended workaround.
func isIgnored(stack []*Matcher, repoRel string, isDir bool) bool {
	probe := repoRel
	if isDir {
		probe += "/"
	}
	ignored := false
	for _, m := range stack {
		sub, ok := m.rel(probe)
		if !ok {
			continue
		}
		matched, pat := m.inner.MatchesPathHow(sub)
		if pat != nil {
			ignored = matched
		}
	}
	return ignored
}

// walkDir recursively visits entries under dir. Any .gitignore /
// .codesearchignore discovered in a subdirectory is pushed onto stack for the
// duration of that subtree and popped before returning. When cache is
// non-nil, subtrees whose dir mtime hasn't changed since the last run are
// pruned (see Walk's doc for rules).
func walkDir(dir, root string, stack []*Matcher, maxFileSize int64, cache *WalkCache, fn WalkFn) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, ent := range ents {
		path := filepath.Join(dir, ent.Name())

		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)

		if ent.IsDir() {
			if isIgnored(stack, rel, true) {
				continue
			}

			m := dirMatcher(root, path)

			if cache != nil {
				info, ierr := ent.Info()
				if ierr != nil {
					return ierr
				}
				mtime := info.ModTime().UnixNano()
				cache.New[rel] = mtime

				// Dirs with ignore files can be edited in place
				// without bumping their mtime, so never prune them.
				if m == nil && cache.Prev != nil {
					prev, ok := cache.Prev[rel]
					if ok && prev == mtime && info.ModTime().Before(cache.LastSyncAt) {
						prefix := rel + "/"
						for k, v := range cache.Prev {
							if strings.HasPrefix(k, prefix) {
								cache.New[k] = v
							}
						}
						cache.Pruned = append(cache.Pruned, rel)
						continue
					}
				}
			}

			if m != nil {
				stack = append(stack, m)
				werr := walkDir(path, root, stack, maxFileSize, cache, fn)
				stack = stack[:len(stack)-1]
				if werr != nil {
					return werr
				}
				continue
			}
			if werr := walkDir(path, root, stack, maxFileSize, cache, fn); werr != nil {
				return werr
			}
			continue
		}

		if isIgnored(stack, rel, false) {
			continue
		}

		info, ierr := ent.Info()
		if ierr != nil {
			return ierr
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if info.Size() == 0 || info.Size() > maxFileSize {
			continue
		}
		if isBinary(path) {
			continue
		}

		if ferr := fn(Entry{Path: rel, Info: info}); ferr != nil {
			return ferr
		}
	}

	return nil
}

func isBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, binaryProbeSize)
	n, _ := f.Read(buf)
	return strings.IndexByte(string(buf[:n]), 0) >= 0
}
