// Package telemetry provides operation-scoped telemetry collection.
// Each operation (e.g., add_resource, find, session_commit) gets its own
// OperationTelemetry instance that collects counters, gauges, durations,
// and token usage. At the end, it produces a TelemetrySnapshot.
package telemetry

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TelemetrySnapshot is the final output of an operation's telemetry.
type TelemetrySnapshot struct {
	TelemetryID string         `json:"id"`
	Summary     map[string]any `json:"summary"`
}

// ToUsageDict returns a simplified usage dictionary.
func (s *TelemetrySnapshot) ToUsageDict() map[string]any {
	dur, _ := s.Summary["duration_ms"].(float64)
	var totalTokens int64
	if tokens, ok := s.Summary["tokens"].(map[string]any); ok {
		if t, ok := tokens["total"].(int64); ok {
			totalTokens = t
		}
	}
	return map[string]any{
		"duration_ms": dur,
		"token_total": totalTokens,
	}
}

// OperationTelemetry collects metrics for a single operation.
// When enabled=false, all methods are no-ops for zero overhead.
type OperationTelemetry struct {
	Operation   string
	Enabled     bool
	TelemetryID string

	startTime    time.Time
	counters     map[string]float64
	gauges       map[string]any
	errorStage   string
	errorCode    string
	errorMessage string
	mu           sync.Mutex
}

// New creates a new OperationTelemetry.
func New(operation string, enabled bool) *OperationTelemetry {
	id := ""
	if enabled {
		id = "tm_" + uuid.New().String()[:8]
	}
	return &OperationTelemetry{
		Operation:   operation,
		Enabled:     enabled,
		TelemetryID: id,
		startTime:   time.Now(),
		counters:    make(map[string]float64),
		gauges:      make(map[string]any),
	}
}

// Nop returns a disabled telemetry instance.
func Nop() *OperationTelemetry {
	return &OperationTelemetry{
		Enabled:  false,
		counters: make(map[string]float64),
		gauges:   make(map[string]any),
	}
}

// Count increments a named counter.
func (t *OperationTelemetry) Count(key string, delta float64) {
	if !t.Enabled {
		return
	}
	t.mu.Lock()
	t.counters[key] += delta
	t.mu.Unlock()
}

// Inc increments a counter by 1.
func (t *OperationTelemetry) Inc(key string) {
	t.Count(key, 1)
}

// Set sets a gauge value.
func (t *OperationTelemetry) Set(key string, value any) {
	if !t.Enabled {
		return
	}
	t.mu.Lock()
	t.gauges[key] = value
	t.mu.Unlock()
}

// AddDuration adds duration in ms to a gauge key.
func (t *OperationTelemetry) AddDuration(key string, durationMs float64) {
	if !t.Enabled {
		return
	}
	gaugeKey := key
	if len(key) < 12 || key[len(key)-12:] != ".duration_ms" {
		gaugeKey = key + ".duration_ms"
	}
	t.mu.Lock()
	existing, _ := t.gauges[gaugeKey].(float64)
	t.gauges[gaugeKey] = existing + durationMs
	t.mu.Unlock()
}

// Measure returns a function that records the elapsed time when called.
// Usage: done := t.Measure("stage"); defer done()
func (t *OperationTelemetry) Measure(key string) func() {
	if !t.Enabled {
		return func() {}
	}
	start := time.Now()
	return func() {
		t.AddDuration(key, float64(time.Since(start).Milliseconds()))
	}
}

// AddTokenUsage records token usage for a source (e.g., "llm", "embedding").
func (t *OperationTelemetry) AddTokenUsage(source string, input, output int) {
	if !t.Enabled {
		return
	}
	total := input + output
	t.Count("tokens.input", float64(input))
	t.Count("tokens.output", float64(output))
	t.Count("tokens.total", float64(total))
	t.Count(fmt.Sprintf("tokens.%s.input", source), float64(input))
	t.Count(fmt.Sprintf("tokens.%s.output", source), float64(output))
	t.Count(fmt.Sprintf("tokens.%s.total", source), float64(total))
}

// SetError records an error that occurred during the operation.
func (t *OperationTelemetry) SetError(stage, code, message string) {
	if !t.Enabled {
		return
	}
	t.mu.Lock()
	t.errorStage = stage
	t.errorCode = code
	t.errorMessage = message
	t.mu.Unlock()
}

