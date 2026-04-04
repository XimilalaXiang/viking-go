package queue

import (
	"sync"
	"testing"
)

func TestNamedQueueEnqueueDequeue(t *testing.T) {
	q := NewNamedQueue(NamedQueueConfig{Name: "test", BufferSize: 10})

	msg := map[string]any{"key": "value"}
	if err := q.Enqueue(msg); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if q.Size() != 1 {
		t.Fatalf("size = %d, want 1", q.Size())
	}

	got, err := q.Dequeue()
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("got key = %v", got["key"])
	}
	if q.Size() != 0 {
		t.Errorf("size after dequeue = %d", q.Size())
	}
}

func TestNamedQueueEmptyDequeue(t *testing.T) {
	q := NewNamedQueue(NamedQueueConfig{Name: "test"})
	got, err := q.Dequeue()
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestNamedQueueFull(t *testing.T) {
	q := NewNamedQueue(NamedQueueConfig{Name: "test", BufferSize: 2})
	q.Enqueue(map[string]any{"a": 1})
	q.Enqueue(map[string]any{"b": 2})
	err := q.Enqueue(map[string]any{"c": 3})
	if err == nil {
		t.Error("expected error when queue is full")
	}
}

func TestNamedQueuePeek(t *testing.T) {
	q := NewNamedQueue(NamedQueueConfig{Name: "test", BufferSize: 10})
	q.Enqueue(map[string]any{"x": 1})
	msg := q.Peek()
	if msg == nil {
		t.Fatal("peek returned nil")
	}
	if msg["x"] != float64(1) && msg["x"] != 1 {
		t.Errorf("peek got %v", msg["x"])
	}
}

func TestNamedQueueClear(t *testing.T) {
	q := NewNamedQueue(NamedQueueConfig{Name: "test", BufferSize: 10})
	for i := 0; i < 5; i++ {
		q.Enqueue(map[string]any{"i": i})
	}
	q.Clear()
	if q.Size() != 0 {
		t.Errorf("size after clear = %d", q.Size())
	}
}

type echoHandler struct{}

func (h *echoHandler) OnDequeue(data map[string]any) (map[string]any, error) {
	data["processed"] = true
	return data, nil
}

func TestNamedQueueWithHandler(t *testing.T) {
	q := NewNamedQueue(NamedQueueConfig{
		Name:           "test",
		BufferSize:     10,
		DequeueHandler: &echoHandler{},
	})

	q.Enqueue(map[string]any{"val": 1})
	got, err := q.Dequeue()
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if got["processed"] != true {
		t.Errorf("handler not invoked")
	}

	st := q.Status()
	if st.Processed != 1 {
		t.Errorf("processed = %d", st.Processed)
	}
}

func TestNamedQueueStatus(t *testing.T) {
	q := NewNamedQueue(NamedQueueConfig{Name: "test", BufferSize: 10})
	q.Enqueue(map[string]any{})
	q.Enqueue(map[string]any{})

	st := q.Status()
	if st.Pending != 2 {
		t.Errorf("pending = %d", st.Pending)
	}
	if st.ErrorCount != 0 {
		t.Errorf("errors = %d", st.ErrorCount)
	}

	q.OnProcessError("test error", nil)
	st = q.Status()
	if st.ErrorCount != 1 {
		t.Errorf("error count = %d", st.ErrorCount)
	}
	if !st.HasErrors() {
		t.Error("HasErrors() should be true")
	}
}

func TestNamedQueueResetStatus(t *testing.T) {
	q := NewNamedQueue(NamedQueueConfig{Name: "test"})
	q.OnProcessError("err", nil)
	q.ResetStatus()
	st := q.Status()
	if st.ErrorCount != 0 || st.Processed != 0 {
		t.Error("status not reset")
	}
}

func TestNamedQueueEnqueueJSON(t *testing.T) {
	q := NewNamedQueue(NamedQueueConfig{Name: "test", BufferSize: 10})
	type Msg struct {
		URI  string `json:"uri"`
		Type string `json:"type"`
	}
	err := q.EnqueueJSON(Msg{URI: "/data", Type: "resource"})
	if err != nil {
		t.Fatalf("enqueue json: %v", err)
	}
	got, _ := q.Dequeue()
	if got["uri"] != "/data" || got["type"] != "resource" {
		t.Errorf("unexpected: %v", got)
	}
}

func TestNamedQueueConcurrent(t *testing.T) {
	q := NewNamedQueue(NamedQueueConfig{Name: "test", BufferSize: 1000})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			q.Enqueue(map[string]any{"n": n})
		}(i)
	}
	wg.Wait()

	if q.Size() != 100 {
		t.Errorf("size = %d, want 100", q.Size())
	}
}

type hookDoubler struct{}

func (h *hookDoubler) OnEnqueue(data map[string]any) (map[string]any, error) {
	if v, ok := data["val"].(int); ok {
		data["val"] = v * 2
	}
	return data, nil
}

func TestNamedQueueWithEnqueueHook(t *testing.T) {
	q := NewNamedQueue(NamedQueueConfig{
		Name:        "test",
		BufferSize:  10,
		EnqueueHook: &hookDoubler{},
	})
	q.Enqueue(map[string]any{"val": 5})
	got, _ := q.Dequeue()
	if got["val"] != 10 {
		t.Errorf("val = %v, want 10", got["val"])
	}
}
