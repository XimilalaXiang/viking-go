package telemetry

import (
	"context"
	"testing"
)

func TestContextBinding(t *testing.T) {
	tm := New("ctx_test", true)

	ctx := WithTelemetry(context.Background(), tm)
	got := FromContext(ctx)
	if got != tm {
		t.Error("expected bound telemetry")
	}
	if !got.Enabled {
		t.Error("should be enabled")
	}
}

func TestContextDefault(t *testing.T) {
	got := FromContext(context.Background())
	if got == nil {
		t.Fatal("expected non-nil noop telemetry")
	}
	if got.Enabled {
		t.Error("default should be disabled")
	}

	got.Inc("should_not_panic")
	snap := got.Finish("ok")
	if snap != nil {
		t.Error("noop should return nil snapshot")
	}
}

func TestContextOverride(t *testing.T) {
	tm1 := New("op1", true)
	tm2 := New("op2", true)

	ctx := WithTelemetry(context.Background(), tm1)
	ctx = WithTelemetry(ctx, tm2)

	got := FromContext(ctx)
	if got.Operation != "op2" {
		t.Errorf("expected op2, got %s", got.Operation)
	}
}