// Finish finalizes the telemetry and produces a snapshot.
func (t *OperationTelemetry) Finish(status string) *TelemetrySnapshot {
	if !t.Enabled {
		return nil
	}

	durationMs := float64(time.Since(t.startTime).Milliseconds())

	t.mu.Lock()
	defer t.mu.Unlock()

	summary := buildSummary(
		t.Operation,
		status,
		durationMs,
		t.counters,
		t.gauges,
		t.errorStage,
		t.errorCode,
		t.errorMessage,
	)

	return &TelemetrySnapshot{
		TelemetryID: t.TelemetryID,
		Summary:     summary,
	}
}

// GetCounter returns the current value of a counter.
func (t *OperationTelemetry) GetCounter(key string) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.counters[key]
}

// GetGauge returns the current value of a gauge.
func (t *OperationTelemetry) GetGauge(key string) any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gauges[key]
}

func buildSummary(
	operation, status string,
	durationMs float64,
	counters map[string]float64,
	gauges map[string]any,
	errorStage, errorCode, errorMessage string,
) map[string]any {
	summary := map[string]any{
		"operation":   operation,
		"status":      status,
		"duration_ms": durationMs,
	}

	llmInput := int64(counters["tokens.llm.input"])
	llmOutput := int64(counters["tokens.llm.output"])
	llmTotal := int64(counters["tokens.llm.total"])
	embTotal := int64(counters["tokens.embedding.total"])
	totalTokens := int64(counters["tokens.total"])

	if totalTokens > 0 {
		summary["tokens"] = map[string]any{
			"total": totalTokens,
			"llm": map[string]any{
				"input":  llmInput,
				"output": llmOutput,
				"total":  llmTotal,
			},
			"embedding": map[string]any{
				"total": embTotal,
			},
		}
	}

	if hasPrefix("vector", counters, gauges) {
		summary["vector"] = map[string]any{
			"searches": int64(counters["vector.searches"]),
			"scored":   int64(counters["vector.scored"]),
			"passed":   int64(counters["vector.passed"]),
			"returned": int64(counters["vector.returned"]),
		}
	}

	if hasPrefix("memory", counters, gauges) {
		mem := map[string]any{}
		if v, ok := gauges["memory.extracted"]; ok {
			mem["extracted"] = v
		}
		if hasPrefix("memory.extract", counters, gauges) {
			mem["extract"] = map[string]any{
				"duration_ms": gaugeFloat(gauges, "memory.extract.total.duration_ms"),
				"actions": map[string]any{
					"created": gaugeInt(gauges, "memory.extract.created"),
					"merged":  gaugeInt(gauges, "memory.extract.merged"),
					"deleted": gaugeInt(gauges, "memory.extract.deleted"),
					"skipped": gaugeInt(gauges, "memory.extract.skipped"),
				},
			}
		}
		summary["memory"] = mem
	}

	if hasPrefix("resource", counters, gauges) {
		summary["resource"] = map[string]any{
			"request": map[string]any{
				"duration_ms": gaugeFloat(gauges, "resource.request.duration_ms"),
			},
			"process": map[string]any{
				"duration_ms": gaugeFloat(gauges, "resource.process.duration_ms"),
			},
		}
	}

	if hasPrefix("queue", counters, gauges) {
		summary["queue"] = map[string]any{
			"semantic": map[string]any{
				"processed":   gaugeInt(gauges, "queue.semantic.processed"),
				"error_count": gaugeInt(gauges, "queue.semantic.error_count"),
			},
			"embedding": map[string]any{
				"processed":   gaugeInt(gauges, "queue.embedding.processed"),
				"error_count": gaugeInt(gauges, "queue.embedding.error_count"),
			},
		}
	}

	if errorStage != "" || errorCode != "" || errorMessage != "" {
		summary["errors"] = map[string]any{
			"stage":      errorStage,
			"error_code": errorCode,
			"message":    errorMessage,
		}
	}

	return summary
}

func hasPrefix(prefix string, counters map[string]float64, gauges map[string]any) bool {
	needle := prefix + "."
	for k := range counters {
		if len(k) > len(needle) && k[:len(needle)] == needle {
			return true
		}
	}
	for k := range gauges {
		if len(k) > len(needle) && k[:len(needle)] == needle {
			return true
		}
	}
	return false
}

func gaugeFloat(gauges map[string]any, key string) float64 {
	if v, ok := gauges[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case int64:
			return float64(val)
		}
	}
	return 0
}

func gaugeInt(gauges map[string]any, key string) int64 {
	if v, ok := gauges[key]; ok {
		switch val := v.(type) {
		case int64:
			return val
		case int:
			return int64(val)
		case float64:
			return int64(val)
		}
	}
	return 0
}
