package doc_test

import (
	"reflect"
	"testing"

	"github.com/mjpitz/codesearch/internal/doc"
)

func TestSplit_NoFrontmatter(t *testing.T) {
	in := []byte("# Title\n\nBody text\n")
	fm, body, err := doc.Split(in)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if fm != nil {
		t.Errorf("expected nil frontmatter, got %v", fm)
	}
	if string(body) != string(in) {
		t.Errorf("expected body to equal input")
	}
}

func TestSplit_StandardForm(t *testing.T) {
	in := []byte("---\ntitle: Hello\ntags: [a, b]\n---\n# Heading\n\nBody\n")
	fm, body, err := doc.Split(in)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if fm["title"] != "Hello" {
		t.Errorf("title = %v, want Hello", fm["title"])
	}
	gotTags := fm["tags"]
	wantTags := []any{"a", "b"}
	if !reflect.DeepEqual(gotTags, wantTags) {
		t.Errorf("tags = %v, want %v", gotTags, wantTags)
	}
	if string(body) != "# Heading\n\nBody\n" {
		t.Errorf("body = %q", body)
	}
}

func TestSplit_NoTrailingNewline(t *testing.T) {
	in := []byte("---\ntitle: Hello\n---")
	fm, body, err := doc.Split(in)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if fm["title"] != "Hello" {
		t.Errorf("title = %v, want Hello", fm["title"])
	}
	if len(body) != 0 {
		t.Errorf("body = %q, want empty", body)
	}
}

func TestSplit_MissingClose(t *testing.T) {
	in := []byte("---\ntitle: Hello\n# Heading\n")
	fm, body, err := doc.Split(in)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if fm != nil {
		t.Errorf("expected nil frontmatter when close missing, got %v", fm)
	}
	if string(body) != string(in) {
		t.Errorf("expected body to equal input when close missing")
	}
}

func TestSplit_InvalidYAML(t *testing.T) {
	in := []byte("---\ntitle: : :\n---\nbody\n")
	if _, _, err := doc.Split(in); err == nil {
		t.Error("expected YAML parse error")
	}
}
