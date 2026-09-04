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

func TestFindRoot_NoMarkers(t *testing.T) {
	root := t.TempDir()

	_, err := config.FindRoot(root)
	if err == nil {
		t.Error("expected error when neither .git nor .codesearch is present")
	}
}

func TestFindRoot_FindsCodesearchMarker(t *testing.T) {
	root := t.TempDir()

	err := os.Mkdir(filepath.Join(root, config.IndexDirName), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(root, "a", "b")
	err = os.MkdirAll(nested, 0o755)
	if err != nil {
		t.Fatal(err)
	}

	got, err := config.FindRoot(nested)
	if err != nil {
		t.Fatalf("FindRoot: %v", err)
	}

	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(root)
	if gotResolved != wantResolved {
		t.Errorf("FindRoot = %q, want %q", gotResolved, wantResolved)
	}
}

func TestFindRoot_PrefersNearestMarker(t *testing.T) {
	outer := t.TempDir()

	err := os.Mkdir(filepath.Join(outer, ".git"), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	inner := filepath.Join(outer, "sub")
	err = os.Mkdir(inner, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.Mkdir(filepath.Join(inner, config.IndexDirName), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	got, err := config.FindRoot(inner)
	if err != nil {
		t.Fatalf("FindRoot: %v", err)
	}

	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(inner)
	if gotResolved != wantResolved {
		t.Errorf("FindRoot = %q, want %q", gotResolved, wantResolved)
	}
}

func TestLoadOrInit_FallsBackToStart(t *testing.T) {
	root := t.TempDir()

	cfg, err := config.LoadOrInit(root)
	if err != nil {
		t.Fatalf("LoadOrInit: %v", err)
	}

	gotResolved, _ := filepath.EvalSymlinks(cfg.Root)
	wantResolved, _ := filepath.EvalSymlinks(root)
	if gotResolved != wantResolved {
		t.Errorf("cfg.Root = %q, want %q", gotResolved, wantResolved)
	}
	if cfg.MaxFileSize != config.DefaultMaxFileSize {
		t.Errorf("cfg.MaxFileSize = %d, want %d", cfg.MaxFileSize, config.DefaultMaxFileSize)
	}
}

func TestLoadOrInit_UsesExistingMarker(t *testing.T) {
	root := t.TempDir()

	err := os.Mkdir(filepath.Join(root, ".git"), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(root, "x", "y")
	err = os.MkdirAll(nested, 0o755)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadOrInit(nested)
	if err != nil {
		t.Fatalf("LoadOrInit: %v", err)
	}

	gotResolved, _ := filepath.EvalSymlinks(cfg.Root)
	wantResolved, _ := filepath.EvalSymlinks(root)
	if gotResolved != wantResolved {
		t.Errorf("cfg.Root = %q, want %q", gotResolved, wantResolved)
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
