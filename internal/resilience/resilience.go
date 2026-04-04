// Package resilience provides circuit breaker and retry utilities for
// protecting API calls against transient and permanent failures.
package resilience

import (
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// --- Error classification ---

var permanentPatterns = []string{
	"400", "401", "403",
	"Forbidden", "Unauthorized", "AccountOverdue",
}

var transientPatterns = []string{
	"429", "500", "502", "503", "504",
	"TooManyRequests", "RateLimit", "RequestBurstTooFast",
	"timeout", "Timeout",
	"ConnectionError", "Connection refused", "Connection reset",
}

// ErrorClass represents the category of an API error.
type ErrorClass string

const (
	ErrorPermanent ErrorClass = "permanent"
	ErrorTransient ErrorClass = "transient"
	ErrorUnknown   ErrorClass = "unknown"
)

// ClassifyError categorizes an error based on known patterns.
func ClassifyError(err error) ErrorClass {
	if err == nil {
		return ErrorUnknown
	}
	texts := []string{err.Error()}
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		texts = append(texts, unwrapped.Error())
	}

	for _, text := range texts {
		for _, pattern := range permanentPatterns {
			if strings.Contains(text, pattern) {
				return ErrorPermanent
			}
		}
	}
	for _, text := range texts {
		for _, pattern := range transientPatterns {
			if strings.Contains(text, pattern) {
				return ErrorTransient
			}
		}
	}
	return ErrorUnknown
}

// IsRetryable returns true if the error is classified as transient.
func IsRetryable(err error) bool {
	return ClassifyError(err) == ErrorTransient
}

// --- Circuit Breaker ---

// CircuitState represents the state of a circuit breaker.
type CircuitState string

const (
	StateClosed   CircuitState = "CLOSED"
	StateOpen     CircuitState = "OPEN"
	StateHalfOpen CircuitState = "HALF_OPEN"
)

// ErrCircuitOpen is returned when the breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreaker protects API calls from cascading failures.
// After FailureThreshold consecutive failures (or immediate trip on permanent
// errors), the breaker opens and blocks requests for ResetTimeout. After the
// timeout, one probe request is allowed (HALF_OPEN). If it succeeds the
// breaker closes; if it fails the breaker reopens.
type CircuitBreaker struct {
	FailureThreshold int
	ResetTimeout     time.Duration

	mu              sync.Mutex
	state           CircuitState
	failureCount    int
	lastFailureTime time.Time
}

// NewCircuitBreaker creates a circuit breaker with the given thresholds.
func NewCircuitBreaker(failureThreshold int, resetTimeout time.Duration) *CircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if resetTimeout <= 0 {
		resetTimeout = 5 * time.Minute
	}
	return &CircuitBreaker{
		FailureThreshold: failureThreshold,
		ResetTimeout:     resetTimeout,
		state:            StateClosed,
	}
}

// Check allows the request through or returns ErrCircuitOpen.
func (cb *CircuitBreaker) Check() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed, StateHalfOpen:
		return nil
	case StateOpen:
		if time.Since(cb.lastFailureTime) >= cb.ResetTimeout {
			cb.state = StateHalfOpen
			log.Printf("[CircuitBreaker] OPEN -> HALF_OPEN (timeout elapsed)")
			return nil
		}
		remaining := cb.ResetTimeout - time.Since(cb.lastFailureTime)
		return fmt.Errorf("%w: retry after %.0fs", ErrCircuitOpen, remaining.Seconds())
	}
	return nil
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// RetryAfter returns seconds until HALF_OPEN transition, capped at 30s.
func (cb *CircuitBreaker) RetryAfter() time.Duration {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state != StateOpen {
		return 0
	}
	remaining := cb.ResetTimeout - time.Since(cb.lastFailureTime)
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 30*time.Second {
		remaining = 30 * time.Second
	}
	return remaining
}

// RecordSuccess records a successful call, resetting the breaker.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == StateHalfOpen {
		log.Printf("[CircuitBreaker] HALF_OPEN -> CLOSED (probe succeeded)")
	}
	cb.failureCount = 0
	cb.state = StateClosed
}

// RecordFailure records a failed call, potentially tripping the breaker.
func (cb *CircuitBreaker) RecordFailure(err error) {
	errClass := ClassifyError(err)
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.state == StateHalfOpen {
		cb.state = StateOpen
		log.Printf("[CircuitBreaker] HALF_OPEN -> OPEN (probe failed)")
		return
	}

	if errClass == ErrorPermanent {
		cb.state = StateOpen
		log.Printf("[CircuitBreaker] tripped immediately on permanent error: %v", err)
		return
	}

	if cb.failureCount >= cb.FailureThreshold {
		cb.state = StateOpen
		log.Printf("[CircuitBreaker] tripped after %d failures", cb.failureCount)
	}
}

// FailureCount returns the current consecutive failure count.
func (cb *CircuitBreaker) FailureCount() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failureCount
}

// --- Retry ---

// RetryConfig controls retry behavior.
type RetryConfig struct {
	MaxRetries    int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	Jitter        bool
	IsRetryable   func(error) bool
	OperationName string
}

// DefaultRetryConfig returns sensible retry defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:    3,
		BaseDelay:     500 * time.Millisecond,
		MaxDelay:      8 * time.Second,
		Jitter:        true,
		IsRetryable:   IsRetryable,
		OperationName: "operation",
	}
}

func computeDelay(attempt int, cfg RetryConfig) time.Duration {
	delay := float64(cfg.BaseDelay) * math.Pow(2, float64(attempt))
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}
	if cfg.Jitter {
		jitter := rand.Float64() * math.Min(float64(cfg.BaseDelay), delay)
		delay += jitter
	}
	return time.Duration(delay)
}

// Retry executes fn with exponential backoff retry on retryable errors.
func Retry[T any](fn func() (T, error), cfg RetryConfig) (T, error) {
	if cfg.IsRetryable == nil {
		cfg.IsRetryable = IsRetryable
	}

	var zero T
	var lastErr error

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err

		if attempt >= cfg.MaxRetries || !cfg.IsRetryable(err) {
			return zero, err
		}

		delay := computeDelay(attempt, cfg)
		log.Printf("[Retry] %s failed (attempt %d/%d): %v; retrying in %s",
			cfg.OperationName, attempt+1, cfg.MaxRetries, err, delay)
		time.Sleep(delay)
	}

	return zero, lastErr
}

// RetrySimple is a convenience wrapper with default config.
func RetrySimple[T any](fn func() (T, error)) (T, error) {
	return Retry(fn, DefaultRetryConfig())
}
