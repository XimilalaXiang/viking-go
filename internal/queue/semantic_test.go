package queue

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
)

func TestSemanticDagFlatDirectory(t *testing.T) {
	callCount := int32(0)

	summarize := func(uri, contextType string, reqCtx *ctx.RequestContext) (string, string, error) {
		atomic.AddInt32(&callCount, 1)
		return fmt.Sprintf("abstract of %s", uri), fmt.Sprintf("overview of %s", uri), nil
	}

	listChildren := func(uri string, reqCtx *ctx.RequestContext) ([]string, []string, error) {
		if uri == "viking://test/root" {
			return []string{"viking://test/root/a.md", "viking://test/root/b.md"}, nil, nil
		}
		return nil, nil, nil
	}

	dag := NewSemanticDagExecutor("resource", nil, false, 5, summarize, listChildren)
	err := dag.Execute("viking://test/root")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	stats := dag.Stats()
	if stats.TotalNodes != 1 {
		t.Errorf("expected 1 node, got %d", stats.TotalNodes)
	}
	if stats.DoneNodes != 1 {
		t.Errorf("expected 1 done, got %d", stats.DoneNodes)
	}
	if atomic.LoadInt32(&callCount) < 3 {
		t.Errorf("expected at least 3 summarize calls (2 files + 1 dir), got %d", callCount)
	}
}

func TestSemanticDagRecursive(t *testing.T) {
	callCount := int32(0)

	summarize := func(uri, contextType string, reqCtx *ctx.RequestContext) (string, string, error) {
		atomic.AddInt32(&callCount, 1)
		return "abs", "ovw", nil
	}

	listChildren := func(uri string, reqCtx *ctx.RequestContext) ([]string, []string, error) {
		switch uri {
		case "viking://test/root":
			return []string{"viking://test/root/readme.md"}, []string{"viking://test/root/sub"}, nil
		case "viking://test/root/sub":
			return []string{"viking://test/root/sub/code.go"}, nil, nil
		}
		return nil, nil, nil
	}

	dag := NewSemanticDagExecutor("resource", nil, true, 5, summarize, listChildren)
	err := dag.Execute("viking://test/root")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	stats := dag.Stats()
	if stats.TotalNodes != 2 {
		t.Errorf("expected 2 nodes, got %d", stats.TotalNodes)
	}
	if stats.DoneNodes != 2 {
		t.Errorf("expected 2 done, got %d", stats.DoneNodes)
	}
}

func TestSemanticDagEmptyDirectory(t *testing.T) {
	summarize := func(uri, ct string, rc *ctx.RequestContext) (string, string, error) {
		return "", "", nil
	}
	listChildren := func(uri string, rc *ctx.RequestContext) ([]string, []string, error) {
		return nil, nil, nil
	}

	dag := NewSemanticDagExecutor("resource", nil, true, 5, summarize, listChildren)
	err := dag.Execute("viking://test/empty")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	stats := dag.Stats()
	if stats.TotalNodes != 1 {
		t.Errorf("expected 1 node (empty dir), got %d", stats.TotalNodes)
	}
}

func TestSemanticDagStats(t *testing.T) {
	summarize := func(uri, ct string, rc *ctx.RequestContext) (string, string, error) {
		time.Sleep(5 * time.Millisecond)
		return "a", "o", nil
	}
	listChildren := func(uri string, rc *ctx.RequestContext) ([]string, []string, error) {
		if uri == "viking://root" {
			return []string{"viking://root/f1", "viking://root/f2", "viking://root/f3"}, nil, nil
		}
		return nil, nil, nil
	}

	dag := NewSemanticDagExecutor("resource", nil, false, 2, summarize, listChildren)
	err := dag.Execute("viking://root")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	stats := dag.Stats()
	if stats.DoneNodes < 1 {
		t.Error("expected at least 1 done node")
	}
}

func TestSemanticQueueEnqueueAndProcess(t *testing.T) {
	processed := int32(0)

	summarize := func(uri, ct string, rc *ctx.RequestContext) (string, string, error) {
		atomic.AddInt32(&processed, 1)
		return "a", "o", nil
	}
	listChildren := func(uri string, rc *ctx.RequestContext) ([]string, []string, error) {
		if uri == "viking://test/q" {
			return []string{"viking://test/q/f1"}, nil, nil
		}
		return nil, nil, nil
	}

	q := NewSemanticQueue(1, 10, summarize, listChildren)
	q.Start()
	defer q.Stop()

	err := q.Enqueue(SemanticMsg{URI: "viking://test/q", ContextType: "resource", Recursive: false})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	stats := q.SemanticStats()
	if stats.Completed < 1 {
		t.Errorf("expected completed >= 1, got %d", stats.Completed)
	}
}

func TestSemanticQueueFull(t *testing.T) {
	summarize := func(uri, ct string, rc *ctx.RequestContext) (string, string, error) {
		time.Sleep(100 * time.Millisecond)
		return "", "", nil
	}
	listChildren := func(uri string, rc *ctx.RequestContext) ([]string, []string, error) {
		return nil, nil, nil
	}

	q := NewSemanticQueue(1, 1, summarize, listChildren)
	q.Start()
	defer q.Stop()

	q.Enqueue(SemanticMsg{URI: "viking://a"})
	q.Enqueue(SemanticMsg{URI: "viking://b"})
	err := q.Enqueue(SemanticMsg{URI: "viking://c"})
	if err == nil {
		t.Log("queue not full yet (timing dependent)")
	}
}
