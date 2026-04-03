package queue

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/indexer"
)

// Job represents an asynchronous indexing job.
type Job struct {
	ID        string
	URI       string
	ReqCtx    *ctx.RequestContext
	CreatedAt time.Time
}

// Stats holds queue statistics.
type Stats struct {
	Pending   int   `json:"pending"`
	Running   int   `json:"running"`
	Completed int64 `json:"completed"`
	Failed    int64 `json:"failed"`
}

// EmbeddingQueue is an async worker pool for embedding/indexing jobs.
// It prevents large imports from blocking the API by processing
// vectorization in the background with configurable concurrency.
type EmbeddingQueue struct {
	indexer     *indexer.Indexer
	jobs        chan Job
	workers     int
	wg          sync.WaitGroup
	stopCh      chan struct{}
	running     int32
	completed   int64
	failed      int64
	mu          sync.Mutex
}

// NewEmbeddingQueue creates a new queue with the given worker count and buffer size.
func NewEmbeddingQueue(idx *indexer.Indexer, workers, bufferSize int) *EmbeddingQueue {
	if workers <= 0 {
		workers = 2
	}
	if bufferSize <= 0 {
		bufferSize = 1000
	}
	return &EmbeddingQueue{
		indexer: idx,
		jobs:    make(chan Job, bufferSize),
		workers: workers,
		stopCh:  make(chan struct{}),
	}
}

// Start launches the worker goroutines.
func (q *EmbeddingQueue) Start() {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}
	log.Printf("[Queue] started %d embedding workers (buffer=%d)", q.workers, cap(q.jobs))
}

// Stop gracefully shuts down the queue, waiting for in-flight jobs.
func (q *EmbeddingQueue) Stop() {
	close(q.stopCh)
	close(q.jobs)
	q.wg.Wait()
	log.Printf("[Queue] stopped (completed=%d, failed=%d)", q.completed, q.failed)
}

// Enqueue adds an indexing job to the queue. Returns an error if the queue is full.
func (q *EmbeddingQueue) Enqueue(uri string, reqCtx *ctx.RequestContext) error {
	job := Job{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		URI:       uri,
		ReqCtx:    reqCtx,
		CreatedAt: time.Now(),
	}

	select {
	case q.jobs <- job:
		return nil
	default:
		return fmt.Errorf("queue is full (capacity=%d)", cap(q.jobs))
	}
}

// Stats returns current queue statistics.
func (q *EmbeddingQueue) Stats() Stats {
	return Stats{
		Pending:   len(q.jobs),
		Running:   int(atomic.LoadInt32(&q.running)),
		Completed: atomic.LoadInt64(&q.completed),
		Failed:    atomic.LoadInt64(&q.failed),
	}
}

func (q *EmbeddingQueue) worker(id int) {
	defer q.wg.Done()

	for {
		select {
		case <-q.stopCh:
			for job := range q.jobs {
				q.processJob(id, job)
			}
			return
		case job, ok := <-q.jobs:
			if !ok {
				return
			}
			q.processJob(id, job)
		}
	}
}

func (q *EmbeddingQueue) processJob(workerID int, job Job) {
	atomic.AddInt32(&q.running, 1)
	defer atomic.AddInt32(&q.running, -1)

	start := time.Now()
	result, err := q.indexer.IndexDirectory(job.URI, job.ReqCtx)
	elapsed := time.Since(start)

	if err != nil {
		atomic.AddInt64(&q.failed, 1)
		log.Printf("[Queue] worker-%d job %s FAILED (%s): %v", workerID, job.URI, elapsed, err)
		return
	}

	atomic.AddInt64(&q.completed, 1)
	log.Printf("[Queue] worker-%d job %s OK (indexed=%d skipped=%d errors=%d, %s)",
		workerID, job.URI, result.Indexed, result.Skipped, result.Errors, elapsed)
}
