package query_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mjpitz/codesearch/internal/doc"
	"github.com/mjpitz/codesearch/internal/index"
	"github.com/mjpitz/codesearch/internal/query"
)

func newIdx(t *testing.T, docs ...*doc.Doc) *index.Index {
	t.Helper()

	idx, err := index.Create(filepath.Join(t.TempDir(), "idx"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	for _, d := range docs {
		if d.ModTime.IsZero() {
			d.ModTime = time.Unix(0, 0)
		}
		err = idx.Upsert(d)
		if err != nil {
			t.Fatal(err)
		}
	}
	return idx
}

func TestSearch_TitleBoostWinsOverBody(t *testing.T) {
	idx := newIdx(t,
		&doc.Doc{Path: "a.md", Title: "Telemetry overview", Body: "ordinary words here"},
		&doc.Doc{Path: "b.md", Title: "Random", Body: "telemetry is mentioned in the body somewhere"},
	)

	res, err := query.Search(idx, query.Request{Terms: "telemetry"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) < 2 {
		t.Fatalf("got %d hits, want >= 2", len(res.Hits))
	}
	if res.Hits[0].Path != "a.md" {
		t.Errorf("top hit = %q, want a.md (title boost should outrank body)", res.Hits[0].Path)
	}
}

func TestSearch_FieldFilter(t *testing.T) {
	idx := newIdx(t,
		&doc.Doc{Path: "r1.md", Title: "Research one", Tags: []string{"research"}, Body: "decentralized"},
		&doc.Doc{Path: "p1.md", Title: "Presentation one", Tags: []string{"presentation"}, Body: "decentralized"},
	)

	req := query.Request{
		Terms:  "decentralized",
		Fields: map[string]string{"tags": "research"},
	}

	res, err := query.Search(idx, req)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(res.Hits))
	}
	if res.Hits[0].Path != "r1.md" {
		t.Errorf("hit = %q, want r1.md", res.Hits[0].Path)
	}
}

func TestSearch_EmptyTermsReturnsAll(t *testing.T) {
	idx := newIdx(t,
		&doc.Doc{Path: "a.md", Title: "A"},
		&doc.Doc{Path: "b.md", Title: "B"},
	)

	res, err := query.Search(idx, query.Request{Terms: "", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Errorf("Total = %d, want 2", res.Total)
	}
}

func TestSearch_HighlightProducesSnippet(t *testing.T) {
	idx := newIdx(t,
		&doc.Doc{Path: "x.md", Title: "X", Body: "the quick brown fox jumps over the lazy dog"},
	)

	res, err := query.Search(idx, query.Request{Terms: "fox", Highlight: "html"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) == 0 || res.Hits[0].Snippet == "" {
		t.Fatal("expected a hit with a non-empty snippet")
	}
}
