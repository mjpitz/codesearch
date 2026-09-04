package index

import (
	"sync"
	"time"
)

// Pool holds one LazyIndex per index path so a single MCP server can
// answer tool calls against multiple projects. Each entry keeps the same
// refcounted, idle-close semantics as a standalone LazyIndex — the pool
// only handles get-or-create and bulk close.
type Pool struct {
	idle  time.Duration
	retry time.Duration

	mu      sync.Mutex
	entries map[string]*LazyIndex
}

// NewPool constructs an empty Pool. idle and retry defaults follow
// NewLazy (zero → DefaultIdleTimeout / DefaultRetryBudget).
func NewPool(idle, retry time.Duration) *Pool {
	return &Pool{
		idle:    idle,
		retry:   retry,
		entries: make(map[string]*LazyIndex),
	}
}

// Get returns the LazyIndex for indexPath, creating one on first
// request. Safe for concurrent use.
func (p *Pool) Get(indexPath string) *LazyIndex {
	p.mu.Lock()
	defer p.mu.Unlock()

	if l, ok := p.entries[indexPath]; ok {
		return l
	}
	l := NewLazy(indexPath, p.idle, p.retry)
	p.entries[indexPath] = l
	return l
}

// Close closes every LazyIndex in the pool. The first close error is
// returned; subsequent errors are dropped but every entry is still
// attempted.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var firstErr error
	for _, l := range p.entries {
		if err := l.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	p.entries = nil
	return firstErr
}
