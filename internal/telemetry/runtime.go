package telemetry

import (
	"sort"
	"sync"
)

// MemoryTelemetryMeter is a lightweight in-process meter for global telemetry
// hook points. It supports counters, gauges, and histograms with attribute tags.
type MemoryTelemetryMeter struct {
	mu         sync.Mutex
	counters   map[attrKey]float64
	gauges     map[attrKey]any
	histograms map[attrKey][]float64
}

type attrKey struct {
	metric string
	attrs  string // sorted key=value pairs
}

func makeAttrKey(metric string, attrs map[string]any) attrKey {
	if len(attrs) == 0 {
		return attrKey{metric: metric}
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var s string
	for i, k := range keys {
		if i > 0 {
			s += ","
		}
		s += k + "="
		switch v := attrs[k].(type) {
		case string:
			s += v
		default:
			s += "?"
		}
	}
	return attrKey{metric: metric, attrs: s}
}

// NewMemoryTelemetryMeter creates a new meter.
func NewMemoryTelemetryMeter() *MemoryTelemetryMeter {
	return &MemoryTelemetryMeter{
		counters:   make(map[attrKey]float64),
		gauges:     make(map[attrKey]any),
		histograms: make(map[attrKey][]float64),
	}
}

// Increment adds a value to a counter.
func (m *MemoryTelemetryMeter) Increment(metric string, value float64, attrs map[string]any) {
	key := makeAttrKey(metric, attrs)
	m.mu.Lock()
	m.counters[key] += value
	m.mu.Unlock()
}

// SetGauge sets a gauge value.
func (m *MemoryTelemetryMeter) SetGauge(metric string, value any, attrs map[string]any) {
	key := makeAttrKey(metric, attrs)
	m.mu.Lock()
	m.gauges[key] = value
	m.mu.Unlock()
}

// RecordHistogram appends a value to a histogram.
func (m *MemoryTelemetryMeter) RecordHistogram(metric string, value float64, attrs map[string]any) {
	key := makeAttrKey(metric, attrs)
	m.mu.Lock()
	m.histograms[key] = append(m.histograms[key], value)
	m.mu.Unlock()
}

// GetCounter returns a counter value.
func (m *MemoryTelemetryMeter) GetCounter(metric string, attrs map[string]any) float64 {
	key := makeAttrKey(metric, attrs)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[key]
}

// GetGauge returns a gauge value.
func (m *MemoryTelemetryMeter) GetGauge(metric string, attrs map[string]any) any {
	key := makeAttrKey(metric, attrs)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gauges[key]
}

// GetHistogram returns a copy of the histogram values.
func (m *MemoryTelemetryMeter) GetHistogram(metric string, attrs map[string]any) []float64 {
	key := makeAttrKey(metric, attrs)
	m.mu.Lock()
	defer m.mu.Unlock()
	src := m.histograms[key]
	if src == nil {
		return nil
	}
	out := make([]float64, len(src))
	copy(out, src)
	return out
}

// TelemetryRuntime holds the global meter singleton.
type TelemetryRuntime struct {
	meter *MemoryTelemetryMeter
}

// Meter returns the global meter.
func (r *TelemetryRuntime) Meter() *MemoryTelemetryMeter {
	return r.meter
}

var globalRuntime = &TelemetryRuntime{
	meter: NewMemoryTelemetryMeter(),
}

// GetRuntime returns the global telemetry runtime.
func GetRuntime() *TelemetryRuntime {
	return globalRuntime
}

// SetRuntime replaces the global telemetry runtime.
func SetRuntime(r *TelemetryRuntime) {
	globalRuntime = r
}

// NewRuntime creates a new TelemetryRuntime with a fresh meter.
func NewRuntime() *TelemetryRuntime {
	return &TelemetryRuntime{meter: NewMemoryTelemetryMeter()}
}
