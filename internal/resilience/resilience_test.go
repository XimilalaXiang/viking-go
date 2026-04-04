package resilience

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		err  error
		want ErrorClass
	}{
		{errors.New("HTTP 401 Unauthorized"), ErrorPermanent},
		{errors.New("403 Forbidden"), ErrorPermanent},
		{errors.New("AccountOverdue"), ErrorPermanent},
		{errors.New("HTTP 429 TooManyRequests"), ErrorTransient},
		{errors.New("HTTP 502 Bad Gateway"), ErrorTransient},
		{errors.New("Connection refused"), ErrorTransient},
		{errors.New("request Timeout"), ErrorTransient},
		{errors.New("something unexpected"), ErrorUnknown},
		{nil, ErrorUnknown},
	}

	for _, tt := range tests {
		got := ClassifyError(tt.err)
		if got != tt.want {
			t.Errorf("ClassifyError(%v) = %s, want %s", tt.err, got, tt.want)
		}
	}
}

func TestClassifyWrappedError(t *testing.T) {
	inner := errors.New("HTTP 503 Service Unavailable")
	outer := fmt.Errorf("api call: %w", inner)
	if ClassifyError(outer) != ErrorTransient {
		t.Error("wrapped 503 should be transient")
	}
}

func TestIsRetryable(t *testing.T) {
	if !IsRetryable(errors.New("HTTP 429")) {
		t.Error("429 should be retryable")
	}
	if IsRetryable(errors.New("403 Forbidden")) {
		t.Error("403 should not be retryable")
	}
	if IsRetryable(errors.New("random error")) {
		t.Error("unknown error should not be retryable")
	}
}

func TestCircuitBreaker_ClosedState(t *testing.T) {
	cb := NewCircuitBreaker(3, 100*time.Millisecond)
	if cb.State() != StateClosed {
		t.Error("should start closed")
	}
	if err := cb.Check(); err != nil {
		t.Error("closed breaker should allow requests")
	}
}

func TestCircuitBreaker_TripsOnThreshold(t *testing.T) {
	cb := NewCircuitBreaker(3, 100*time.Millisecond)

	cb.RecordFailure(errors.New("HTTP 500"))
	cb.RecordFailure(errors.New("HTTP 500"))
	if cb.State() != StateClosed {
		t.Error("should still be closed after 2 failures")
	}

	cb.RecordFailure(errors.New("HTTP 500"))
	if cb.State() != StateOpen {
		t.Error("should be open after 3 failures")
	}

	err := cb.Check()
	if !errors.Is(err, ErrCircuitOpen) {
		t.Error("should return circuit open error")
	}
}

func TestCircuitBreaker_TripsImmediatelyOnPermanent(t *testing.T) {
	cb := NewCircuitBreaker(5, 100*time.Millisecond)
	cb.RecordFailure(errors.New("401 Unauthorized"))
	if cb.State() != StateOpen {
		t.Error("permanent error should trip immediately")
	}
}

func TestCircuitBreaker_HalfOpenOnTimeout(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)
	cb.RecordFailure(errors.New("HTTP 500"))

	if cb.State() != StateOpen {
		t.Fatal("should be open")
	}

	time.Sleep(60 * time.Millisecond)
	if err := cb.Check(); err != nil {
		t.Error("should allow probe after timeout")
	}
	if cb.State() != StateHalfOpen {
		t.Error("should be half-open")
	}
}

func TestCircuitBreaker_HalfOpenToClosedOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)
	cb.RecordFailure(errors.New("HTTP 500"))
	time.Sleep(60 * time.Millisecond)
	cb.Check()

	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Error("should close after successful probe")
	}
	if cb.FailureCount() != 0 {
		t.Error("failure count should reset")
	}
}

func TestCircuitBreaker_HalfOpenToOpenOnFailure(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)
	cb.RecordFailure(errors.New("HTTP 500"))
	time.Sleep(60 * time.Millisecond)
	cb.Check()

	cb.RecordFailure(errors.New("HTTP 500"))
	if cb.State() != StateOpen {
		t.Error("should reopen after probe failure")
	}
}

