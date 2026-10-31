// Package sync reconciles the filesystem (walker + digest) with the
// persisted Bleve index, using mtime/size fast-path and digest equality
// to skip unchanged docs.
package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/digest"
	"github.com/mjpitz/codesearch/internal/doc"
	"github.com/mjpitz/codesearch/internal/ignore"
	"github.com/mjpitz/codesearch/internal/index"
)

// Meta is the JSON sidecar written next to the Bleve index.
type Meta struct {
	SchemaVersion int              `json:"schema_version"`
	LastSyncAt    time.Time        `json:"last_sync_at"`
	MaxFileSize   int64            `json:"max_file_size,omitempty"`
	IgnoreHash    string           `json:"ignore_hash,omitempty"`
	Directories   map[string]int64 `json:"directories,omitempty"`
}

// RunOptions tunes a single Run invocation.
type RunOptions struct {
	// Full forces a full walk, ignoring the directory-mtime cache.
	Full bool
}

// Result reports what a single Run did.
type Result struct {
	Scanned   int // files visited by the walker
	Upserted  int // new or content-changed docs written
	Touched   int // unchanged bodies but mtime/size metadata refreshed
	Deleted   int // index entries removed because the file is gone
	Unchanged int // fast-path skips (mtime + size matched stored)
	Pruned    int // subtrees skipped because dir mtime matched the cache
}

type upsertInfo struct {
	d       *doc.Doc
	isTouch bool
}

type updatePlan struct {
	toDelete []string
	toUpsert []upsertInfo
}

