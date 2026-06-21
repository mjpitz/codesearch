package index

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// DefaultIdleTimeout is how long LazyIndex waits with zero refs before
// closing the underlying handle and releasing the OS file lock.
const DefaultIdleTimeout = 30 * time.Second

// DefaultRetryBudget caps how long Acquire will keep retrying when the
// underlying open hits ErrIndexLocked (i.e. another process holds the
// writer lock).
const DefaultRetryBudget = 30 * time.Second

// LazyIndex is a refcounted, idle-closing wrapper around a read-only
// *Index. The MCP server uses it so concurrent tool requests share a
// single open handle while sync/query CLI invocations can still acquire
// the OS file lock during idle windows.
//
// Concurrent Acquire calls are coalesced: the first opens the handle,
// the rest wait. Once all refs are released, an idle timer closes the
// handle so other processes are free to write.
type LazyIndex struct {
	path        string
	idleTimeout time.Duration
	retryBudget time.Duration

	mu     sync.Mutex
	inner  *Index
	refs   int
	timer  *time.Timer
	openCh chan struct{}
}

// NewLazy constructs a LazyIndex over path. idle and retry default to
// DefaultIdleTimeout and DefaultRetryBudget when zero.
func NewLazy(path string, idle, retry time.Duration) *LazyIndex {
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}
	if retry <= 0 {
		retry = DefaultRetryBudget
	}
	return &LazyIndex{
		path:        path,
		idleTimeout: idle,
		retryBudget: retry,
	}
}

// Acquire returns the shared *Index and a release func. Callers must
// invoke release exactly once (typically with defer). Opens the index
// lazily on the first uncontended call; subsequent concurrent calls
// reuse the same handle.
func (l *LazyIndex) Acquire() (*Index, func(), error) {
	for {
		l.mu.Lock()

		if l.inner != nil {
			if l.timer != nil {
				l.timer.Stop()
				l.timer = nil
			}
			l.refs++
			inner := l.inner
			l.mu.Unlock()
			return inner, l.release, nil
		}

		if l.openCh != nil {
			ch := l.openCh
			l.mu.Unlock()
			<-ch
			continue
		}

		ch := make(chan struct{})
		l.openCh = ch
		l.mu.Unlock()

		idx, err := openWithRetry(l.path, l.retryBudget)

		l.mu.Lock()
		l.openCh = nil
		close(ch)
		if err != nil {
			l.mu.Unlock()
			return nil, nil, err
		}
		l.inner = idx
		l.refs++
		l.mu.Unlock()
		return idx, l.release, nil
	}
}

// Close releases the underlying handle. It errors if any refs are
// outstanding so a buggy shutdown can't yank the index from under an
// in-flight request.
func (l *LazyIndex) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.refs > 0 {
		return fmt.Errorf("lazy index: cannot close with %d active refs", l.refs)
	}
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	if l.inner == nil {
		return nil
	}
	err := l.inner.Close()
	l.inner = nil
	return err
}

func (l *LazyIndex) release() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.refs--
	if l.refs > 0 {
		return
	}
	if l.refs < 0 {
		panic("lazy index: release called more times than acquire")
	}
	if l.timer != nil {
		l.timer.Stop()
	}
	l.timer = time.AfterFunc(l.idleTimeout, l.closeIfIdle)
}

func (l *LazyIndex) closeIfIdle() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.refs > 0 || l.inner == nil {
		return
	}
	_ = l.inner.Close()
	l.inner = nil
	l.timer = nil
}

func openWithRetry(path string, budget time.Duration) (*Index, error) {
	deadline := time.Now().Add(budget)
	backoff := 200 * time.Millisecond

	for {
		idx, err := OpenReadOnly(path)
		if err == nil {
			return idx, nil
		}
		if !errors.Is(err, ErrIndexLocked) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(backoff)
		if backoff < 2*time.Second {
			backoff *= 2
		}
	}
}
