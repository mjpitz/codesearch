// Package digest computes git-blob hashes for indexed files. It reads
// HEAD-tree and staging-index blob SHAs via go-git (no shelling out) and
// falls back to hashing working-tree bytes for dirty or untracked files.
package digest

import (
	"context"
	"errors"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Source caches blob hashes for paths that go-git already knows about
// (tracked-and-clean files), avoiding rehashes during sync.
type Source struct {
	TreeBlobs  map[string]plumbing.Hash
	IndexBlobs map[string]plumbing.Hash
}

// Load opens the git repository rooted at root and prefetches blob hashes
// for the HEAD tree and the staging index. A non-git directory returns an
// empty Source (every file falls through to working-tree hashing).
func Load(ctx context.Context, root string) (*Source, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	repo, err := git.PlainOpenWithOptions(root, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return &Source{
				TreeBlobs:  map[string]plumbing.Hash{},
				IndexBlobs: map[string]plumbing.Hash{},
			}, nil
		}
		return nil, err
	}

	s := &Source{
		TreeBlobs:  map[string]plumbing.Hash{},
		IndexBlobs: map[string]plumbing.Hash{},
	}

	err = loadTreeBlobs(repo, s.TreeBlobs)
	if err != nil {
		return nil, err
	}

	err = loadIndexBlobs(repo, s.IndexBlobs)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func loadTreeBlobs(repo *git.Repository, into map[string]plumbing.Hash) error {
	head, err := repo.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil // unborn branch — no HEAD yet
		}
		return err
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return err
	}
	tree, err := commit.Tree()
	if err != nil {
		return err
	}
	return tree.Files().ForEach(func(f *object.File) error {
		into[f.Name] = f.Hash
		return nil
	})
}

func loadIndexBlobs(repo *git.Repository, into map[string]plumbing.Hash) error {
	idx, err := repo.Storer.Index()
	if err != nil {
		return err
	}
	for _, e := range idx.Entries {
		into[e.Name] = e.Hash
	}
	return nil
}

// For returns the git-blob hash to record for path. It prefers the staged
// blob (covers "staged but committed body is different"), then the HEAD
// tree, and finally hashes the working-tree body in-process. body is only
// read when the cache misses, so callers can pass nil for clean files.
func (s *Source) For(path string, body []byte) plumbing.Hash {
	if s != nil {
		if h, ok := s.IndexBlobs[path]; ok {
			return h
		}
		if h, ok := s.TreeBlobs[path]; ok {
			return h
		}
	}
	return plumbing.ComputeHash(plumbing.BlobObject, body)
}

// Tracked reports whether path is present in either the HEAD tree or the
// staging index. Useful when sync needs to decide whether to trust the
// cached hash without re-reading the file body.
func (s *Source) Tracked(path string) bool {
	if s == nil {
		return false
	}
	if _, ok := s.IndexBlobs[path]; ok {
		return true
	}
	_, ok := s.TreeBlobs[path]
	return ok
}
