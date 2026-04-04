package queue

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestEmbeddingTrackerBasic(t *testing.T) {
	tracker := NewEmbeddingTaskTracker()

	var called int32
	tracker.Register("msg-1", 3, func() {
		atomic.AddInt32(&called, 1)
	})

	if tracker.ActiveCount() != 1 {
		t.Errorf("active = %d", tracker.ActiveCount())
	}

	r := tracker.Decrement("msg-1")
	if r != 2 {
		t.Errorf("remaining = %d, want 2", r)
	}
	r = tracker.Decrement("msg-1")
	if r != 1 {
		t.Errorf("remaining = %d, want 1", r)
	}
	r = tracker.Decrement("msg-1")
	if r != 0 {
		t.Errorf("remaining = %d, want 0", r)
	}

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("callback called %d times", called)
	}
	if tracker.ActiveCount() != 0 {
		t.Errorf("active after completion = %d", tracker.ActiveCount())
	}
}

func TestEmbeddingTrackerZeroTasks(t *testing.T) {
	tracker := NewEmbeddingTaskTracker()
	var called int32
	tracker.Register("msg-zero", 0, func() {
		atomic.AddInt32(&called, 1)
	})

	if atomic.LoadInt32(&called) != 1 {
		t.Error("callback not fired for zero tasks")
	}
	if tracker.ActiveCount() != 0 {
		t.Errorf("active = %d", tracker.ActiveCount())
	}
}

func TestEmbeddingTrackerNotFound(t *testing.T) {
	tracker := NewEmbeddingTaskTracker()
	r := tracker.Decrement("nonexistent")
	if r != -1 {
		t.Errorf("expected -1 for unknown ID, got %d", r)
	}
}

func TestEmbeddingTrackerRemove(t *testing.T) {
	tracker := NewEmbeddingTaskTracker()
	tracker.Register("msg-rm", 5, nil)
	tracker.Remove("msg-rm")
	if tracker.ActiveCount() != 0 {
		t.Error("expected 0 active after remove")
	}
}

func TestEmbeddingTrackerConcurrent(t *testing.T) {
	tracker := NewEmbeddingTaskTracker()
	n := 100
	var callCount int32

	tracker.Register("msg-conc", n, func() {
		atomic.AddInt32(&callCount, 1)
	})

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.Decrement("msg-conc")
		}()
	}
	wg.Wait()

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("callback called %d times (expected exactly 1)", callCount)
	}
}

func TestEmbeddingTrackerRemaining(t *testing.T) {
	tracker := NewEmbeddingTaskTracker()
	tracker.Register("msg-r", 5, nil)

	if r := tracker.Remaining("msg-r"); r != 5 {
		t.Errorf("remaining = %d", r)
	}
	tracker.Decrement("msg-r")
	if r := tracker.Remaining("msg-r"); r != 4 {
		t.Errorf("remaining after decrement = %d", r)
	}

	if r := tracker.Remaining("unknown"); r != -1 {
		t.Errorf("unknown remaining = %d", r)
	}
}

func TestEmbeddingTrackerSingleton(t *testing.T) {
	a := GetEmbeddingTaskTracker()
	b := GetEmbeddingTaskTracker()
	if a != b {
		t.Error("singleton should return same pointer")
	}
}
