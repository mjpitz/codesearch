package ignore

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// Walk iterates the filesystem under root, skipping:
//   - directories matched by the Matcher,
//   - files matched by the Matcher,
//   - empty files,
//   - files larger than maxFileSize (or DefaultMaxFileSize when 0),
//   - files whose first 512 bytes contain a null byte (binary heuristic).
//
// Paths passed to fn are repo-relative with forward slashes regardless of
// the host OS.
func Walk(root string, matcher *Matcher, maxFileSize int64, fn WalkFn) error {
	if maxFileSize == 0 {
		maxFileSize = DefaultMaxFileSize
	}

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}

		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)

		if matcher != nil && matcher.Match(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}

		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() == 0 || info.Size() > maxFileSize {
			return nil
		}
		if isBinary(path) {
			return nil
		}

		return fn(Entry{Path: rel, Info: info})
	})
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
