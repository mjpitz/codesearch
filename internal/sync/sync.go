// Package sync reconciles the filesystem (walker + digest) with the
// persisted Bleve index, using mtime/size fast-path and digest equality
// to skip unchanged docs.
package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/digest"
	"github.com/mjpitz/codesearch/internal/doc"
	"github.com/mjpitz/codesearch/internal/ignore"
	"github.com/mjpitz/codesearch/internal/index"
)

// Meta is the JSON sidecar written next to the Bleve index.
type Meta struct {
	SchemaVersion int       `json:"schema_version"`
	LastSyncAt    time.Time `json:"last_sync_at"`
}

// Result reports what a single Run did.
type Result struct {
	Scanned   int // files visited by the walker
	Upserted  int // new or content-changed docs written
	Touched   int // unchanged bodies but mtime/size metadata refreshed
	Deleted   int // index entries removed because the file is gone
	Unchanged int // fast-path skips (mtime + size matched stored)
}

// Run reconciles the filesystem with the index in cfg.IndexPath(). The
// index must already exist (call init first). Returns a Result describing
// the work performed.
func Run(ctx context.Context, cfg *config.IndexConfig) (Result, error) {
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

	wanted := map[string]ignore.Entry{}
	collect := func(e ignore.Entry) error {
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return ctxErr
		}
		wanted[e.Path] = e
		return nil
	}

	err = ignore.Walk(cfg.Root, matcher, cfg.MaxFileSize, collect)
	if err != nil {
		return Result{}, fmt.Errorf("walk: %w", err)
	}

	var res Result
	seen := make(map[string]bool, len(wanted))

	reconcile := func(s index.Stored) error {
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return ctxErr
		}
		entry, ok := wanted[s.Path]
		if !ok {
			delErr := idx.Delete(s.Path)
			if delErr != nil {
				return fmt.Errorf("delete %s: %w", s.Path, delErr)
			}
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

		upsertErr := idx.Upsert(d)
		if upsertErr != nil {
			return upsertErr
		}

		if hash == s.Digest {
			res.Touched++
		} else {
			res.Upserted++
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

		err = idx.Upsert(d)
		if err != nil {
			return res, err
		}
		res.Upserted++
	}

	meta := Meta{
		SchemaVersion: index.SchemaVersion,
		LastSyncAt:    time.Now().UTC(),
	}

	err = WriteMeta(cfg.MetaPath(), meta)
	if err != nil {
		return res, fmt.Errorf("write meta: %w", err)
	}

	return res, nil
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
