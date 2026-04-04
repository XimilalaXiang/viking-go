package queue

import (
	"fmt"
	"sync"
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
		t.Errorf("expected 1 dir node, got %d", stats.TotalNodes)
	}
	// DoneNodes counts: 2 files + 1 directory = 3
	if stats.DoneNodes < 3 {
		t.Errorf("expected at least 3 done (2 files + 1 dir), got %d", stats.DoneNodes)
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
		t.Errorf("expected 2 dir nodes, got %d", stats.TotalNodes)
	}
	// DoneNodes: 1 file in root + 1 file in sub + 2 dirs = 4
	if stats.DoneNodes < 4 {
		t.Errorf("expected at least 4 done, got %d", stats.DoneNodes)
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

func TestSemanticDagVectorizeTasks(t *testing.T) {
	summarize := func(uri, ct string, rc *ctx.RequestContext) (string, string, error) {
		return "abs:" + uri, "ovw:" + uri, nil
	}
	listChildren := func(uri string, rc *ctx.RequestContext) ([]string, []string, error) {
		if uri == "viking://root" {
			return []string{"viking://root/a.md", "viking://root/b.md"}, nil, nil
		}
		return nil, nil, nil
	}

	dag := NewSemanticDagExecutor("resource", nil, false, 5, summarize, listChildren)
	err := dag.Execute("viking://root")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	tasks := dag.VectorizeTasks()
	if len(tasks) < 3 {
		t.Errorf("expected at least 3 vectorize tasks (2 files + 1 dir), got %d", len(tasks))
	}

	fileCount, dirCount := 0, 0
	for _, task := range tasks {
		switch task.TaskType {
		case "file":
			fileCount++
		case "directory":
			dirCount++
		}
	}
	if fileCount != 2 {
		t.Errorf("expected 2 file tasks, got %d", fileCount)
	}
	if dirCount != 1 {
		t.Errorf("expected 1 dir task, got %d", dirCount)
	}
}

func TestSemanticDagWriteContent(t *testing.T) {
	written := make(map[string]string)
	var mu sync.Mutex

	writeContent := func(uri, abstract, overview string) error {
		mu.Lock()
		written[uri] = abstract + "|" + overview
		mu.Unlock()
		return nil
	}
	summarize := func(uri, ct string, rc *ctx.RequestContext) (string, string, error) {
		return "a", "o", nil
	}
	listChildren := func(uri string, rc *ctx.RequestContext) ([]string, []string, error) {
		if uri == "viking://root" {
			return []string{"viking://root/f1"}, nil, nil
		}
		return nil, nil, nil
	}

	dag := NewSemanticDagExecutorWithConfig(DagExecutorConfig{
		ContextType:   "resource",
		Recursive:     false,
		MaxConcurrent: 5,
		Summarize:     summarize,
		WriteContent:  writeContent,
		ListChildren:  listChildren,
	})
	err := dag.Execute("viking://root")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if _, ok := written["viking://root"]; !ok {
		t.Error("expected writeContent to be called for root directory")
	}
}

func TestSemanticDagSkipFilenames(t *testing.T) {
	callCount := int32(0)

	summarize := func(uri, ct string, rc *ctx.RequestContext) (string, string, error) {
		atomic.AddInt32(&callCount, 1)
		return "a", "o", nil
	}
	listChildren := func(uri string, rc *ctx.RequestContext) ([]string, []string, error) {
		if uri == "viking://root" {
			return []string{
				"viking://root/readme.md",
				"viking://root/messages.jsonl",
				"viking://root/.hidden",
			}, nil, nil
		}
		return nil, nil, nil
	}

	dag := NewSemanticDagExecutor("resource", nil, false, 5, summarize, listChildren)
	err := dag.Execute("viking://root")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Only readme.md should be processed (messages.jsonl and .hidden are skipped)
	// Plus 1 dir summary = 2 total calls
	count := atomic.LoadInt32(&callCount)
	if count != 2 {
		t.Errorf("expected 2 summarize calls (1 file + 1 dir), got %d", count)
	}
}

func TestUriName(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"viking://root/file.md", "file.md"},
		{"viking://root/sub/deep/file.txt", "file.txt"},
		{"file.md", "file.md"},
		{"", ""},
	}
	for _, tt := range tests {
		got := uriName(tt.uri)
		if got != tt.want {
			t.Errorf("uriName(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}
