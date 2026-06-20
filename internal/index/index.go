package index

import (
	"errors"
	"fmt"
	"time"

	"github.com/blevesearch/bleve/v2"
	"go.etcd.io/bbolt"
	bbolterrors "go.etcd.io/bbolt/errors"

	"github.com/mjpitz/codesearch/internal/doc"
)

// openLockTimeout caps how long Open / OpenReadOnly will wait for the
// underlying bbolt lock. Bleve uses bbolt.DefaultOptions, which has
// Timeout = 0 (block forever). We override briefly during open so a stuck
// reader or writer surfaces as an actionable error instead of a hang.
const openLockTimeout = 5 * time.Second

// Index wraps bleve.Index with the Upsert/Delete/Iterate surface that the
// sync, query, and MCP layers consume.
type Index struct {
	inner bleve.Index
	path  string
}

// Create builds a new index at path using BuildMapping.
func Create(path string) (*Index, error) {
	inner, err := bleve.New(path, BuildMapping())
	if err != nil {
		return nil, err
	}
	return &Index{inner: inner, path: path}, nil
}

// Open opens an existing index at path for read/write access. If the
// underlying bbolt lock cannot be acquired within openLockTimeout, returns
// a wrapped ErrIndexLocked.
func Open(path string) (*Index, error) {
	return openWithTimeout(path, false)
}

// OpenReadOnly opens an existing index at path with read-only semantics
// (shared lock). Multiple read-only handles can coexist; a writer is still
// blocked while any read-only handle is open.
func OpenReadOnly(path string) (*Index, error) {
	return openWithTimeout(path, true)
}

// ErrIndexLocked is returned when the bbolt file lock cannot be acquired
// within openLockTimeout. Most commonly this means another codesearch
// process (typically the `serve` MCP server, or a parallel `sync`) is
// holding an incompatible lock.
var ErrIndexLocked = errors.New("index is locked by another process")

func openWithTimeout(path string, readOnly bool) (*Index, error) {
	// Bleve's scorch reads bbolt.DefaultOptions when opening its metadata
	// store, so we tweak the global Timeout briefly and restore it. This
	// is safe for our single-process CLI because opens are serial.
	prev := bbolt.DefaultOptions.Timeout
	bbolt.DefaultOptions.Timeout = openLockTimeout
	defer func() { bbolt.DefaultOptions.Timeout = prev }()

	var (
		inner bleve.Index
		err   error
	)
	if readOnly {
		inner, err = bleve.OpenUsing(path, map[string]any{"read_only": true})
	} else {
		inner, err = bleve.Open(path)
	}
	if err != nil {
		if errors.Is(err, bbolterrors.ErrTimeout) {
			return nil, fmt.Errorf("%w (%s): %v", ErrIndexLocked, path, err)
		}
		return nil, err
	}
	return &Index{inner: inner, path: path}, nil
}

// Bleve returns the underlying bleve.Index for query construction.
func (i *Index) Bleve() bleve.Index { return i.inner }

// Path is the on-disk location of the index directory.
func (i *Index) Path() string { return i.path }

// Close releases the underlying resources.
func (i *Index) Close() error { return i.inner.Close() }

// Upsert indexes (or replaces) a document under d.Path.
func (i *Index) Upsert(d *doc.Doc) error {
	return i.inner.Index(d.Path, d.Flatten())
}

// Delete removes a document by ID (repo-relative path).
func (i *Index) Delete(id string) error { return i.inner.Delete(id) }

// DocCount returns the number of indexed documents.
func (i *Index) DocCount() (uint64, error) { return i.inner.DocCount() }

// Stored captures the bookkeeping fields the sync layer needs to compare
// against on-disk files without re-parsing the body.
type Stored struct {
	Path   string
	Digest string
	MTime  float64 // unix nanoseconds
	Size   float64
}

// Iterate calls fn once per indexed document, paginating internally. The
// fn must return nil to continue; any error aborts iteration.
func (i *Index) Iterate(fn func(Stored) error) error {
	const pageSize = 1000

	from := 0
	for {
		req := bleve.NewSearchRequestOptions(bleve.NewMatchAllQuery(), pageSize, from, false)
		req.Fields = []string{"path", "digest", "mtime", "size"}

		result, err := i.inner.Search(req)
		if err != nil {
			return err
		}
		for _, hit := range result.Hits {
			s := Stored{Path: hit.ID}
			if v, ok := hit.Fields["digest"].(string); ok {
				s.Digest = v
			}
			if v, ok := hit.Fields["mtime"].(float64); ok {
				s.MTime = v
			}
			if v, ok := hit.Fields["size"].(float64); ok {
				s.Size = v
			}
			err = fn(s)
			if err != nil {
				return err
			}
		}
		if len(result.Hits) < pageSize {
			return nil
		}
		from += pageSize
	}
}
