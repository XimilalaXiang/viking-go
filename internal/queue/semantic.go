package queue

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
)

// SemanticMsg represents a semantic processing message.
type SemanticMsg struct {
	URI         string `json:"uri"`
	ContextType string `json:"context_type"`
	Recursive   bool   `json:"recursive"`
	Incremental bool   `json:"incremental"`
	MsgID       string `json:"msg_id,omitempty"`
}

// DirNode tracks the state of a directory node in the DAG execution.
type DirNode struct {
	URI              string
	ChildrenDirs     []string
	FilePaths        []string
	FileSummaries    map[string]*FileSummary
	ChildAbstracts   map[string]string
	Pending          int32
	Dispatched       bool
	OverviewDone     bool
	mu               sync.Mutex
}

// FileSummary holds the generated summary for a file.
type FileSummary struct {
	Abstract string
	Overview string
}

// DagStats tracks DAG execution progress.
type DagStats struct {
	TotalNodes    int32 `json:"total_nodes"`
	PendingNodes  int32 `json:"pending_nodes"`
	RunningNodes  int32 `json:"running_nodes"`
	DoneNodes     int32 `json:"done_nodes"`
}

// SummarizeFunc is the callback for generating file/directory summaries.
// It receives a URI and context type, returns abstract + overview.
type SummarizeFunc func(uri, contextType string, reqCtx *ctx.RequestContext) (abstract, overview string, err error)

// ListChildrenFunc returns the children (files and subdirectories) of a URI.
type ListChildrenFunc func(uri string, reqCtx *ctx.RequestContext) (files []string, dirs []string, err error)

// SemanticDagExecutor processes a directory tree bottom-up,
// generating summaries for files first, then rolling up to directories.
type SemanticDagExecutor struct {
	contextType    string
	reqCtx         *ctx.RequestContext
	recursive      bool
	maxConcurrent  int
	summarize      SummarizeFunc
	listChildren   ListChildrenFunc

	nodes   map[string]*DirNode
	parent  map[string]string
	rootURI string
	stats   DagStats
	sem     chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	err     error
}

// NewSemanticDagExecutor creates a new DAG executor.
func NewSemanticDagExecutor(
	contextType string,
	reqCtx *ctx.RequestContext,
	recursive bool,
	maxConcurrent int,
	summarize SummarizeFunc,
	listChildren ListChildrenFunc,
) *SemanticDagExecutor {
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}
	return &SemanticDagExecutor{
		contextType:   contextType,
		reqCtx:        reqCtx,
		recursive:     recursive,
		maxConcurrent: maxConcurrent,
		summarize:     summarize,
		listChildren:  listChildren,
		nodes:         make(map[string]*DirNode),
		parent:        make(map[string]string),
		sem:           make(chan struct{}, maxConcurrent),
		done:          make(chan struct{}),
	}
}

// Execute runs the DAG from the given root URI.
// It blocks until all nodes are processed or an error occurs.
func (d *SemanticDagExecutor) Execute(rootURI string) error {
	d.rootURI = rootURI
	if err := d.buildDAG(rootURI, ""); err != nil {
		return fmt.Errorf("build DAG: %w", err)
	}
	if len(d.nodes) == 0 {
		return nil
	}
	atomic.StoreInt32(&d.stats.TotalNodes, int32(len(d.nodes)))
	atomic.StoreInt32(&d.stats.PendingNodes, int32(len(d.nodes)))

	d.dispatchLeaves()

	<-d.done
	return d.err
}

// Stats returns current execution statistics.
func (d *SemanticDagExecutor) Stats() DagStats {
	return DagStats{
		TotalNodes:   atomic.LoadInt32(&d.stats.TotalNodes),
		PendingNodes: atomic.LoadInt32(&d.stats.PendingNodes),
		RunningNodes: atomic.LoadInt32(&d.stats.RunningNodes),
		DoneNodes:    atomic.LoadInt32(&d.stats.DoneNodes),
	}
}

func (d *SemanticDagExecutor) buildDAG(uri, parentURI string) error {
	files, dirs, err := d.listChildren(uri, d.reqCtx)
	if err != nil {
		return err
	}

	node := &DirNode{
		URI:            uri,
		FilePaths:      files,
		FileSummaries:  make(map[string]*FileSummary),
		ChildAbstracts: make(map[string]string),
	}

	if d.recursive {
		node.ChildrenDirs = dirs
	}

	node.Pending = int32(len(node.FilePaths) + len(node.ChildrenDirs))

	d.mu.Lock()
	d.nodes[uri] = node
	if parentURI != "" {
		d.parent[uri] = parentURI
	}
	d.mu.Unlock()

	if d.recursive {
		for _, childDir := range dirs {
			if err := d.buildDAG(childDir, uri); err != nil {
				return err
			}
		}
	}

	return nil
}

func (d *SemanticDagExecutor) dispatchLeaves() {
	for _, node := range d.nodes {
		d.dispatchNode(node)
	}
}