// Run reconciles the filesystem with the index in cfg.IndexPath(). The
// index must already exist (call init first). Returns a Result describing
// the work performed.
func Run(ctx context.Context, cfg *config.IndexConfig, opts RunOptions) (Result, error) {
	err := ctx.Err()
	if err != nil {
		return Result{}, err
	}

	matcher, err := ignore.Load(cfg.Root)
	if err != nil {
		return Result{}, fmt.Errorf("load ignore: %w", err)
	}

	src, err := digest.Load(ctx, cfg.Root)
	if err != nil {
		return Result{}, fmt.Errorf("load digest source: %w", err)
	}

	idx, err := index.Open(cfg.IndexPath())
	if err != nil {
		return Result{}, fmt.Errorf("open index: %w", err)
	}
	defer func() { _ = idx.Close() }()

	prev, _ := ReadMeta(cfg.MetaPath())
	ignoreHash, err := hashRootIgnoreFiles(cfg.Root)
	if err != nil {
		return Result{}, fmt.Errorf("hash ignore files: %w", err)
	}

	cache := &ignore.WalkCache{New: map[string]int64{}}
	cacheValid := !opts.Full &&
		prev.SchemaVersion == index.SchemaVersion &&
		prev.MaxFileSize == cfg.MaxFileSize &&
		prev.IgnoreHash == ignoreHash &&
		prev.Directories != nil
	if cacheValid {
		cache.Prev = prev.Directories
		cache.LastSyncAt = prev.LastSyncAt
	}

	wanted := map[string]ignore.Entry{}
	collect := func(e ignore.Entry) error {
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return ctxErr
		}
		wanted[e.Path] = e
		return nil
	}

	err = ignore.Walk(cfg.Root, matcher, cfg.MaxFileSize, cache, collect)
	if err != nil {
		return Result{}, fmt.Errorf("walk: %w", err)
	}

	prunedPrefixes := make([]string, 0, len(cache.Pruned))
	for _, p := range cache.Pruned {
		prunedPrefixes = append(prunedPrefixes, p+"/")
	}
	sort.Strings(prunedPrefixes)

	var res Result
	seen := make(map[string]bool, len(wanted))
	plan := updatePlan{}

	reconcile := func(s index.Stored) error {
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return ctxErr
		}
		if isUnderPrunedPrefix(s.Path, prunedPrefixes) {
			seen[s.Path] = true
			res.Unchanged++
			res.Pruned++
			return nil
		}
		entry, ok := wanted[s.Path]
		if !ok {
			plan.toDelete = append(plan.toDelete, s.Path)
			res.Deleted++
			return nil
		}
		seen[s.Path] = true

		mtime := float64(entry.Info.ModTime().UnixNano())
		size := float64(entry.Info.Size())

		if s.MTime == mtime && s.Size == size {
			res.Unchanged++
			return nil
		}

		// mtime/size differ — re-hash and decide whether the body actually
		// changed or only the metadata.
		body, readErr := os.ReadFile(filepath.Join(cfg.Root, entry.Path))
		if readErr != nil {
			return fmt.Errorf("read %s: %w", entry.Path, readErr)
		}
		hash := src.For(entry.Path, body).String()

		d, parseErr := doc.FromBytes(entry.Path, body)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", entry.Path, parseErr)
		}
		d.ModTime = entry.Info.ModTime()
		d.Size = entry.Info.Size()
		d.Digest = hash

		if hash == s.Digest {
			res.Touched++
			plan.toUpsert = append(plan.toUpsert, upsertInfo{d: d, isTouch: true})
		} else {
			res.Upserted++
			plan.toUpsert = append(plan.toUpsert, upsertInfo{d: d, isTouch: false})
		}
		return nil
	}

	err = idx.Iterate(reconcile)
	if err != nil {
		return res, err
	}

	res.Scanned = len(wanted)

	for path, entry := range wanted {
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return res, ctxErr
		}
		if seen[path] {
			continue
		}

		body, readErr := os.ReadFile(filepath.Join(cfg.Root, entry.Path))
		if readErr != nil {
			return res, fmt.Errorf("read %s: %w", entry.Path, readErr)
		}

		d, parseErr := doc.FromBytes(entry.Path, body)
		if parseErr != nil {
			return res, fmt.Errorf("parse %s: %w", entry.Path, parseErr)
		}
		d.ModTime = entry.Info.ModTime()
		d.Size = entry.Info.Size()
		d.Digest = src.For(entry.Path, body).String()

		plan.toUpsert = append(plan.toUpsert, upsertInfo{d: d, isTouch: false})
		res.Upserted++
	}

	// Execute plan
	if len(plan.toDelete) > 0 {
		const batchSize = 1000
		for i := 0; i < len(plan.toDelete); i += batchSize {
			end := i + batchSize
			if end > len(plan.toDelete) {
				end = len(plan.toDelete)
			}
			batch := idx.NewBatch()
			for _, path := range plan.toDelete[i:end] {
				batch.Delete(path)
			}
			if err := idx.Batch(batch); err != nil {
				return res, fmt.Errorf("batch delete: %w", err)
			}
		}
	}

	if len(plan.toUpsert) > 0 {
		const batchSize = 1000
		for i := 0; i < len(plan.toUpsert); i += batchSize {
			end := i + batchSize
			if end > len(plan.toUpsert) {
				end = len(plan.toUpsert)
			}
			batch := idx.NewBatch()
			for _, info := range plan.toUpsert[i:end] {
				batch.Index(info.d.Path, info.d.Flatten())
			}
			if err := idx.Batch(batch); err != nil {
				return res, fmt.Errorf("batch upsert: %w", err)
			}
		}
	}

	meta := Meta{
		SchemaVersion: index.SchemaVersion,
		LastSyncAt:    time.Now().UTC(),
		MaxFileSize:   cfg.MaxFileSize,
		IgnoreHash:    ignoreHash,
		Directories:   cache.New,
	}

	err = WriteMeta(cfg.MetaPath(), meta)
	if err != nil {
		return res, fmt.Errorf("write meta: %w", err)
	}

	return res, nil
}

// hashRootIgnoreFiles returns a sha256 digest of the root .gitignore and
// .codesearchignore contents (concatenated with a separator). Missing files
// contribute nothing. The digest is stored in Meta to invalidate the
// directory-prune cache when either file is edited — in-place edits to these
// files don't bump their containing directory's mtime, so a dedicated
// fingerprint is required to detect them.
func hashRootIgnoreFiles(root string) (string, error) {
	h := sha256.New()
	for _, name := range []string{".gitignore", ignore.IgnoreFileName} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return "", err
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isUnderPrunedPrefix reports whether path lives beneath any of the sorted
// prefixes (each prefix ends with "/" so src/foo doesn't shadow src/foobar).
func isUnderPrunedPrefix(path string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return false
	}
	i := sort.SearchStrings(prefixes, path)
	if i > 0 && strings.HasPrefix(path, prefixes[i-1]) {
		return true
	}
	if i < len(prefixes) && strings.HasPrefix(path, prefixes[i]) {
		return true
	}
	return false
}

// ReadMeta returns the persisted Meta or an empty Meta if the file does
// not exist.
func ReadMeta(path string) (Meta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Meta{}, nil
		}
		return Meta{}, err
	}

	var m Meta
	err = json.Unmarshal(data, &m)
	if err != nil {
		return Meta{}, err
	}

	return m, nil
}

// WriteMeta serializes m to path, creating the parent directory if needed.
func WriteMeta(path string, m Meta) error {
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}
