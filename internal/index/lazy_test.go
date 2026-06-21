package index_test

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mjpitz/codesearch/internal/index"
)

func newLazyTarget(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "idx")
	idx, err := index.Create(dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	err = idx.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	return dir
}

func TestLazy_OpenReuseAndIdleClose(t *testing.T) {
	dir := newLazyTarget(t)
	lazy := index.NewLazy(dir, 50*time.Millisecond, time.Second)
	t.Cleanup(func() { _ = lazy.Close() })

	first, release1, err := lazy.Acquire()
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}
	if first == nil {
		t.Fatal("Acquire 1 returned nil index")
	}

	second, release2, err := lazy.Acquire()
	if err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}
	if second != first {
		t.Error("Acquire 2 returned a different *Index; expected handle reuse")
	}

	release1()
	release2()

	time.Sleep(150 * time.Millisecond) // > idle timeout

	// After idle close, the next acquire must succeed by opening a fresh
	// handle (write op on the underlying bbolt would fail otherwise).
	third, release3, err := lazy.Acquire()
	if err != nil {
		t.Fatalf("Acquire 3: %v", err)
	}
	if third == nil {
		t.Fatal("Acquire 3 returned nil index")
	}
	release3()
}

func TestLazy_ConcurrentAcquireCoalesces(t *testing.T) {
	dir := newLazyTarget(t)
	lazy := index.NewLazy(dir, 100*time.Millisecond, time.Second)
	t.Cleanup(func() { _ = lazy.Close() })

	const n = 16
	results := make([]*index.Index, n)
	releases := make([]func(), n)
	errs := make([]error, n)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], releases[i], errs[i] = lazy.Acquire()
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("Acquire %d: %v", i, errs[i])
		}
		if results[i] != results[0] {
			t.Errorf("Acquire %d returned a different handle; expected coalesced open", i)
		}
	}
	for _, r := range releases {
		r()
	}
}

func TestLazy_CloseRefusesWhileHeld(t *testing.T) {
	dir := newLazyTarget(t)
	lazy := index.NewLazy(dir, time.Second, time.Second)

	_, release, err := lazy.Acquire()
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	err = lazy.Close()
	if err == nil {
		t.Error("Close while held returned nil; want error")
	}

	release()

	err = lazy.Close()
	if err != nil {
		t.Errorf("Close after release: %v", err)
	}
}

func TestLazy_AcquireWaitsForExclusiveWriter(t *testing.T) {
	dir := newLazyTarget(t)

	// Hold an exclusive writer lock from another in-process handle. While
	// it's open, OpenReadOnly inside Acquire will time out with
	// ErrIndexLocked; the retry budget must keep trying until we close.
	writer, err := index.Open(dir)
	if err != nil {
		t.Fatalf("Open writer: %v", err)
	}

	lazy := index.NewLazy(dir, 50*time.Millisecond, 5*time.Second)
	t.Cleanup(func() { _ = lazy.Close() })

	acquired := make(chan struct{})
	var (
		got     *index.Index
		release func()
		acqErr  error
	)
	go func() {
		got, release, acqErr = lazy.Acquire()
		close(acquired)
	}()

	// Give the goroutine time to start and hit the locked open.
	time.Sleep(300 * time.Millisecond)

	select {
	case <-acquired:
		t.Fatal("Acquire returned before writer closed")
	default:
	}

	err = writer.Close()
	if err != nil {
		t.Fatalf("writer Close: %v", err)
	}

	select {
	case <-acquired:
	case <-time.After(15 * time.Second):
		t.Fatal("Acquire did not return after writer closed")
	}

	if acqErr != nil {
		t.Fatalf("Acquire returned err: %v", acqErr)
	}
	if got == nil {
		t.Fatal("Acquire returned nil index")
	}
	release()
}
