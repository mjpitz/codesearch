package watch

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/ignore"
	syncpkg "github.com/mjpitz/codesearch/internal/sync"
)

// Watcher monitors the filesystem for changes and triggers a re-sync.
type Watcher struct {
	cfg             *config.IndexConfig
	matcher         *ignore.Matcher
	debounceTimeout time.Duration
	logger          *log.Logger
	syncFunc        func(context.Context, *config.IndexConfig, syncpkg.RunOptions) (syncpkg.Result, error)

	watcher *fsnotify.Watcher
	mu      sync.Mutex
	watched map[string]bool
}

// NewWatcher creates a new Watcher instance.
func NewWatcher(cfg *config.IndexConfig, matcher *ignore.Matcher, debounceTimeout time.Duration, logger *log.Logger, syncFunc func(context.Context, *config.IndexConfig, syncpkg.RunOptions) (syncpkg.Result, error)) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}

	return &Watcher{
		cfg:             cfg,
		matcher:         matcher,
		debounceTimeout: debounceTimeout,
		logger:          logger,
		syncFunc:        syncFunc,
		watcher:         w,
		watched:         make(map[string]bool),
	}, nil
}

// Close closes the fsnotify watcher.
func (w *Watcher) Close() error {
	return w.watcher.Close()
}

// Run starts the watching loop.
func (w *Watcher) Run(ctx context.Context, paths []string) error {
	syncChan := make(chan struct{}, 1)

	// 1. Watcher Goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-w.watcher.Errors:
				if !ok {
					return
				}
				w.logger.Printf("watcher error: %v\n", err)
			case event, ok := <-w.watcher.Events:
				if !ok {
					return
				}
				if err := w.handleEvent(event); err != nil {
					w.logger.Printf("event error: %v\n", err)
				} else {
					// If it's a relevant event, trigger sync
					select {
					case syncChan <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	// 2. Debounce/Coordinator Goroutine
	go func() {
		var timer *time.Timer
		var timerChan <-chan time.Time

		for {
			select {
			case <-ctx.Done():
				return
			case <-syncChan:
				if timer != nil {
					timer.Stop()
				}
				timer = time.NewTimer(w.debounceTimeout)
				timerChan = timer.C
			case <-timerChan:
				res, err := w.syncFunc(ctx, w.cfg, syncpkg.RunOptions{})
				if err != nil {
					w.logger.Printf("sync error: %v\n", err)
				} else if res.Upserted > 0 || res.Deleted > 0 {
					w.logger.Printf("sync complete: scanned=%d upserted=%d touched=%d deleted=%d unchanged=%d pruned=%d\n",
						res.Scanned, res.Upserted, res.Touched, res.Deleted, res.Unchanged, res.Pruned)
				}
				timerChan = nil // Reset timerChan
			}
		}
	}()

	// Initial recursive watch
	for _, p := range paths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("abs path %q: %w", p, err)
		}
		if err := w.addRecursive(absPath); err != nil {
			return fmt.Errorf("add recursive %q: %w", absPath, err)
		}
	}

	// 3. Main Goroutine (in this method)
	<-ctx.Done()
	return nil
}

func (w *Watcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(w.cfg.Root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if w.matcher.Match(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			w.mu.Lock()
			if !w.watched[path] {
				if err := w.watcher.Add(path); err != nil {
					w.mu.Unlock()
					return err
				}
				w.watched[path] = true
			}
			w.mu.Unlock()
			return nil
		}

		return nil
	})
}

func (w *Watcher) handleEvent(event fsnotify.Event) error {
	path := event.Name

	// Check if path is ignored
	rel, err := filepath.Rel(w.cfg.Root, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)

	if w.matcher.Match(rel) {
		return nil
	}

	// Handle events
	if event.Has(fsnotify.Create) {
		info, err := os.Stat(path)
		if err != nil {
			return nil // Path might have been deleted already
		}
		if info.IsDir() {
			return w.addRecursive(path)
		}
	}

	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		w.mu.Lock()
		delete(w.watched, path)
		w.mu.Unlock()
	}

	return nil
}
