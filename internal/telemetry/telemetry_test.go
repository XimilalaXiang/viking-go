package telemetry

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNopTelemetry(t *testing.T) {
	nop := Nop()
	nop.Inc("test")
	nop.Count("test", 5)
	nop.Set("gauge", 42)
	nop.AddDuration("stage", 100)
	nop.AddTokenUsage("llm", 100, 50)
	nop.SetError("stage", "ERR", "msg")
	done := nop.Measure("op")
	done()

	snap := nop.Finish("ok")
	if snap != nil {
		t.Error("expected nil snapshot from disabled telemetry")
	}
}

func TestEnabledTelemetry(t *testing.T) {
	tm := New("test_op", true)
	if tm.TelemetryID == "" {
		t.Error("expected non-empty telemetry ID")
	}
	if !strings.HasPrefix(tm.TelemetryID, "tm_") {
		t.Errorf("expected tm_ prefix, got %q", tm.TelemetryID)
	}

	tm.Inc("requests")
	tm.Inc("requests")
	tm.Count("bytes", 1024)

	if tm.GetCounter("requests") != 2 {
		t.Errorf("expected 2, got %f", tm.GetCounter("requests"))
	}

	tm.Set("status", "running")
	if tm.GetGauge("status") != "running" {
		t.Errorf("unexpected gauge value")
	}

	snap := tm.Finish("ok")
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if snap.Summary["operation"] != "test_op" {
		t.Error("wrong operation")
	}
	if snap.Summary["status"] != "ok" {
		t.Error("wrong status")
	}
	dur, ok := snap.Summary["duration_ms"].(float64)
	if !ok || dur < 0 {
		t.Error("expected non-negative duration")
	}
}

func TestTokenUsage(t *testing.T) {
	tm := New("token_test", true)
	tm.AddTokenUsage("llm", 100, 50)
	tm.AddTokenUsage("embedding", 200, 0)

	snap := tm.Finish("ok")
	tokens, ok := snap.Summary["tokens"].(map[string]any)
	if !ok {
		t.Fatal("expected tokens in summary")
	}
	total, _ := tokens["total"].(int64)
	if total != 350 {
		t.Errorf("expected 350 total tokens, got %d", total)
	}
	llm, _ := tokens["llm"].(map[string]any)
	if llm == nil {
		t.Fatal("expected llm token breakdown")
	}
	if llmTotal, _ := llm["total"].(int64); llmTotal != 150 {
		t.Errorf("expected 150 LLM tokens, got %d", llmTotal)
	}
}

func TestMeasure(t *testing.T) {
	tm := New("measure_test", true)
	done := tm.Measure("stage_a")
	time.Sleep(5 * time.Millisecond)
	done()

	val := tm.GetGauge("stage_a.duration_ms")
	if val == nil {
		t.Fatal("expected duration gauge")
	}
	dur, ok := val.(float64)
	if !ok || dur < 1 {
		t.Errorf("expected >1ms duration, got %v", val)
	}
}

func TestSetError(t *testing.T) {
	tm := New("error_test", true)
	tm.SetError("parse", "INVALID_INPUT", "bad json")

	snap := tm.Finish("error")
	errs, ok := snap.Summary["errors"].(map[string]any)
	if !ok {
		t.Fatal("expected errors in summary")
	}
	if errs["stage"] != "parse" {
		t.Error("wrong error stage")
	}
	if errs["error_code"] != "INVALID_INPUT" {
		t.Error("wrong error code")
	}
}

func TestAddDuration(t *testing.T) {
	tm := New("dur_test", true)
	tm.AddDuration("process", 100.5)
	tm.AddDuration("process", 50.3)

	val := tm.GetGauge("process.duration_ms")
	dur, ok := val.(float64)
	if !ok {
		t.Fatal("expected float64 gauge")
	}
	if dur < 150 || dur > 151 {
		t.Errorf("expected ~150.8, got %f", dur)
	}
}

func TestVectorMetrics(t *testing.T) {
	tm := New("vector_test", true)
	tm.Inc("vector.searches")
	tm.Count("vector.scored", 50)
	tm.Count("vector.passed", 10)

	snap := tm.Finish("ok")
	vec, ok := snap.Summary["vector"].(map[string]any)
	if !ok {
		t.Fatal("expected vector metrics in summary")
	}
	if vec["searches"] != int64(1) {
		t.Errorf("expected 1 search, got %v", vec["searches"])
	}
	if vec["scored"] != int64(50) {
		t.Errorf("expected 50 scored, got %v", vec["scored"])
	}
}

func TestRegistry(t *testing.T) {
	tm := New("reg_test", true)
	Register(tm)

	got := Resolve(tm.TelemetryID)
	if got != tm {
		t.Error("expected to resolve registered telemetry")
	}

	if ActiveCount() < 1 {
		t.Error("expected at least 1 active telemetry")
	}

	Unregister(tm.TelemetryID)
	if Resolve(tm.TelemetryID) != nil {
		t.Error("expected nil after unregister")
	}
}

func TestRegistryNopSkipped(t *testing.T) {
	nop := Nop()
	Register(nop)
	if ActiveCount() != 0 {
		t.Error("nop should not be registered")
	}
}

func TestConcurrentAccess(t *testing.T) {
	tm := New("concurrent_test", true)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tm.Inc("requests")
			tm.Count("bytes", 10)
			tm.Set("active", true)
			tm.AddDuration("op", 1.0)
			tm.AddTokenUsage("llm", 1, 1)
		}()
	}
	wg.Wait()

	if tm.GetCounter("requests") != 100 {
		t.Errorf("expected 100, got %f", tm.GetCounter("requests"))
	}

	snap := tm.Finish("ok")
	if snap == nil {
		t.Fatal("expected snapshot")
	}
}

func TestSnapshotToUsageDict(t *testing.T) {
	tm := New("usage_test", true)
	tm.AddTokenUsage("llm", 100, 50)
	snap := tm.Finish("ok")

	usage := snap.ToUsageDict()
	if usage["token_total"] != int64(150) {
		t.Errorf("expected 150 tokens, got %v", usage["token_total"])
	}
}

func TestMemoryMetrics(t *testing.T) {
	tm := New("memory_test", true)
	tm.Set("memory.extracted", 5)
	tm.Set("memory.extract.total.duration_ms", 250.0)
	tm.Set("memory.extract.created", int64(3))
	tm.Set("memory.extract.merged", int64(1))
	tm.Set("memory.extract.deleted", int64(0))
	tm.Set("memory.extract.skipped", int64(1))

	snap := tm.Finish("ok")
	mem, ok := snap.Summary["memory"].(map[string]any)
	if !ok {
		t.Fatal("expected memory section")
	}
	if mem["extracted"] != 5 {
		t.Errorf("expected 5 extracted, got %v", mem["extracted"])
	}
	extract, ok := mem["extract"].(map[string]any)
	if !ok {
		t.Fatal("expected extract subsection")
	}
	if extract["duration_ms"] != 250.0 {
		t.Errorf("expected 250ms, got %v", extract["duration_ms"])
	}
}

func init() {
	registeredLock.Lock()
	registered = make(map[string]*OperationTelemetry)
	registeredLock.Unlock()
}
