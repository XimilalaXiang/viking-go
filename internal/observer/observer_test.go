package observer

import (
	"testing"
	"time"
)

func TestRegistryEmpty(t *testing.T) {
	r := NewRegistry()
	if !r.IsHealthy() {
		t.Error("empty registry should be healthy")
	}
	if len(r.Names()) != 0 {
		t.Error("expected no names")
	}
}

func TestRegistrySystemStatus(t *testing.T) {
	r := NewRegistry()

	qo := NewQueueObserver(func() map[string]any {
		return map[string]any{"pending": 5, "completed": int64(100), "error_count": int64(0)}
	})
	r.Register(qo)

	so := NewStorageObserver(func() (bool, map[string]any) {
		return true, map[string]any{"vector_count": 1000}
	})
	r.Register(so)

	if !r.IsHealthy() {
		t.Error("all observers healthy, registry should be healthy")
	}

	status := r.SystemStatus()
	if status["status"] != "ok" {
		t.Errorf("status = %v, want ok", status["status"])
	}
	comps, _ := status["components"].(map[string]any)
	if len(comps) != 2 {
		t.Errorf("expected 2 components, got %d", len(comps))
	}
}

func TestRegistryDegraded(t *testing.T) {
	r := NewRegistry()
	r.Register(NewStorageObserver(func() (bool, map[string]any) {
		return false, map[string]any{"error": "connection refused"}
	}))

	if r.IsHealthy() {
		t.Error("should be unhealthy")
	}
	status := r.SystemStatus()
	if status["status"] != "degraded" {
		t.Errorf("status = %v, want degraded", status["status"])
	}
}

func TestQueueObserver(t *testing.T) {
	o := NewQueueObserver(func() map[string]any {
		return map[string]any{
			"pending":     10,
			"completed":   int64(50),
			"error_count": int64(0),
		}
	})
	if o.Name() != "queue" {
		t.Error("wrong name")
	}
	if !o.IsHealthy() {
		t.Error("should be healthy with 0 errors")
	}
	if o.HasErrors() {
		t.Error("should have no errors")
	}

	table := o.StatusTable()
	if table == "" {
		t.Error("empty status table")
	}

	j := o.StatusJSON()
	if j["pending"] != 10 {
		t.Errorf("pending = %v", j["pending"])
	}
}

func TestQueueObserverErrors(t *testing.T) {
	o := NewQueueObserver(func() map[string]any {
		return map[string]any{"error_count": int64(5)}
	})
	if o.IsHealthy() {
		t.Error("should be unhealthy with errors")
	}
	if !o.HasErrors() {
		t.Error("should have errors")
	}
}

func TestStorageObserver(t *testing.T) {
	o := NewStorageObserver(func() (bool, map[string]any) {
		return true, map[string]any{"vector_count": 500}
	})
	if !o.IsHealthy() {
		t.Error("should be healthy")
	}
	j := o.StatusJSON()
	if j["vector_count"] != 500 {
		t.Error("wrong vector_count")
	}
}

func TestStorageObserverUnhealthy(t *testing.T) {
	o := NewStorageObserver(func() (bool, map[string]any) {
		return false, nil
	})
	if o.IsHealthy() {
		t.Error("should be unhealthy")
	}
	if !o.HasErrors() {
		t.Error("should have errors")
	}
}

func TestModelsObserver(t *testing.T) {
	o := NewModelsObserver()
	if o.IsHealthy() {
		t.Error("no models set, should be unhealthy")
	}

	o.SetLLMStatus("gpt-4o", true)
	if !o.IsHealthy() {
		t.Error("should be healthy with LLM available")
	}

	o.SetEmbeddingStatus("text-embedding-3-small", true)
	o.SetRerankStatus("rerank-v1", false)

	j := o.StatusJSON()
	llm, _ := j["llm"].(map[string]any)
	if llm["model"] != "gpt-4o" {
		t.Errorf("llm model = %v", llm["model"])
	}
	if llm["available"] != true {
		t.Error("llm should be available")
	}

	table := o.StatusTable()
	if table == "" {
		t.Error("empty status table")
	}
}

func TestLockObserver(t *testing.T) {
	o := NewLockObserver(func() map[string]any {
		return map[string]any{
			"active_handles": 3,
			"active_locks":   10,
			"lock_expire":    "5m0s",
		}
	}, 10*time.Minute)

	if !o.IsHealthy() {
		t.Error("3 handles should be healthy")
	}

	j := o.StatusJSON()
	if j["active_handles"] != 3 {
		t.Errorf("handles = %v", j["active_handles"])
	}
}

func TestLockObserverTooMany(t *testing.T) {
	o := NewLockObserver(func() map[string]any {
		return map[string]any{"active_handles": 200, "active_locks": 500, "lock_expire": "5m"}
	}, 10*time.Minute)

	if o.IsHealthy() {
		t.Error("200 handles should be unhealthy")
	}
}

func TestRetrievalObserver(t *testing.T) {
	o := NewRetrievalObserver(func() map[string]any {
		return map[string]any{
			"total_queries":    int64(100),
			"total_results":    int64(500),
			"zero_result_rate": 0.1,
			"avg_score":        0.85,
		}
	})

	if !o.IsHealthy() {
		t.Error("10% zero-result rate should be healthy")
	}
	if o.HasErrors() {
		t.Error("should have no errors")
	}
}

func TestRetrievalObserverHighZeroRate(t *testing.T) {
	o := NewRetrievalObserver(func() map[string]any {
		return map[string]any{
			"total_queries":    int64(10),
			"zero_result_rate": 0.6,
		}
	})

	if o.IsHealthy() {
		t.Error("60% zero-result rate should be unhealthy")
	}
	if !o.HasErrors() {
		t.Error("should have errors")
	}
}

func TestRetrievalObserverNoQueries(t *testing.T) {
	o := NewRetrievalObserver(func() map[string]any {
		return map[string]any{"total_queries": int64(0)}
	})
	if !o.IsHealthy() {
		t.Error("no queries should be healthy")
	}
	if o.HasErrors() {
		t.Error("should have no errors with < 5 queries")
	}
}

func TestRegistryGetAndNames(t *testing.T) {
	r := NewRegistry()
	r.Register(NewModelsObserver())
	r.Register(NewQueueObserver(func() map[string]any { return nil }))

	names := r.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}

	got := r.Get("models")
	if got == nil {
		t.Error("expected models observer")
	}
	if got.Name() != "models" {
		t.Error("wrong observer returned")
	}

	if r.Get("nonexistent") != nil {
		t.Error("expected nil for nonexistent")
	}
}
