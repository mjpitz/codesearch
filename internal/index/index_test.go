package index_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mjpitz/codesearch/internal/doc"
	"github.com/mjpitz/codesearch/internal/index"
)

func newTempIndex(t *testing.T) *index.Index {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "idx")
	idx, err := index.Create(dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

func TestCreate_ReopensWithOpen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "idx")

	first, err := index.Create(dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = first.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := index.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = second.Close() }()

	n, _ := second.DocCount()
	if n != 0 {
		t.Errorf("DocCount = %d, want 0", n)
	}
}

func TestUpsertDeleteRoundTrip(t *testing.T) {
	idx := newTempIndex(t)

	d := &doc.Doc{
		Path:    "docs/test.md",
		Title:   "Hello World",
		Tags:    []string{"alpha", "beta"},
		Body:    "the quick brown fox",
		Digest:  "abc123",
		ModTime: time.Unix(1000, 0),
		Size:    42,
	}

	err := idx.Upsert(d)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	count, err := idx.DocCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("DocCount = %d, want 1", count)
	}

	var seen []index.Stored
	collector := func(s index.Stored) error {
		seen = append(seen, s)
		return nil
	}

	err = idx.Iterate(collector)
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}

	if len(seen) != 1 || seen[0].Path != "docs/test.md" {
		t.Fatalf("Iterate result = %+v, want one entry for docs/test.md", seen)
	}
	if seen[0].Digest != "abc123" {
		t.Errorf("Digest = %q, want abc123", seen[0].Digest)
	}

	err = idx.Delete("docs/test.md")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	n, _ := idx.DocCount()
	if n != 0 {
		t.Errorf("DocCount after Delete = %d, want 0", n)
	}
}
