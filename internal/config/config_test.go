package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mjpitz/codesearch/internal/config"
)

func TestFindRoot_WalksUp(t *testing.T) {
	root := t.TempDir()

	err := os.Mkdir(filepath.Join(root, ".git"), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(root, "a", "b", "c")
	err = os.MkdirAll(nested, 0o755)
	if err != nil {
		t.Fatal(err)
	}

	got, err := config.FindRoot(nested)
	if err != nil {
		t.Fatalf("FindRoot: %v", err)
	}

	// Resolve symlinks on macOS (/private/var vs /var) before comparing.
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(root)
	if gotResolved != wantResolved {
		t.Errorf("FindRoot = %q, want %q", gotResolved, wantResolved)
	}
}

func TestFindRoot_NoGit(t *testing.T) {
	root := t.TempDir()

	_, err := config.FindRoot(root)
	if err == nil {
		t.Error("expected error when no .git is present")
	}
}

func TestIndexPaths(t *testing.T) {
	c := &config.IndexConfig{Root: "/repo"}

	got, want := c.IndexDir(), filepath.Join("/repo", config.IndexDirName)
	if got != want {
		t.Errorf("IndexDir = %q, want %q", got, want)
	}

	got, want = c.IndexPath(), filepath.Join("/repo", config.IndexDirName, config.IndexSubdir)
	if got != want {
		t.Errorf("IndexPath = %q, want %q", got, want)
	}

	got, want = c.MetaPath(), filepath.Join("/repo", config.IndexDirName, config.MetaFileName)
	if got != want {
		t.Errorf("MetaPath = %q, want %q", got, want)
	}
}
