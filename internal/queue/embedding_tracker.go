package queue

import (
	"log"
	"sync"
)

// CompletionCallback is invoked when all embedding tasks for a semantic
// message have been processed.
type CompletionCallback func()

type taskRecord struct {
	Remaining  int
	Total      int
	OnComplete CompletionCallback
}

// EmbeddingTaskTracker coordinates embedding task completion across queues.
// When all embedding tasks for a semantic message are done, the registered
// callback fires (typically to release a lifecycle lock or enqueue the next
// processing step).
type EmbeddingTaskTracker struct {
	mu    sync.Mutex
	tasks map[string]*taskRecord
}

var (
	globalTracker     *EmbeddingTaskTracker
	globalTrackerOnce sync.Once
)

// GetEmbeddingTaskTracker returns the global singleton tracker.
func GetEmbeddingTaskTracker() *EmbeddingTaskTracker {
	globalTrackerOnce.Do(func() {
		globalTracker = NewEmbeddingTaskTracker()
	})
	return globalTracker
}

// NewEmbeddingTaskTracker creates a new tracker.
func NewEmbeddingTaskTracker() *EmbeddingTaskTracker {
	return &EmbeddingTaskTracker{
		tasks: make(map[string]*taskRecord),
	}
}

// Register records a semantic message with its total embedding task count.
// If totalCount <= 0, the callback fires immediately.
func (t *EmbeddingTaskTracker) Register(semanticMsgID string, totalCount int, onComplete CompletionCallback) {
	t.mu.Lock()

	if _, exists := t.tasks[semanticMsgID]; exists {
		log.Printf("[EmbeddingTracker] overwriting existing record for %s", semanticMsgID)
	}

	t.tasks[semanticMsgID] = &taskRecord{
		Remaining:  totalCount,
		Total:      totalCount,
		OnComplete: onComplete,
	}

	log.Printf("[EmbeddingTracker] registered %s: %d tasks", semanticMsgID, totalCount)

	if totalCount <= 0 {
		rec := t.tasks[semanticMsgID]
		delete(t.tasks, semanticMsgID)
		t.mu.Unlock()
		log.Printf("[EmbeddingTracker] no tasks for %s, firing callback immediately", semanticMsgID)
		if rec.OnComplete != nil {
			rec.OnComplete()
		}
		return
	}

	t.mu.Unlock()
}

// Decrement reduces the remaining count for a semantic message. When it
// reaches zero, the completion callback is invoked and the entry is removed.
// Returns the remaining count, or -1 if the ID was not found.
func (t *EmbeddingTaskTracker) Decrement(semanticMsgID string) int {
	t.mu.Lock()

	rec, ok := t.tasks[semanticMsgID]
	if !ok {
		t.mu.Unlock()
		return -1
	}

	rec.Remaining--
	remaining := rec.Remaining

	if remaining <= 0 {
		delete(t.tasks, semanticMsgID)
		t.mu.Unlock()
		log.Printf("[EmbeddingTracker] all %d tasks completed for %s", rec.Total, semanticMsgID)
		if rec.OnComplete != nil {
			rec.OnComplete()
		}
		return 0
	}

	t.mu.Unlock()
	return remaining
}

// Remaining returns the remaining task count for a semantic message.
// Returns -1 if not found.
func (t *EmbeddingTaskTracker) Remaining(semanticMsgID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	rec, ok := t.tasks[semanticMsgID]
	if !ok {
		return -1
	}
	return rec.Remaining
}

// ActiveCount returns the number of active tracking entries.
func (t *EmbeddingTaskTracker) ActiveCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.tasks)
}

// Remove cancels tracking for a semantic message without firing the callback.
func (t *EmbeddingTaskTracker) Remove(semanticMsgID string) {
	t.mu.Lock()
	delete(t.tasks, semanticMsgID)
	t.mu.Unlock()
}