func (d *SemanticDagExecutor) dispatchNode(node *DirNode) {
	node.mu.Lock()
	if node.Dispatched {
		node.mu.Unlock()
		return
	}
	node.Dispatched = true
	node.mu.Unlock()

	for _, filePath := range node.FilePaths {
		fp := filePath
		go func() {
			d.sem <- struct{}{}
			defer func() { <-d.sem }()
			atomic.AddInt32(&d.stats.RunningNodes, 1)

			abstract, overview, err := d.summarize(fp, d.contextType, d.reqCtx)
			atomic.AddInt32(&d.stats.RunningNodes, -1)

			if err != nil {
				log.Printf("[SemanticDAG] file summary error %s: %v", fp, err)
			}

			node.mu.Lock()
			node.FileSummaries[fp] = &FileSummary{Abstract: abstract, Overview: overview}
			remaining := atomic.AddInt32(&node.Pending, -1)
			node.mu.Unlock()

			if remaining <= 0 {
				d.onNodeComplete(node)
			}
		}()
	}

	if len(node.FilePaths) == 0 && atomic.LoadInt32(&node.Pending) <= 0 {
		d.onNodeComplete(node)
	}
}

func (d *SemanticDagExecutor) onNodeComplete(node *DirNode) {
	node.mu.Lock()
	if node.OverviewDone {
		node.mu.Unlock()
		return
	}
	node.OverviewDone = true
	node.mu.Unlock()

	d.sem <- struct{}{}
	abstract, overview, err := d.summarize(node.URI, d.contextType, d.reqCtx)
	<-d.sem

	if err != nil {
		log.Printf("[SemanticDAG] dir summary error %s: %v", node.URI, err)
	}

	atomic.AddInt32(&d.stats.DoneNodes, 1)
	atomic.AddInt32(&d.stats.PendingNodes, -1)

	parentURI, hasParent := d.parent[node.URI]
	if hasParent {
		d.mu.Lock()
		parentNode := d.nodes[parentURI]
		d.mu.Unlock()

		if parentNode != nil {
			parentNode.mu.Lock()
			parentNode.ChildAbstracts[node.URI] = abstract
			_ = overview
			remaining := atomic.AddInt32(&parentNode.Pending, -1)
			parentNode.mu.Unlock()

			if remaining <= 0 {
				d.onNodeComplete(parentNode)
			}
		}
	}

	if node.URI == d.rootURI {
		close(d.done)
	}
}

// SemanticQueue processes semantic messages asynchronously.
type SemanticQueue struct {
	msgs      chan SemanticMsg
	workers   int
	stopCh    chan struct{}
	wg        sync.WaitGroup
	running   int32
	completed int64
	failed    int64
	reqCtx    *ctx.RequestContext

	summarize    SummarizeFunc
	listChildren ListChildrenFunc
}

// NewSemanticQueue creates a new semantic processing queue.
func NewSemanticQueue(workers, bufferSize int, summarize SummarizeFunc, listChildren ListChildrenFunc) *SemanticQueue {
	if workers <= 0 {
		workers = 1
	}
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &SemanticQueue{
		msgs:         make(chan SemanticMsg, bufferSize),
		workers:      workers,
		stopCh:       make(chan struct{}),
		summarize:    summarize,
		listChildren: listChildren,
	}
}

// Start launches worker goroutines.
func (q *SemanticQueue) Start() {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}
	log.Printf("[SemanticQueue] started %d workers (buffer=%d)", q.workers, cap(q.msgs))
}

// Stop gracefully shuts down.
func (q *SemanticQueue) Stop() {
	close(q.stopCh)
	close(q.msgs)
	q.wg.Wait()
	log.Printf("[SemanticQueue] stopped (completed=%d, failed=%d)", q.completed, q.failed)
}

// Enqueue adds a semantic message to the queue.
func (q *SemanticQueue) Enqueue(msg SemanticMsg) error {
	select {
	case q.msgs <- msg:
		return nil
	default:
		return fmt.Errorf("semantic queue full (capacity=%d)", cap(q.msgs))
	}
}

// SemanticStats returns queue statistics.
func (q *SemanticQueue) SemanticStats() Stats {
	return Stats{
		Pending:   len(q.msgs),
		Running:   int(atomic.LoadInt32(&q.running)),
		Completed: atomic.LoadInt64(&q.completed),
		Failed:    atomic.LoadInt64(&q.failed),
	}
}

func (q *SemanticQueue) worker(id int) {
	defer q.wg.Done()
	for {
		select {
		case <-q.stopCh:
			for msg := range q.msgs {
				q.processMsg(id, msg)
			}
			return
		case msg, ok := <-q.msgs:
			if !ok {
				return
			}
			q.processMsg(id, msg)
		}
	}
}

func (q *SemanticQueue) processMsg(workerID int, msg SemanticMsg) {
	atomic.AddInt32(&q.running, 1)
	defer atomic.AddInt32(&q.running, -1)

	start := time.Now()
	reqCtx := &ctx.RequestContext{}

	dag := NewSemanticDagExecutor(
		msg.ContextType,
		reqCtx,
		msg.Recursive,
		10,
		q.summarize,
		q.listChildren,
	)

	if err := dag.Execute(msg.URI); err != nil {
		atomic.AddInt64(&q.failed, 1)
		log.Printf("[SemanticQueue] worker-%d msg %s FAILED (%s): %v",
			workerID, msg.URI, time.Since(start), err)
		return
	}

	stats := dag.Stats()
	atomic.AddInt64(&q.completed, 1)
	log.Printf("[SemanticQueue] worker-%d msg %s OK (nodes=%d, %s)",
		workerID, msg.URI, stats.DoneNodes, time.Since(start))
}
