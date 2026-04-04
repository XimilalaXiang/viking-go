// Package observer provides a component health monitoring framework.
// Each Observer monitors a subsystem and reports health, errors, and
// formatted status information.
package observer

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Observer is the interface that all system observers must implement.
type Observer interface {
	// Name returns the observer's subsystem identifier.
	Name() string
	// IsHealthy reports whether the subsystem is operating normally.
	IsHealthy() bool
	// HasErrors reports whether the subsystem has active errors.
	HasErrors() bool
	// StatusTable returns a human-readable status summary.
	StatusTable() string
	// StatusJSON returns structured status data for API responses.
	StatusJSON() map[string]any
}

// Registry holds all registered observers and provides aggregate health checks.
type Registry struct {
	mu        sync.RWMutex
	observers map[string]Observer
}

// NewRegistry creates an empty observer registry.
func NewRegistry() *Registry {
	return &Registry{observers: make(map[string]Observer)}
}

// Register adds an observer.
func (r *Registry) Register(obs Observer) {
	r.mu.Lock()
	r.observers[obs.Name()] = obs
	r.mu.Unlock()
}

// Get returns a specific observer by name.
func (r *Registry) Get(name string) Observer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.observers[name]
}

// Names returns all registered observer names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.observers))
	for n := range r.observers {
		names = append(names, n)
	}
	return names
}

// All returns a snapshot of all observers.
func (r *Registry) All() map[string]Observer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]Observer, len(r.observers))
	for k, v := range r.observers {
		out[k] = v
	}
	return out
}

// IsHealthy returns true only if all observers report healthy.
func (r *Registry) IsHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, obs := range r.observers {
		if !obs.IsHealthy() {
			return false
		}
	}
	return true
}

// SystemStatus returns aggregate system health as structured data.
func (r *Registry) SystemStatus() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	components := make(map[string]any, len(r.observers))
	allHealthy := true
	for name, obs := range r.observers {
		healthy := obs.IsHealthy()
		if !healthy {
			allHealthy = false
		}
		components[name] = map[string]any{
			"is_healthy": healthy,
			"has_errors": obs.HasErrors(),
			"status":     obs.StatusJSON(),
		}
	}

	status := "ok"
	if !allHealthy {
		status = "degraded"
	}

	return map[string]any{
		"status":     status,
		"is_healthy": allHealthy,
		"components": components,
	}
}

// --- Concrete observers ---

// QueueHealthFunc returns queue statistics.
type QueueHealthFunc func() map[string]any

// QueueObserver monitors the embedding and semantic queues.
type QueueObserver struct {
	getStats QueueHealthFunc
}

func NewQueueObserver(getStats QueueHealthFunc) *QueueObserver {
	return &QueueObserver{getStats: getStats}
}

func (o *QueueObserver) Name() string { return "queue" }

func (o *QueueObserver) IsHealthy() bool {
	stats := o.getStats()
	if errCount, _ := stats["error_count"].(int64); errCount > 0 {
		return false
	}
	return true
}

func (o *QueueObserver) HasErrors() bool {
	stats := o.getStats()
	errCount, _ := stats["error_count"].(int64)
	return errCount > 0
}

func (o *QueueObserver) StatusTable() string {
	stats := o.getStats()
	var sb strings.Builder
	sb.WriteString("Queue Status:\n")
	for k, v := range stats {
		sb.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
	}
	return sb.String()
}

func (o *QueueObserver) StatusJSON() map[string]any {
	return o.getStats()
}

// StorageHealthFunc checks storage backend health.
type StorageHealthFunc func() (healthy bool, details map[string]any)

// StorageObserver monitors the vector storage backend.
type StorageObserver struct {
	check StorageHealthFunc
}

func NewStorageObserver(check StorageHealthFunc) *StorageObserver {
	return &StorageObserver{check: check}
}

func (o *StorageObserver) Name() string { return "storage" }

func (o *StorageObserver) IsHealthy() bool {
	healthy, _ := o.check()
	return healthy
}

func (o *StorageObserver) HasErrors() bool {
	healthy, _ := o.check()
	return !healthy
}

