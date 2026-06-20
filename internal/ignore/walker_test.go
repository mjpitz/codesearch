package ignore_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

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

	err := ignore.Walk(root, m, maxSize, collector)
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

	err = ignore.Walk(root, m, 0, check)
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
