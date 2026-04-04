package queue

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// QueueManager manages multiple named queues and their worker loops. It is the
// central registry for all queues in the system (embedding, semantic, custom).
type QueueManager struct {
	mountPoint             string
	maxConcurrentEmbedding int
	maxConcurrentSemantic  int
	pollInterval           time.Duration

	mu      sync.RWMutex
	queues  map[string]*NamedQueue
	workers map[string]*queueWorker
	started bool
}

// QueueManagerConfig configures a QueueManager.
type QueueManagerConfig struct {
	MountPoint             string
	MaxConcurrentEmbedding int
	MaxConcurrentSemantic  int
	PollInterval           time.Duration
}

// NewQueueManager creates a new queue manager.
func NewQueueManager(cfg QueueManagerConfig) *QueueManager {
	if cfg.MountPoint == "" {
		cfg.MountPoint = "/queue"
	}
	if cfg.MaxConcurrentEmbedding <= 0 {
		cfg.MaxConcurrentEmbedding = 10
	}
	if cfg.MaxConcurrentSemantic <= 0 {
		cfg.MaxConcurrentSemantic = 100
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 200 * time.Millisecond
	}
	return &QueueManager{
		mountPoint:             cfg.MountPoint,
		maxConcurrentEmbedding: cfg.MaxConcurrentEmbedding,
		maxConcurrentSemantic:  cfg.MaxConcurrentSemantic,
		pollInterval:           cfg.PollInterval,
		queues:                 make(map[string]*NamedQueue),
		workers:                make(map[string]*queueWorker),
	}
}

// Standard queue names.
const (
	QueueEmbedding = "Embedding"
	QueueSemantic  = "Semantic"
)

// Start launches worker goroutines for all registered queues.
func (m *QueueManager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return
	}
	m.started = true
	for _, q := range m.queues {
		m.startWorkerLocked(q)
	}
	log.Printf("[QueueManager] started")
}

// Stop signals all workers to shut down and waits for them to finish.
func (m *QueueManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return
	}
	for name, w := range m.workers {
		w.stop()
		log.Printf("[QueueManager] worker %s stopped", name)
	}
	m.workers = make(map[string]*queueWorker)
	m.started = false
	log.Printf("[QueueManager] stopped")
}

// IsRunning reports whether the manager has been started.
func (m *QueueManager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// GetQueue returns an existing queue or creates one if allowCreate is true.
func (m *QueueManager) GetQueue(name string, cfg *NamedQueueConfig, allowCreate bool) (*NamedQueue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if q, ok := m.queues[name]; ok {
		if m.started {
			m.startWorkerLocked(q)
		}
		return q, nil
	}
	if !allowCreate {
		return nil, &QueueNotFoundError{Name: name}
	}
	if cfg == nil {
		cfg = &NamedQueueConfig{Name: name}
	}
	cfg.Name = name
	q := NewNamedQueue(*cfg)
	m.queues[name] = q
	if m.started {
		m.startWorkerLocked(q)
	}
	return q, nil
}

// Enqueue adds a message to the named queue.
func (m *QueueManager) Enqueue(queueName string, data map[string]any) error {
	m.mu.RLock()
	q, ok := m.queues[queueName]
	m.mu.RUnlock()
	if !ok {
		return &QueueNotFoundError{Name: queueName}
	}
	return q.Enqueue(data)
}

// Size returns the pending count for a queue.
func (m *QueueManager) Size(queueName string) (int, error) {
	m.mu.RLock()
	q, ok := m.queues[queueName]
	m.mu.RUnlock()
	if !ok {
		return 0, &QueueNotFoundError{Name: queueName}
	}
	return q.Size(), nil
}

// CheckStatus returns status for one or all queues.
func (m *QueueManager) CheckStatus(queueName string) map[string]QueueStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if queueName != "" {
		q, ok := m.queues[queueName]
		if !ok {
			return nil
		}
		return map[string]QueueStatus{queueName: q.Status()}
	}
	result := make(map[string]QueueStatus, len(m.queues))
	for name, q := range m.queues {
		result[name] = q.Status()
	}
	return result
}

// HasErrors reports whether any queue (or a specific one) has errors.
func (m *QueueManager) HasErrors(queueName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if queueName != "" {
		q, ok := m.queues[queueName]
		return ok && q.Status().HasErrors()
	}
	for _, q := range m.queues {
		if q.Status().HasErrors() {
			return true
		}
	}
	return false
}

// IsAllComplete reports whether all queues (or a specific one) are idle.
func (m *QueueManager) IsAllComplete(queueName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if queueName != "" {
		q, ok := m.queues[queueName]
		return !ok || q.Status().IsComplete()
	}
	for _, q := range m.queues {
		if !q.Status().IsComplete() {
			return false
		}
	}
	return true
}

// QueueNames returns the names of all registered queues.
func (m *QueueManager) QueueNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.queues))
	for n := range m.queues {
		names = append(names, n)
	}
	return names
}

// ---- internal worker ----

func (m *QueueManager) startWorkerLocked(q *NamedQueue) {
	if _, ok := m.workers[q.Name]; ok {
		return
	}
	maxConcurrent := 1
	switch q.Name {
	case QueueEmbedding:
		maxConcurrent = m.maxConcurrentEmbedding
	case QueueSemantic:
		maxConcurrent = m.maxConcurrentSemantic
	}
	w := newQueueWorker(q, maxConcurrent, m.pollInterval)
	m.workers[q.Name] = w
	w.start()
}

// queueWorker drains a NamedQueue in a background goroutine.
type queueWorker struct {
	queue         *NamedQueue
	maxConcurrent int
	pollInterval  time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

func newQueueWorker(q *NamedQueue, maxConcurrent int, pollInterval time.Duration) *queueWorker {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	return &queueWorker{
		queue:         q,
		maxConcurrent: maxConcurrent,
		pollInterval:  pollInterval,
		stopCh:        make(chan struct{}),
	}
}

func (w *queueWorker) start() {
	w.wg.Add(1)
	go w.loop()
}

func (w *queueWorker) stop() {
	close(w.stopCh)
	w.wg.Wait()
}

func (w *queueWorker) loop() {
	defer w.wg.Done()
	sem := make(chan struct{}, w.maxConcurrent)

	for {
		select {
		case <-w.stopCh:
			return
		default:
		}

		if !w.queue.HasDequeueHandler() || w.queue.Size() == 0 {
			select {
			case <-w.stopCh:
				return
			case <-time.After(w.pollInterval):
				continue
			}
		}

		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			if _, err := w.queue.Dequeue(); err != nil {
				log.Printf("[QueueWorker] %s dequeue error: %v", w.queue.Name, err)
			}
		}()
	}
}

// QueueNotFoundError is returned when a queue does not exist.
type QueueNotFoundError struct {
	Name string
}

func (e *QueueNotFoundError) Error() string {
	return fmt.Sprintf("queue %q does not exist", e.Name)
}
