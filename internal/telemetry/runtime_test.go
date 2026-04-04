package telemetry

import (
	"sync"
	"testing"
)

func TestMemoryTelemetryMeter_Counter(t *testing.T) {
	m := NewMemoryTelemetryMeter()
	m.Increment("requests", 1, nil)
	m.Increment("requests", 1, nil)
	m.Increment("requests", 3, nil)

	if got := m.GetCounter("requests", nil); got != 5 {
		t.Errorf("counter = %f, want 5", got)
	}
}

func TestMemoryTelemetryMeter_CounterWithAttrs(t *testing.T) {
	m := NewMemoryTelemetryMeter()
	m.Increment("requests", 1, map[string]any{"method": "GET"})
	m.Increment("requests", 1, map[string]any{"method": "POST"})
	m.Increment("requests", 2, map[string]any{"method": "GET"})

	if got := m.GetCounter("requests", map[string]any{"method": "GET"}); got != 3 {
		t.Errorf("GET counter = %f, want 3", got)
	}
	if got := m.GetCounter("requests", map[string]any{"method": "POST"}); got != 1 {
		t.Errorf("POST counter = %f, want 1", got)
	}
}

func TestMemoryTelemetryMeter_Gauge(t *testing.T) {
	m := NewMemoryTelemetryMeter()
	m.SetGauge("active_connections", 42, nil)

	if got := m.GetGauge("active_connections", nil); got != 42 {
		t.Errorf("gauge = %v, want 42", got)
	}

	m.SetGauge("active_connections", 10, nil)
	if got := m.GetGauge("active_connections", nil); got != 10 {
		t.Errorf("gauge = %v, want 10 after overwrite", got)
	}
}

func TestMemoryTelemetryMeter_Histogram(t *testing.T) {
	m := NewMemoryTelemetryMeter()
	m.RecordHistogram("latency_ms", 100, nil)
	m.RecordHistogram("latency_ms", 200, nil)
	m.RecordHistogram("latency_ms", 150, nil)

	vals := m.GetHistogram("latency_ms", nil)
	if len(vals) != 3 {
		t.Fatalf("histogram len = %d, want 3", len(vals))
	}
	if vals[0] != 100 || vals[1] != 200 || vals[2] != 150 {
		t.Errorf("histogram = %v", vals)
	}
}

func TestMemoryTelemetryMeter_HistogramNil(t *testing.T) {
	m := NewMemoryTelemetryMeter()
	vals := m.GetHistogram("nonexistent", nil)
	if vals != nil {
		t.Error("expected nil for nonexistent histogram")
	}
}

func TestMemoryTelemetryMeter_Concurrent(t *testing.T) {
	m := NewMemoryTelemetryMeter()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Increment("requests", 1, nil)
			m.SetGauge("status", "ok", nil)
			m.RecordHistogram("latency", 1.0, nil)
		}()
	}
	wg.Wait()

	if got := m.GetCounter("requests", nil); got != 100 {
		t.Errorf("counter = %f, want 100", got)
	}
}

func TestTelemetryRuntime(t *testing.T) {
	r := NewRuntime()
	m := r.Meter()
	if m == nil {
		t.Fatal("meter should not be nil")
	}

	m.Increment("test", 1, nil)
	if got := m.GetCounter("test", nil); got != 1 {
		t.Errorf("counter = %f", got)
	}
}

func TestGlobalRuntime(t *testing.T) {
	original := GetRuntime()

	newRT := NewRuntime()
	SetRuntime(newRT)

	if GetRuntime() != newRT {
		t.Error("SetRuntime didn't work")
	}

	SetRuntime(original)
}
