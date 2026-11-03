package watch

import (
	"context"
	"fmt"
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
	matchers        map[string]*ignore.Matcher // map of absolute path to matcher
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
		matchers:        make(map[string]*ignore.Matcher),
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
				relevant, err := w.handleEvent(event)
				if err != nil {
					w.logger.Printf("event error: %v\n", err)
				} else if relevant {
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
	rel, err := filepath.Rel(w.cfg.Root, root)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)

	// Check if the root itself is ignored.
	if ignore.IsIgnored([]*ignore.Matcher{w.matcher}, rel, true) {
		return nil
	}

	w.mu.Lock()
	if !w.watched[root] {
		if err := w.watcher.Add(root); err != nil {
			w.mu.Unlock()
			return err
		}
		w.watched[root] = true
	}
	w.mu.Unlock()

	return w.addRecursiveInternal(root, []*ignore.Matcher{w.matcher})
}

func (w *Watcher) addRecursiveInternal(path string, stack []*ignore.Matcher) error {
	// 1. Check for a new matcher in this directory.
	m := ignore.DirMatcher(w.cfg.Root, path)
	newStack := stack
	if m != nil {
		w.mu.Lock()
		w.matchers[path] = m
		w.mu.Unlock()
		newStack = append(stack, m)
	}

	// 2. Read the directory entries.
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		childPath := filepath.Join(path, entry.Name())
		rel, err := filepath.Rel(w.cfg.Root, childPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if ignore.IsIgnored(newStack, rel, entry.IsDir()) {
			continue
		}

		if entry.IsDir() {
			w.mu.Lock()
			if !w.watched[childPath] {
				if err := w.watcher.Add(childPath); err != nil {
					w.mu.Unlock()
					return err
				}
				w.watched[childPath] = true
			}
			w.mu.Unlock()

			if err := w.addRecursiveInternal(childPath, newStack); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *Watcher) getMatcherStack(path string) []*ignore.Matcher {
	w.mu.Lock()
	defer w.mu.Unlock()

	var collected []*ignore.Matcher
	curr := filepath.Dir(path)

	for {
		if m, ok := w.matchers[curr]; ok {
			collected = append(collected, m)
		}

		if curr == w.cfg.Root || curr == "." || curr == "/" {
			break
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}

	stack := make([]*ignore.Matcher, 0, len(collected)+1)
	stack = append(stack, w.matcher)
	for i := len(collected) - 1; i >= 0; i-- {
		stack = append(stack, collected[i])
	}
	return stack
}

func (w *Watcher) handleEvent(event fsnotify.Event) (bool, error) {
	path := event.Name

	// Handle ignore file changes
	base := filepath.Base(path)
	if base == ".gitignore" || base == ignore.IgnoreFileName {
		dir := filepath.Dir(path)
		m := ignore.DirMatcher(w.cfg.Root, dir)
		w.mu.Lock()
		if m != nil {
			w.matchers[dir] = m
		} else {
			delete(w.matchers, dir)
		}
		w.mu.Unlock()
		return true, nil
	}

	// Check if path is ignored
	rel, err := filepath.Rel(w.cfg.Root, path)
	if err != nil {
		return false, err
	}
	rel = filepath.ToSlash(rel)

	stack := w.getMatcherStack(path)
	if ignore.IsIgnored(stack, rel, false) {
		return false, nil
	}

	// Handle events
	if event.Has(fsnotify.Create) {
		info, err := os.Stat(path)
		if err != nil {
			return false, nil // Path might have been deleted already
		}
		if info.IsDir() {
			if err := w.addRecursive(path); err != nil {
				return false, err
			}
		}
	}

	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		w.mu.Lock()
		delete(w.watched, path)
		delete(w.matchers, path)
		w.mu.Unlock()
	}

	return true, nil
}
