package sync_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/index"
	"github.com/mjpitz/codesearch/internal/sync"
)

func newRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	_, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}
	return root
}

func mustWrite(t *testing.T, root, rel, body string) {
	t.Helper()

	p := filepath.Join(root, filepath.FromSlash(rel))
	err := os.MkdirAll(filepath.Dir(p), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(p, []byte(body), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

func newCfg(t *testing.T, root string) *config.IndexConfig {
	t.Helper()

	cfg := &config.IndexConfig{
		Root:        root,
		MaxFileSize: 1 * 1024 * 1024,
		Boosts:      config.DefaultBoosts,
	}

	idx, err := index.Create(cfg.IndexPath())
	if err != nil {
		t.Fatal(err)
	}
	_ = idx.Close()
	return cfg
}

func TestRun_IndexesNewFiles(t *testing.T) {
	root := newRepo(t)
	cfg := newCfg(t, root)
	mustWrite(t, root, "docs/a.md", "---\ntitle: A\ntags: [x]\n---\n# A\nbody\n")
	mustWrite(t, root, "docs/b.md", "# B\nplain")

	res, err := sync.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Upserted != 2 {
		t.Errorf("Upserted = %d, want 2", res.Upserted)
	}
	if res.Scanned != 2 {
		t.Errorf("Scanned = %d, want 2", res.Scanned)
	}
}

func TestRun_FastPathOnSecondCall(t *testing.T) {
	root := newRepo(t)
	cfg := newCfg(t, root)
	mustWrite(t, root, "docs/a.md", "body a")
	mustWrite(t, root, "docs/b.md", "body b")

	_, err := sync.Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	res, err := sync.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res.Unchanged != 2 {
		t.Errorf("Unchanged = %d, want 2", res.Unchanged)
	}
	if res.Upserted != 0 || res.Touched != 0 {
		t.Errorf("expected no writes on second run; got upserted=%d touched=%d", res.Upserted, res.Touched)
	}
}

func TestRun_DeletesMissingFiles(t *testing.T) {
	root := newRepo(t)
	cfg := newCfg(t, root)
	mustWrite(t, root, "keep.md", "keep")
	mustWrite(t, root, "drop.md", "drop")

	_, err := sync.Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	err = os.Remove(filepath.Join(root, "drop.md"))
	if err != nil {
		t.Fatal(err)
	}

	res, err := sync.Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1", res.Deleted)
	}
}

func TestRun_TouchOnlyRefreshesMeta(t *testing.T) {
	root := newRepo(t)
	cfg := newCfg(t, root)
	mustWrite(t, root, "x.md", "stable body")

	_, err := sync.Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Bump mtime without changing content.
	future := time.Now().Add(2 * time.Second)
	err = os.Chtimes(filepath.Join(root, "x.md"), future, future)
	if err != nil {
		t.Fatal(err)
	}

	res, err := sync.Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Touched != 1 {
		t.Errorf("Touched = %d, want 1", res.Touched)
	}
	if res.Upserted != 0 {
		t.Errorf("Upserted = %d, want 0", res.Upserted)
	}
}

func TestRun_DetectsBodyChange(t *testing.T) {
	root := newRepo(t)
	cfg := newCfg(t, root)
	mustWrite(t, root, "x.md", "first body")

	_, err := sync.Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Sleep so mtime changes detectably on filesystems with second
	// granularity.
	time.Sleep(1100 * time.Millisecond)
	mustWrite(t, root, "x.md", "second body completely different")

	res, err := sync.Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Upserted != 1 {
		t.Errorf("Upserted = %d, want 1", res.Upserted)
	}
}
