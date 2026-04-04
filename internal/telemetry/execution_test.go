package telemetry

import (
	"context"
	"errors"
	"testing"
)

func TestBuildPayload(t *testing.T) {
	tm := New("test", true)
	tm.Inc("vector.searches")

	payload := BuildPayload(tm, TelemetrySelection{IncludeSummary: true}, "ok")
	if payload == nil {
		t.Fatal("expected non-nil payload")
	}
	if payload["id"] == nil {
		t.Error("missing id")
	}
	if payload["summary"] == nil {
		t.Error("missing summary when IncludeSummary=true")
	}
}

func TestBuildPayload_NoSummary(t *testing.T) {
	tm := New("test", true)
	payload := BuildPayload(tm, TelemetrySelection{IncludeSummary: false}, "ok")
	if payload != nil {
		t.Error("expected nil payload when IncludeSummary=false")
	}
}

func TestBuildPayload_Disabled(t *testing.T) {
	tm := Nop()
	payload := BuildPayload(tm, TelemetrySelection{IncludeSummary: true}, "ok")
	if payload != nil {
		t.Error("expected nil payload from disabled collector")
	}
}

func TestAttachPayload(t *testing.T) {
	result := map[string]any{"status": "ok"}
	payload := map[string]any{"id": "tm_123"}

	out := AttachPayload(result, payload)
	if out["telemetry"] == nil {
		t.Error("expected telemetry key")
	}
	if out["status"] != "ok" {
		t.Error("original result keys preserved")
	}
}

func TestAttachPayload_Nil(t *testing.T) {
	result := map[string]any{"status": "ok"}
	out := AttachPayload(result, nil)
	if _, ok := out["telemetry"]; ok {
		t.Error("nil payload should not be attached")
	}
}

func TestAttachPayload_NilResult(t *testing.T) {
	payload := map[string]any{"id": "tm_123"}
	out := AttachPayload(nil, payload)
	if out["telemetry"] == nil {
		t.Error("should create result map")
	}
}

func TestRunWithTelemetry_Success(t *testing.T) {
	result, err := RunWithTelemetry[string](
		context.Background(),
		"test_op",
		true,
		func(ctx context.Context) (string, error) {
			tm := FromContext(ctx)
			if !tm.Enabled {
				t.Error("telemetry should be enabled in context")
			}
			tm.Inc("vector.searches")
			return "hello", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Result != "hello" {
		t.Errorf("result = %v", result.Result)
	}
	if result.Telemetry == nil {
		t.Error("expected telemetry payload")
	}
	if !result.Selection.IncludeSummary {
		t.Error("expected include summary")
	}
}

func TestRunWithTelemetry_Error(t *testing.T) {
	_, err := RunWithTelemetry[string](
		context.Background(),
		"test_op",
		true,
		func(ctx context.Context) (string, error) {
			return "", errors.New("boom")
		},
	)
	if err == nil {
		t.Error("expected error")
	}
}

func TestRunWithTelemetry_Disabled(t *testing.T) {
	result, err := RunWithTelemetry[string](
		context.Background(),
		"test_op",
		false,
		func(ctx context.Context) (string, error) {
			tm := FromContext(ctx)
			if tm.Enabled {
				t.Error("should be disabled")
			}
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Telemetry != nil {
		t.Error("expected nil telemetry when disabled")
	}
}

func TestRunWithTelemetry_BadRequest(t *testing.T) {
	_, err := RunWithTelemetry[string](
		context.Background(),
		"test_op",
		42,
		func(ctx context.Context) (string, error) {
			return "", nil
		},
	)
	if err == nil {
		t.Error("expected error for bad telemetry request")
	}
}
