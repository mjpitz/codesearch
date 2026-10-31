package ignore_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mjpitz/codesearch/internal/ignore"
)

func writeFile(t *testing.T, root, rel string, body []byte) {
	t.Helper()

	p := filepath.Join(root, filepath.FromSlash(rel))
	err := os.MkdirAll(filepath.Dir(p), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(p, body, 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

func collect(t *testing.T, root string, m *ignore.Matcher, maxSize int64) []string {
	t.Helper()

	var out []string
	collector := func(e ignore.Entry) error {
		out = append(out, e.Path)
		return nil
	}

	err := ignore.Walk(root, m, maxSize, nil, collector)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	sort.Strings(out)
	return out
}

func TestWalk_PrunesIgnoredDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ignore.IgnoreFileName, []byte("node_modules/\n"))
	writeFile(t, root, "docs/keep.md", []byte("hello"))
	writeFile(t, root, "node_modules/skip.js", []byte("var x = 1"))
	writeFile(t, root, ".git/HEAD", []byte("ref: refs/heads/main"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	got := collect(t, root, m, 0)
	want := []string{ignore.IgnoreFileName, "docs/keep.md"}
	if !equal(got, want) {
		t.Errorf("Walk paths = %v, want %v", got, want)
	}
}

func TestWalk_SkipsBinaryAndOversize(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "good.md", []byte("# Title"))
	writeFile(t, root, "empty.md", []byte{})
	writeFile(t, root, "bin.dat", []byte{0x68, 0x00, 0x69}) // null byte in probe
	writeFile(t, root, "big.txt", bytes.Repeat([]byte("a"), 200))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	got := collect(t, root, m, 50) // cap below big.txt's 200 bytes
	want := []string{"good.md"}
	if !equal(got, want) {
		t.Errorf("Walk paths = %v, want %v", got, want)
	}
}

func TestWalk_UsesForwardSlashes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/b/c.md", []byte("body"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	check := func(e ignore.Entry) error {
		if strings.Contains(e.Path, "\\") {
			t.Errorf("path %q contains backslash; expected forward slashes only", e.Path)
		}
		return nil
	}

	err = ignore.Walk(root, m, 0, nil, check)
	if err != nil {
		t.Fatal(err)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWalk_RootGitignorePrunes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", []byte("secrets/\n*.log\n"))
	writeFile(t, root, "a.log", []byte("log"))
	writeFile(t, root, "secrets/key.txt", []byte("x"))
	writeFile(t, root, "keep.md", []byte("keep"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	got := collect(t, root, m, 0)
	want := []string{".gitignore", "keep.md"}
	if !equal(got, want) {
		t.Errorf("Walk paths = %v, want %v", got, want)
	}
}

func TestWalk_RootCodesearchignoreNegatesGitignore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", []byte("*.log\n"))
	writeFile(t, root, ".codesearchignore", []byte("*.log\n!keep.log\n"))
	writeFile(t, root, "drop.log", []byte("drop"))
	writeFile(t, root, "keep.log", []byte("keep"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	got := collect(t, root, m, 0)
	want := []string{".codesearchignore", ".gitignore", "keep.log"}
	if !equal(got, want) {
		t.Errorf("Walk paths = %v, want %v", got, want)
	}
}

func TestWalk_NestedGitignoreScopedToSubtree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "build/.gitignore", []byte("out.js\n"))
	writeFile(t, root, "build/out.js", []byte("drop"))
	writeFile(t, root, "build/keep.txt", []byte("keep"))
	writeFile(t, root, "src/out.js", []byte("not pruned — nested rule only scopes to build/"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	got := collect(t, root, m, 0)
	want := []string{"build/.gitignore", "build/keep.txt", "src/out.js"}
	if !equal(got, want) {
		t.Errorf("Walk paths = %v, want %v", got, want)
	}
}

func TestWalk_DeeperFileOverridesRootGitignore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", []byte("*.log\n"))
	// Repeat the positive pattern alongside the negation; go-gitignore does not
	// surface pure-negation matches, so the deeper file must contain both.
	writeFile(t, root, "logs/.codesearchignore", []byte("*.log\n!app.log\n"))
	writeFile(t, root, "logs/app.log", []byte("keep"))
	writeFile(t, root, "logs/debug.log", []byte("drop"))
	writeFile(t, root, "other.log", []byte("drop"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	got := collect(t, root, m, 0)
	want := []string{".gitignore", "logs/.codesearchignore", "logs/app.log"}
	if !equal(got, want) {
		t.Errorf("Walk paths = %v, want %v", got, want)
	}
}

func TestWalk_PrunedDirectoryBlocksNestedReinclude(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", []byte("data/\n"))
	writeFile(t, root, "data/.codesearchignore", []byte("*\n!keep.csv\n"))
	writeFile(t, root, "data/keep.csv", []byte("keep"))
	writeFile(t, root, "data/skip.csv", []byte("skip"))
	writeFile(t, root, "src/app.go", []byte("package app"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	got := collect(t, root, m, 0)
	want := []string{".gitignore", "src/app.go"}
	if !equal(got, want) {
		t.Errorf("Walk paths = %v, want %v", got, want)
	}
}

func TestWalk_NestedAnchoredPatternScoped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "logs/.gitignore", []byte("/debug.log\n"))
	writeFile(t, root, "logs/debug.log", []byte("drop — anchored at logs/"))
	writeFile(t, root, "logs/sub/debug.log", []byte("keep — anchor only hits logs/debug.log"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	got := collect(t, root, m, 0)
	want := []string{"logs/.gitignore", "logs/sub/debug.log"}
	if !equal(got, want) {
		t.Errorf("Walk paths = %v, want %v", got, want)
	}
}

// walkWithCache runs Walk capturing both the file list and the cache state.
func walkWithCache(t *testing.T, root string, m *ignore.Matcher, cache *ignore.WalkCache) []string {
	t.Helper()

	var out []string
	err := ignore.Walk(root, m, 0, cache, func(e ignore.Entry) error {
		out = append(out, e.Path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sort.Strings(out)
	return out
}

func TestWalk_PrunesUnchangedSubtree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/a.go", []byte("package a"))
	writeFile(t, root, "src/sub/b.go", []byte("package sub"))
	writeFile(t, root, "docs/c.md", []byte("# c"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// First walk: no Prev, nothing is pruned, mtimes are captured.
	cache1 := &ignore.WalkCache{}
	_ = walkWithCache(t, root, m, cache1)
	if len(cache1.Pruned) != 0 {
		t.Fatalf("first walk Pruned = %v, want empty", cache1.Pruned)
	}
	if _, ok := cache1.New["src"]; !ok {
		t.Fatalf("cache.New missing src: %v", cache1.New)
	}

	// Second walk after a short pause so LastSyncAt strictly exceeds every
	// recorded mtime (racy-mtime guard requires strict Before).
	time.Sleep(20 * time.Millisecond)
	cache2 := &ignore.WalkCache{Prev: cache1.New, LastSyncAt: time.Now()}
	got := walkWithCache(t, root, m, cache2)

	// Files under src/ (which has no ignore file) should be pruned; docs
	// has no ignore file either, so it's pruned too. The only entries
	// walked are the ones directly under root (none here).
	if !containsString(cache2.Pruned, "src") || !containsString(cache2.Pruned, "docs") {
		t.Errorf("Pruned = %v, want src and docs", cache2.Pruned)
	}
	// Walked entries must not include anything under a pruned subtree.
	for _, p := range got {
		if strings.HasPrefix(p, "src/") || strings.HasPrefix(p, "docs/") {
			t.Errorf("walked pruned path %q", p)
		}
	}
}

func TestWalk_DoesNotPruneWhenDirMtimeChanges(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/a.go", []byte("package a"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	cache1 := &ignore.WalkCache{}
	_ = walkWithCache(t, root, m, cache1)

	// Bump src/ mtime by creating a new file inside it (POSIX bumps parent
	// dir mtime on child creat).
	writeFile(t, root, "src/b.go", []byte("package a"))

	time.Sleep(20 * time.Millisecond)
	cache2 := &ignore.WalkCache{Prev: cache1.New, LastSyncAt: time.Now()}
	got := walkWithCache(t, root, m, cache2)

	if containsString(cache2.Pruned, "src") {
		t.Errorf("src was pruned despite mtime change: %v", cache2.Pruned)
	}
	if !containsString(got, "src/b.go") {
		t.Errorf("new file src/b.go not walked: %v", got)
	}
}

func TestWalk_DoesNotPruneDirWithIgnoreFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/.gitignore", []byte("*.tmp\n"))
	writeFile(t, root, "src/a.go", []byte("package a"))
	writeFile(t, root, "docs/c.md", []byte("# c"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	cache1 := &ignore.WalkCache{}
	_ = walkWithCache(t, root, m, cache1)

	time.Sleep(20 * time.Millisecond)
	cache2 := &ignore.WalkCache{Prev: cache1.New, LastSyncAt: time.Now()}
	got := walkWithCache(t, root, m, cache2)

	if containsString(cache2.Pruned, "src") {
		t.Errorf("src pruned despite containing .gitignore: %v", cache2.Pruned)
	}
	if !containsString(cache2.Pruned, "docs") {
		t.Errorf("docs should be pruned (no ignore file, unchanged): %v", cache2.Pruned)
	}
	if !containsString(got, "src/.gitignore") || !containsString(got, "src/a.go") {
		t.Errorf("src was pruned by mistake, missing entries: %v", got)
	}
}

func TestWalk_RacyMtimeGuard(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/a.go", []byte("package a"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	cache1 := &ignore.WalkCache{}
	_ = walkWithCache(t, root, m, cache1)

	// Set LastSyncAt equal to src's mtime — strict Before must refuse to
	// prune on second-granularity filesystems where the dir could change
	// within the same second as the previous sync.
	srcMtime := time.Unix(0, cache1.New["src"])
	cache2 := &ignore.WalkCache{Prev: cache1.New, LastSyncAt: srcMtime}
	_ = walkWithCache(t, root, m, cache2)

	if containsString(cache2.Pruned, "src") {
		t.Errorf("racy-mtime guard failed: src pruned when mtime == LastSyncAt")
	}
}

func TestWalk_PreservesDescendantMtimesOnPrune(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/sub/a.go", []byte("package sub"))

	m, err := ignore.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	cache1 := &ignore.WalkCache{}
	_ = walkWithCache(t, root, m, cache1)

	subMtime, ok := cache1.New["src/sub"]
	if !ok {
		t.Fatalf("cache1.New missing src/sub: %v", cache1.New)
	}

	time.Sleep(20 * time.Millisecond)
	cache2 := &ignore.WalkCache{Prev: cache1.New, LastSyncAt: time.Now()}
	_ = walkWithCache(t, root, m, cache2)

	if !containsString(cache2.Pruned, "src") {
		t.Fatalf("expected src to be pruned, got %v", cache2.Pruned)
	}
	// src/sub was never visited this run but must survive in New so the
	// NEXT run can still prune it.
	if cache2.New["src/sub"] != subMtime {
		t.Errorf("src/sub mtime not carried forward: got %d want %d", cache2.New["src/sub"], subMtime)
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
