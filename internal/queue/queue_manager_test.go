package queue

import (
	"testing"
	"time"
)

func TestQueueManagerGetQueueCreate(t *testing.T) {
	m := NewQueueManager(QueueManagerConfig{})
	q, err := m.GetQueue("test", nil, true)
	if err != nil {
		t.Fatalf("get queue: %v", err)
	}
	if q.Name != "test" {
		t.Errorf("name = %s", q.Name)
	}

	q2, err := m.GetQueue("test", nil, false)
	if err != nil {
		t.Fatal("second get failed")
	}
	if q2 != q {
		t.Error("expected same queue pointer")
	}
}

func TestQueueManagerGetQueueNotFound(t *testing.T) {
	m := NewQueueManager(QueueManagerConfig{})
	_, err := m.GetQueue("missing", nil, false)
	if err == nil {
		t.Error("expected error for missing queue")
	}
	if _, ok := err.(*QueueNotFoundError); !ok {
		t.Errorf("wrong error type: %T", err)
	}
}

func TestQueueManagerEnqueue(t *testing.T) {
	m := NewQueueManager(QueueManagerConfig{})
	m.GetQueue("q1", nil, true)

	err := m.Enqueue("q1", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	size, _ := m.Size("q1")
	if size != 1 {
		t.Errorf("size = %d", size)
	}
}

func TestQueueManagerEnqueueNotFound(t *testing.T) {
	m := NewQueueManager(QueueManagerConfig{})
	err := m.Enqueue("missing", map[string]any{})
	if err == nil {
		t.Error("expected error")
	}
}

func TestQueueManagerCheckStatus(t *testing.T) {
	m := NewQueueManager(QueueManagerConfig{})
	m.GetQueue("a", nil, true)
	m.GetQueue("b", nil, true)

	m.Enqueue("a", map[string]any{})

	statuses := m.CheckStatus("")
	if len(statuses) != 2 {
		t.Fatalf("statuses = %d", len(statuses))
	}
	if statuses["a"].Pending != 1 {
		t.Errorf("a pending = %d", statuses["a"].Pending)
	}
	if statuses["b"].Pending != 0 {
		t.Errorf("b pending = %d", statuses["b"].Pending)
	}

	single := m.CheckStatus("a")
	if len(single) != 1 {
		t.Errorf("single statuses = %d", len(single))
	}
}

func TestQueueManagerIsAllComplete(t *testing.T) {
	m := NewQueueManager(QueueManagerConfig{})
	m.GetQueue("q", nil, true)

	if !m.IsAllComplete("") {
		t.Error("expected all complete when empty")
	}

	m.Enqueue("q", map[string]any{})
	if m.IsAllComplete("") {
		t.Error("should not be complete with pending items")
	}
}

func TestQueueManagerHasErrors(t *testing.T) {
	m := NewQueueManager(QueueManagerConfig{})
	q, _ := m.GetQueue("q", nil, true)

	if m.HasErrors("") {
		t.Error("no errors expected initially")
	}

	q.OnProcessError("test", nil)
	if !m.HasErrors("q") {
		t.Error("expected error on q")
	}
	if !m.HasErrors("") {
		t.Error("expected global errors")
	}
}

func TestQueueManagerStartStop(t *testing.T) {
	m := NewQueueManager(QueueManagerConfig{
		PollInterval: 50 * time.Millisecond,
	})
	m.GetQueue("q", nil, true)
	m.Start()
	if !m.IsRunning() {
		t.Error("should be running")
	}
	m.Stop()
	if m.IsRunning() {
		t.Error("should be stopped")
	}
}

func TestQueueManagerQueueNames(t *testing.T) {
	m := NewQueueManager(QueueManagerConfig{})
	m.GetQueue("alpha", nil, true)
	m.GetQueue("beta", nil, true)

	names := m.QueueNames()
	if len(names) != 2 {
		t.Errorf("names = %d", len(names))
	}
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["alpha"] || !nameSet["beta"] {
		t.Errorf("unexpected names: %v", names)
	}
}
