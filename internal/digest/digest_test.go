package digest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/mjpitz/codesearch/internal/digest"
)

// initRepo creates a fresh git repo at root with a single committed file
// `tracked.md` and returns the repo handle. The HEAD commit's tree contains
// just that one file.
func initRepo(t *testing.T) (root string, repo *git.Repository, committedHash plumbing.Hash) {
	t.Helper()
	root = t.TempDir()

	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	err = os.WriteFile(filepath.Join(root, "tracked.md"), []byte("# Tracked\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	_, err = wt.Add("tracked.md")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	commit, err := wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@t.test", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	c, err := repo.CommitObject(commit)
	if err != nil {
		t.Fatal(err)
	}

	tree, err := c.Tree()
	if err != nil {
		t.Fatal(err)
	}

	entry, err := tree.FindEntry("tracked.md")
	if err != nil {
		t.Fatal(err)
	}
	committedHash = entry.Hash
	return
}

func TestLoad_NonGitDir(t *testing.T) {
	dir := t.TempDir()

	src, err := digest.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(src.TreeBlobs) != 0 || len(src.IndexBlobs) != 0 {
		t.Errorf("expected empty caches in non-git dir")
	}
	if src.Tracked("anything") {
		t.Error("Tracked should be false in non-git dir")
	}
}

func TestSource_For_UsesCommittedHash(t *testing.T) {
	root, _, want := initRepo(t)

	src, err := digest.Load(context.Background(), root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := src.For("tracked.md", nil)
	if got != want {
		t.Errorf("For(tracked.md) = %s, want %s (HEAD blob)", got, want)
	}
	if !src.Tracked("tracked.md") {
		t.Error("Tracked(tracked.md) should be true")
	}
}

func TestSource_For_StagedOverridesHEAD(t *testing.T) {
	root, repo, headHash := initRepo(t)

	// Modify the tracked file and stage it without committing.
	err := os.WriteFile(filepath.Join(root, "tracked.md"), []byte("# Tracked\n\nNew line\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	wt, _ := repo.Worktree()
	_, err = wt.Add("tracked.md")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	src, err := digest.Load(context.Background(), root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := src.For("tracked.md", nil)
	if got == headHash {
		t.Errorf("For = HEAD hash %s; expected staged hash to win", headHash)
	}
}

func TestSource_For_UntrackedHashesWorkingTree(t *testing.T) {
	root, _, _ := initRepo(t)

	src, err := digest.Load(context.Background(), root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	body := []byte("untracked body\n")
	got := src.For("untracked.md", body)
	want := plumbing.ComputeHash(plumbing.BlobObject, body)
	if got != want {
		t.Errorf("For(untracked.md) = %s, want %s (working-tree blob)", got, want)
	}
	if src.Tracked("untracked.md") {
		t.Error("Tracked(untracked.md) should be false")
	}
}
