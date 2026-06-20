package ignore_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mjpitz/codesearch/internal/ignore"
)

func TestLoad_NoFile(t *testing.T) {
	dir := t.TempDir()

	m, err := ignore.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !m.Match(".git/HEAD") {
		t.Errorf("expected .git/HEAD to match default ignore")
	}
	if !m.Match(".codesearch/index/store") {
		t.Errorf("expected .codesearch/index/store to match default ignore")
	}
	if m.Match("docs/README.md") {
		t.Errorf("expected docs/README.md to be allowed without ignore file")
	}
}

func TestLoad_WithFile(t *testing.T) {
	dir := t.TempDir()
	contents := "# comment\nnode_modules/\n\n*.log\n"

	err := os.WriteFile(filepath.Join(dir, ignore.IgnoreFileName), []byte(contents), 0o644)
	if err != nil {
		t.Fatalf("write ignore file: %v", err)
	}

	m, err := ignore.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := []struct {
		path   string
		ignore bool
	}{
		{"node_modules/foo/bar.js", true},
		{"app.log", true},
		{".git/objects/abc", true},
		{".codesearch/meta.json", true},
		{"docs/README.md", false},
		{"services/clickhouse/README.md", false},
	}
	for _, tc := range cases {
		got := m.Match(tc.path)
		if got != tc.ignore {
			t.Errorf("Match(%q) = %v, want %v", tc.path, got, tc.ignore)
		}
	}
}
