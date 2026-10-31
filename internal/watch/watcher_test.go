package watch

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/ignore"
	syncpkg "github.com/mjpitz/codesearch/internal/sync"
)

func TestWatcher_RecursiveWatching(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "watcher_test_recursive")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create some structure
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	file1 := filepath.Join(subDir, "file1.txt")
	if err := os.WriteFile(file1, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.IndexConfig{
		Root:        tmpDir,
		MaxFileSize: 10 * 1024 * 1024,
	}
	matcher, _ := ignore.Load(tmpDir)
	logger := log.New(os.Stdout, "[test] ", 0)

	syncCalled := make(chan struct{}, 1)
	syncFunc := func(ctx context.Context, cfg *config.IndexConfig, opts syncpkg.RunOptions) (syncpkg.Result, error) {
		select {
		case syncCalled <- struct{}{}:
		default:
		}
		return syncpkg.Result{}, nil
	}

	w, err := NewWatcher(cfg, matcher, 10*time.Millisecond, logger, syncFunc)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := w.Run(ctx, []string{tmpDir}); err != nil {
			t.Errorf("Run error: %v", err)
		}
	}()

	// Wait for initial scan/watch to settle
	time.Sleep(100 * time.Millisecond)

	// Trigger an event in a subdirectory
	if err := os.WriteFile(file1, []byte("updated"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-syncCalled:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for sync callback after file change in subdir")
	}
}

func TestWatcher_Debouncing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "watcher_test_debounce")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.IndexConfig{
		Root:        tmpDir,
		MaxFileSize: 10 * 1024 * 1024,
	}
	matcher, _ := ignore.Load(tmpDir)
	logger := log.New(os.Stdout, "[test] ", 0)

	debounce := 200 * time.Millisecond
	syncCount := 0
	var mu sync.Mutex
	syncFunc := func(ctx context.Context, cfg *config.IndexConfig, opts syncpkg.RunOptions) (syncpkg.Result, error) {
		mu.Lock()
		syncCount++
		mu.Unlock()
		return syncpkg.Result{}, nil
	}

	w, err := NewWatcher(cfg, matcher, debounce, logger, syncFunc)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := w.Run(ctx, []string{tmpDir}); err != nil {
			t.Errorf("Run error: %v", err)
		}
	}()

	// Rapid changes
	for i := 0; i < 5; i++ {
		f := filepath.Join(tmpDir, "file.txt")
		_ = os.WriteFile(f, []byte(string(rune(i))), 0644)
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for debounce
	time.Sleep(debounce * 2)

	mu.Lock()
	count := syncCount
	mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 sync call due to debouncing, got %d", count)
	}
}

func TestWatcher_IgnorePatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "watcher_test_ignore")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create an ignore file
	ignoreFile := filepath.Join(tmpDir, ".codesearchignore")
	if err := os.WriteFile(ignoreFile, []byte("ignored_dir/\nignored_file.txt\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.IndexConfig{
		Root:        tmpDir,
		MaxFileSize: 10 * 1024 * 1024,
	}
	matcher, _ := ignore.Load(tmpDir)
	logger := log.New(os.Stdout, "[test] ", 0)

	syncCalled := make(chan struct{}, 1)
	syncFunc := func(ctx context.Context, cfg *config.IndexConfig, opts syncpkg.RunOptions) (syncpkg.Result, error) {
		select {
		case syncCalled <- struct{}{}:
		default:
		}
		return syncpkg.Result{}, nil
	}

	w, err := NewWatcher(cfg, matcher, 10*time.Millisecond, logger, syncFunc)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := w.Run(ctx, []string{tmpDir}); err != nil {
			t.Errorf("Run error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// 1. Change an ignored file
	if err := os.WriteFile(filepath.Join(tmpDir, "ignored_file.txt"), []byte("ignore me"), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Change a file in an ignored directory
	ignoreDir := filepath.Join(tmpDir, "ignored_dir")
	_ = os.Mkdir(ignoreDir, 0755)
	if err := os.WriteFile(filepath.Join(ignoreDir, "file.txt"), []byte("ignore me too"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Change a NON-ignored file
	if err := os.WriteFile(filepath.Join(tmpDir, "important.txt"), []byte("not ignored"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-syncCalled:
		// Success - it was called for important.txt
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for sync callback for non-ignored file")
	}

	// Clear the channel to check if anymore calls happen
	for len(syncCalled) > 0 {
		<-syncCalled
	}

	// Wait to see if ignored files trigger more syncs
	time.Sleep(100 * time.Millisecond)
	if len(syncCalled) > 0 {
		t.Error("sync called for ignored file/directory")
	}
}

func TestWatcher_DynamicDirectoryManagement(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "watcher_test_dynamic")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.IndexConfig{
		Root:        tmpDir,
		MaxFileSize: 10 * 1024 * 1024,
	}
	matcher, _ := ignore.Load(tmpDir)
	logger := log.New(os.Stdout, "[test] ", 0)

	syncCalled := make(chan struct{}, 1)
	syncFunc := func(ctx context.Context, cfg *config.IndexConfig, opts syncpkg.RunOptions) (syncpkg.Result, error) {
		select {
		case syncCalled <- struct{}{}:
		default:
		}
		return syncpkg.Result{}, nil
	}

	w, err := NewWatcher(cfg, matcher, 10*time.Millisecond, logger, syncFunc)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := w.Run(ctx, []string{tmpDir}); err != nil {
			t.Errorf("Run error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Create a new directory
	newDir := filepath.Join(tmpDir, "new_dir")
	if err := os.Mkdir(newDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Wait for watcher to pick up new dir and trigger sync
	select {
	case <-syncCalled:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for sync callback after new directory creation")
	}

	// Verify it's actually watching the new directory by adding a file to it
	if err := os.WriteFile(filepath.Join(newDir, "new_file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-syncCalled:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for sync callback after file creation in new directory")
	}
}

func TestWatcher_ErrorHandling(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "watcher_test_error")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.IndexConfig{
		Root:        tmpDir,
		MaxFileSize: 10 * 1024 * 1024,
	}
	matcher, _ := ignore.Load(tmpDir)
	
	logger := log.New(os.Stdout, "[test] ", 0) 

	errorSyncFunc := func(ctx context.Context, cfg *config.IndexConfig, opts syncpkg.RunOptions) (syncpkg.Result, error) {
		return syncpkg.Result{}, os.ErrPermission
	}
	
	wErr, _ := NewWatcher(cfg, matcher, 10*time.Millisecond, logger, errorSyncFunc)
	defer wErr.Close()
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = wErr.Run(ctx, []string{tmpDir})
	}()
	
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(tmpDir, "err_file.txt"), []byte("trigger error"), 0644); err != nil {
		t.Fatal(err)
	}

	// If we reach here without a panic, we've handled the error gracefully.
	time.Sleep(100 * time.Millisecond)
}