func (o *StorageObserver) StatusTable() string {
	healthy, details := o.check()
	status := "OK"
	if !healthy {
		status = "ERROR"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Storage: %s\n", status))
	for k, v := range details {
		sb.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
	}
	return sb.String()
}

func (o *StorageObserver) StatusJSON() map[string]any {
	healthy, details := o.check()
	if details == nil {
		details = make(map[string]any)
	}
	details["is_healthy"] = healthy
	return details
}

// ModelsObserver monitors LLM, embedding, and rerank model availability.
type ModelsObserver struct {
	mu         sync.RWMutex
	llmOK      bool
	embedOK    bool
	rerankOK   bool
	llmModel   string
	embedModel string
	rerankModel string
}

func NewModelsObserver() *ModelsObserver {
	return &ModelsObserver{}
}

// SetLLMStatus updates LLM model status.
func (o *ModelsObserver) SetLLMStatus(model string, ok bool) {
	o.mu.Lock()
	o.llmModel = model
	o.llmOK = ok
	o.mu.Unlock()
}

// SetEmbeddingStatus updates embedding model status.
func (o *ModelsObserver) SetEmbeddingStatus(model string, ok bool) {
	o.mu.Lock()
	o.embedModel = model
	o.embedOK = ok
	o.mu.Unlock()
}

// SetRerankStatus updates rerank model status.
func (o *ModelsObserver) SetRerankStatus(model string, ok bool) {
	o.mu.Lock()
	o.rerankModel = model
	o.rerankOK = ok
	o.mu.Unlock()
}

func (o *ModelsObserver) Name() string { return "models" }

func (o *ModelsObserver) IsHealthy() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.llmOK || o.embedOK || o.rerankOK
}

func (o *ModelsObserver) HasErrors() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return !o.llmOK && !o.embedOK && !o.rerankOK
}

func (o *ModelsObserver) StatusTable() string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return fmt.Sprintf("Models: LLM=%s(%v) Embed=%s(%v) Rerank=%s(%v)",
		o.llmModel, o.llmOK, o.embedModel, o.embedOK, o.rerankModel, o.rerankOK)
}

func (o *ModelsObserver) StatusJSON() map[string]any {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return map[string]any{
		"llm":     map[string]any{"model": o.llmModel, "available": o.llmOK},
		"embedding": map[string]any{"model": o.embedModel, "available": o.embedOK},
		"rerank":  map[string]any{"model": o.rerankModel, "available": o.rerankOK},
	}
}

// LockStatsFunc returns lock manager statistics.
type LockStatsFunc func() map[string]any

// LockObserver monitors the path-level lock manager.
type LockObserver struct {
	getStats  LockStatsFunc
	threshold time.Duration
}

func NewLockObserver(getStats LockStatsFunc, hangingThreshold time.Duration) *LockObserver {
	if hangingThreshold <= 0 {
		hangingThreshold = 10 * time.Minute
	}
	return &LockObserver{getStats: getStats, threshold: hangingThreshold}
}

func (o *LockObserver) Name() string { return "locks" }

func (o *LockObserver) IsHealthy() bool {
	stats := o.getStats()
	handles, _ := stats["active_handles"].(int)
	return handles < 100
}

func (o *LockObserver) HasErrors() bool {
	return !o.IsHealthy()
}

func (o *LockObserver) StatusTable() string {
	stats := o.getStats()
	return fmt.Sprintf("Locks: handles=%v locks=%v expire=%v",
		stats["active_handles"], stats["active_locks"], stats["lock_expire"])
}

func (o *LockObserver) StatusJSON() map[string]any {
	return o.getStats()
}

// RetrievalStatsFunc returns retrieval quality statistics.
type RetrievalStatsFunc func() map[string]any

// RetrievalObserver monitors retrieval quality metrics.
type RetrievalObserver struct {
	getStats           RetrievalStatsFunc
	zeroResultThreshold float64
}

func NewRetrievalObserver(getStats RetrievalStatsFunc) *RetrievalObserver {
	return &RetrievalObserver{getStats: getStats, zeroResultThreshold: 0.5}
}

func (o *RetrievalObserver) Name() string { return "retrieval" }

func (o *RetrievalObserver) IsHealthy() bool {
	stats := o.getStats()
	total, _ := stats["total_queries"].(int64)
	if total == 0 {
		return true
	}
	rate, _ := stats["zero_result_rate"].(float64)
	return rate < o.zeroResultThreshold
}

func (o *RetrievalObserver) HasErrors() bool {
	stats := o.getStats()
	total, _ := stats["total_queries"].(int64)
	if total < 5 {
		return false
	}
	rate, _ := stats["zero_result_rate"].(float64)
	return rate >= o.zeroResultThreshold
}

func (o *RetrievalObserver) StatusTable() string {
	stats := o.getStats()
	return fmt.Sprintf("Retrieval: queries=%v results=%v zero_rate=%v avg_score=%v",
		stats["total_queries"], stats["total_results"],
		stats["zero_result_rate"], stats["avg_score"])
}

func (o *RetrievalObserver) StatusJSON() map[string]any {
	return o.getStats()
}
