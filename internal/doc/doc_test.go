package doc_test

import (
	"reflect"
	"testing"

	"github.com/mjpitz/codesearch/internal/doc"
)

func TestFromBytes_FrontmatterDoc(t *testing.T) {
	in := []byte("---\ntitle: ClickHouse\ntags: [Service-Catalog, Telemetry]\nservice:\n  name: clickhouse\n  category: telemetry\n---\n# ClickHouse\n\nColumnar OLAP store.\n")
	d, err := doc.FromBytes("services/clickhouse/README.md", in)
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if d.Title != "ClickHouse" {
		t.Errorf("Title = %q, want ClickHouse", d.Title)
	}
	wantTags := []string{"service-catalog", "telemetry"}
	if !reflect.DeepEqual(d.Tags, wantTags) {
		t.Errorf("Tags = %v, want %v", d.Tags, wantTags)
	}
	if d.Frontmatter == nil {
		t.Fatal("expected Frontmatter populated")
	}
}

func TestFromBytes_NoFrontmatter_TitleFromH1(t *testing.T) {
	in := []byte("# Top Heading\n\nSome body.")
	d, err := doc.FromBytes("README.md", in)
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if d.Title != "Top Heading" {
		t.Errorf("Title = %q, want Top Heading", d.Title)
	}
	if d.Frontmatter != nil {
		t.Errorf("Frontmatter = %v, want nil", d.Frontmatter)
	}
}

func TestFromBytes_NoTitle_FallbackToFilename(t *testing.T) {
	in := []byte("plain text body, no headings")
	d, err := doc.FromBytes("notes/scratch.md", in)
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if d.Title != "scratch" {
		t.Errorf("Title = %q, want scratch", d.Title)
	}
}

func TestFromBytes_TagsNormalization(t *testing.T) {
	// Verifies normalization (lowercase, trim) happens via the public API.
	in := []byte("---\ntags: [\"  Research  \", DECENTRALIZED, \"\"]\n---\nbody\n")
	d, err := doc.FromBytes("x.md", in)
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	want := []string{"research", "decentralized"}
	if !reflect.DeepEqual(d.Tags, want) {
		t.Errorf("Tags = %v, want %v", d.Tags, want)
	}
}

func TestFlatten_ReservedFieldsWin(t *testing.T) {
	d := &doc.Doc{
		Path:  "x.md",
		Title: "Real Title",
		Tags:  []string{"a"},
		Body:  "body",
		Frontmatter: map[string]any{
			"title":       "Should Be Ignored",
			"tags":        []any{"ignored"},
			"description": "desc",
			"keywords":    []any{"kw1", "kw2"},
			"service": map[string]any{
				"name":     "clickhouse",
				"category": "telemetry",
			},
		},
	}
	flat := d.Flatten()
	if flat["title"] != "Real Title" {
		t.Errorf("title = %v, want Real Title", flat["title"])
	}
	if !reflect.DeepEqual(flat["tags"], []string{"a"}) {
		t.Errorf("tags = %v, want [a]", flat["tags"])
	}
	if flat["description"] != "desc" {
		t.Errorf("description = %v", flat["description"])
	}
	if flat["service.name"] != "clickhouse" {
		t.Errorf("service.name = %v", flat["service.name"])
	}
	if flat["service.category"] != "telemetry" {
		t.Errorf("service.category = %v", flat["service.category"])
	}
}
