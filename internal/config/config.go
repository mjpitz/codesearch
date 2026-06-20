// Package config defines indexer configuration: repo root discovery, index
// storage paths, default field boosts, and ignore-file location.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Index storage layout, relative to the repo root.
const (
	IndexDirName = ".codesearch"
	IndexSubdir  = "index"
	MetaFileName = "meta.json"
)

// DefaultMaxFileSize caps the per-file size accepted by the walker.
const DefaultMaxFileSize int64 = 1 * 1024 * 1024 // 1 MiB

// DefaultBoosts are the query-time field boosts applied when an indexed
// field is present. SEO-base names (description, keywords) are listed here
// because they're the conventions used in this repo, but they're only
// applied when present so the indexer remains generic.
var DefaultBoosts = map[string]float64{
	"title":       5.0,
	"tags":        2.0,
	"body":        1.0,
	"description": 3.0,
	"keywords":    4.0,
}

// FindRoot walks up from start looking for a `.git` entry (directory for a
// regular clone, file for a worktree). Returns the absolute path of the
// containing directory.
func FindRoot(start string) (string, error) {
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		start = cwd
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}

	cur := abs
	for {
		_, statErr := os.Stat(filepath.Join(cur, ".git"))
		if statErr == nil {
			return cur, nil
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("no .git directory found at or above %s", abs)
		}
		cur = parent
	}
}

// IndexConfig captures the runtime knobs shared across the codesearch
// subcommands.
type IndexConfig struct {
	Root        string
	MaxFileSize int64
	Boosts      map[string]float64
}

// Load discovers the repo root from start (or cwd when empty) and returns
// the config with defaults applied.
func Load(start string) (*IndexConfig, error) {
	root, err := FindRoot(start)
	if err != nil {
		return nil, err
	}
	return &IndexConfig{
		Root:        root,
		MaxFileSize: DefaultMaxFileSize,
		Boosts:      DefaultBoosts,
	}, nil
}

// IndexDir is the directory where the Bleve index and metadata live.
func (c *IndexConfig) IndexDir() string { return filepath.Join(c.Root, IndexDirName) }

// IndexPath is the Bleve index location inside IndexDir.
func (c *IndexConfig) IndexPath() string { return filepath.Join(c.IndexDir(), IndexSubdir) }

// MetaPath is the JSON sidecar location inside IndexDir.
func (c *IndexConfig) MetaPath() string { return filepath.Join(c.IndexDir(), MetaFileName) }
