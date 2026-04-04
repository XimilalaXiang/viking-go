package embedder

import (
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/ximilala/viking-go/internal/resilience"
)

// ResilientEmbedder wraps an Embedder with circuit breaker protection and
// health tracking. When the underlying provider is unavailable, calls fail
// fast with a descriptive error instead of blocking on retries.
type ResilientEmbedder struct {
	inner   Embedder
	breaker *resilience.CircuitBreaker

	totalCalls   int64
	totalErrors  int64
	lastErrorMsg atomic.Value // stores string
}

// NewResilientEmbedder wraps an embedder with circuit breaker protection.
// failureThreshold: how many consecutive failures before opening the circuit.
// resetTimeout: how long to wait before allowing a probe request.
func NewResilientEmbedder(inner Embedder, failureThreshold int, resetTimeout time.Duration) *ResilientEmbedder {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if resetTimeout <= 0 {
		resetTimeout = 2 * time.Minute
	}
	return &ResilientEmbedder{
		inner:   inner,
		breaker: resilience.NewCircuitBreaker(failureThreshold, resetTimeout),
	}
}

func (r *ResilientEmbedder) Embed(text string, isQuery bool) (*EmbedResult, error) {
	atomic.AddInt64(&r.totalCalls, 1)

	if err := r.breaker.Check(); err != nil {
		atomic.AddInt64(&r.totalErrors, 1)
		return nil, fmt.Errorf("embedder circuit open: %w", err)
	}

	result, err := r.inner.Embed(text, isQuery)
	if err != nil {
		atomic.AddInt64(&r.totalErrors, 1)
		r.lastErrorMsg.Store(err.Error())
		r.breaker.RecordFailure(err)
		return nil, err
	}

	r.breaker.RecordSuccess()
	return result, nil
}

func (r *ResilientEmbedder) EmbedBatch(texts []string, isQuery bool) ([]*EmbedResult, error) {
	atomic.AddInt64(&r.totalCalls, 1)

	if err := r.breaker.Check(); err != nil {
		atomic.AddInt64(&r.totalErrors, 1)
		return nil, fmt.Errorf("embedder circuit open: %w", err)
	}

	results, err := r.inner.EmbedBatch(texts, isQuery)
	if err != nil {
		atomic.AddInt64(&r.totalErrors, 1)
		r.lastErrorMsg.Store(err.Error())
		r.breaker.RecordFailure(err)
		return nil, err
	}

	r.breaker.RecordSuccess()
	return results, nil
}

func (r *ResilientEmbedder) Dimension() int { return r.inner.Dimension() }

func (r *ResilientEmbedder) Close() { r.inner.Close() }

// Health returns a health status map for observability.
func (r *ResilientEmbedder) Health() map[string]any {
	state := r.breaker.State()
	status := "healthy"
	switch state {
	case resilience.StateOpen:
		status = "unavailable"
	case resilience.StateHalfOpen:
		status = "recovering"
	}

	h := map[string]any{
		"status":         status,
		"circuit_state":  string(state),
		"total_calls":    atomic.LoadInt64(&r.totalCalls),
		"total_errors":   atomic.LoadInt64(&r.totalErrors),
		"failure_count":  r.breaker.FailureCount(),
	}

	if v := r.lastErrorMsg.Load(); v != nil {
		h["last_error"] = v.(string)
	}

	if state == resilience.StateOpen {
		h["retry_after_seconds"] = r.breaker.RetryAfter().Seconds()
	}

	return h
}

// IsAvailable returns true if the circuit is not open (i.e., calls are allowed).
func (r *ResilientEmbedder) IsAvailable() bool {
	return r.breaker.State() != resilience.StateOpen
}

// ResilientLLMClient wraps an LLM Client with circuit breaker protection.
// Unlike embeddings, LLM failures in memory extraction should not block
// the entire system — extraction is skipped gracefully.
type ResilientLLMClient struct {
	breaker *resilience.CircuitBreaker

	totalCalls  int64
	totalErrors int64
	lastError   atomic.Value
}

// NewResilientLLMClient creates a circuit breaker tracker for LLM calls.
func NewResilientLLMClient(failureThreshold int, resetTimeout time.Duration) *ResilientLLMClient {
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	if resetTimeout <= 0 {
		resetTimeout = 3 * time.Minute
	}
	return &ResilientLLMClient{
		breaker: resilience.NewCircuitBreaker(failureThreshold, resetTimeout),
	}
}

// CheckAndRecord checks if the circuit is open and records results.
// Returns nil if calls are allowed. Call RecordResult after the operation.
func (r *ResilientLLMClient) CheckAndRecord() error {
	atomic.AddInt64(&r.totalCalls, 1)
	return r.breaker.Check()
}

// RecordResult records success or failure.
func (r *ResilientLLMClient) RecordResult(err error) {
	if err == nil {
		r.breaker.RecordSuccess()
		return
	}
	atomic.AddInt64(&r.totalErrors, 1)
	r.lastError.Store(err.Error())
	r.breaker.RecordFailure(err)
	log.Printf("[ResilientLLM] failure recorded: %v (count=%d)", err, r.breaker.FailureCount())
}

// Health returns health status.
func (r *ResilientLLMClient) Health() map[string]any {
	state := r.breaker.State()
	status := "healthy"
	switch state {
	case resilience.StateOpen:
		status = "unavailable"
	case resilience.StateHalfOpen:
		status = "recovering"
	}
	h := map[string]any{
		"status":        status,
		"circuit_state": string(state),
		"total_calls":   atomic.LoadInt64(&r.totalCalls),
		"total_errors":  atomic.LoadInt64(&r.totalErrors),
		"failure_count": r.breaker.FailureCount(),
	}
	if v := r.lastError.Load(); v != nil {
		h["last_error"] = v.(string)
	}
	return h
}

// IsAvailable returns true if LLM calls are allowed.
func (r *ResilientLLMClient) IsAvailable() bool {
	return r.breaker.State() != resilience.StateOpen
}