func TestCircuitBreaker_SuccessResetsCount(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Minute)
	cb.RecordFailure(errors.New("timeout"))
	cb.RecordFailure(errors.New("timeout"))
	cb.RecordSuccess()

	if cb.FailureCount() != 0 {
		t.Error("success should reset failure count")
	}
	cb.RecordFailure(errors.New("timeout"))
	if cb.State() != StateClosed {
		t.Error("should still be closed (count was reset)")
	}
}

func TestCircuitBreaker_RetryAfter(t *testing.T) {
	cb := NewCircuitBreaker(1, time.Minute)
	if cb.RetryAfter() != 0 {
		t.Error("closed breaker should have 0 retry_after")
	}

	cb.RecordFailure(errors.New("HTTP 500"))
	ra := cb.RetryAfter()
	if ra <= 0 || ra > 30*time.Second {
		t.Errorf("retry_after = %v, expected > 0 and <= 30s", ra)
	}
}

func TestRetry_Success(t *testing.T) {
	calls := int32(0)
	result, err := Retry(func() (string, error) {
		atomic.AddInt32(&calls, 1)
		return "ok", nil
	}, RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond})

	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Errorf("result = %s", result)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Error("should only call once on success")
	}
}

func TestRetry_TransientThenSuccess(t *testing.T) {
	calls := int32(0)
	result, err := Retry(func() (string, error) {
		n := atomic.AddInt32(&calls, 1)
		if n <= 2 {
			return "", errors.New("HTTP 503 Service Unavailable")
		}
		return "ok", nil
	}, RetryConfig{
		MaxRetries:    3,
		BaseDelay:     time.Millisecond,
		MaxDelay:      5 * time.Millisecond,
		IsRetryable:   IsRetryable,
		OperationName: "test",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %s", result)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetry_PermanentError(t *testing.T) {
	calls := int32(0)
	_, err := Retry(func() (string, error) {
		atomic.AddInt32(&calls, 1)
		return "", errors.New("401 Unauthorized")
	}, RetryConfig{
		MaxRetries:  3,
		BaseDelay:   time.Millisecond,
		IsRetryable: IsRetryable,
	})

	if err == nil {
		t.Error("expected error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Error("permanent error should not retry")
	}
}

func TestRetry_ExhaustsRetries(t *testing.T) {
	calls := int32(0)
	_, err := Retry(func() (string, error) {
		atomic.AddInt32(&calls, 1)
		return "", errors.New("HTTP 503")
	}, RetryConfig{
		MaxRetries:  2,
		BaseDelay:   time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
		IsRetryable: IsRetryable,
	})

	if err == nil {
		t.Error("expected error after exhausting retries")
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("expected 3 calls (1 + 2 retries), got %d", calls)
	}
}

func TestRetrySimple(t *testing.T) {
	result, err := RetrySimple(func() (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != 42 {
		t.Errorf("result = %d", result)
	}
}

func TestComputeDelay(t *testing.T) {
	cfg := RetryConfig{
		BaseDelay: 100 * time.Millisecond,
		MaxDelay:  5 * time.Second,
		Jitter:    false,
	}

	d0 := computeDelay(0, cfg)
	if d0 != 100*time.Millisecond {
		t.Errorf("delay(0) = %v, want 100ms", d0)
	}

	d1 := computeDelay(1, cfg)
	if d1 != 200*time.Millisecond {
		t.Errorf("delay(1) = %v, want 200ms", d1)
	}

	d10 := computeDelay(10, cfg)
	if d10 > 5*time.Second {
		t.Error("should be capped at max_delay")
	}
}

func TestComputeDelayWithJitter(t *testing.T) {
	cfg := RetryConfig{
		BaseDelay: 100 * time.Millisecond,
		MaxDelay:  5 * time.Second,
		Jitter:    true,
	}

	d := computeDelay(0, cfg)
	if d < 100*time.Millisecond || d > 200*time.Millisecond {
		t.Errorf("delay with jitter = %v, expected between 100ms and 200ms", d)
	}
}
