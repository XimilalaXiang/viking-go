package telemetry

import "testing"

func TestNormalizeTelemetryRequest_Nil(t *testing.T) {
	sel, err := NormalizeTelemetryRequest(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sel.IncludeSummary {
		t.Error("nil should mean no summary")
	}
	if sel.IncludePayload() {
		t.Error("nil should mean no payload")
	}
}

func TestNormalizeTelemetryRequest_True(t *testing.T) {
	sel, err := NormalizeTelemetryRequest(true)
	if err != nil {
		t.Fatal(err)
	}
	if !sel.IncludeSummary {
		t.Error("true should include summary")
	}
	if !sel.IncludePayload() {
		t.Error("true should include payload")
	}
}

func TestNormalizeTelemetryRequest_False(t *testing.T) {
	sel, err := NormalizeTelemetryRequest(false)
	if err != nil {
		t.Fatal(err)
	}
	if sel.IncludeSummary {
		t.Error("false should exclude summary")
	}
}

func TestNormalizeTelemetryRequest_MapDefault(t *testing.T) {
	sel, err := NormalizeTelemetryRequest(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !sel.IncludeSummary {
		t.Error("empty map should default to include summary")
	}
}

func TestNormalizeTelemetryRequest_MapSummaryFalse(t *testing.T) {
	sel, err := NormalizeTelemetryRequest(map[string]any{"summary": false})
	if err != nil {
		t.Fatal(err)
	}
	if sel.IncludeSummary {
		t.Error("summary=false should exclude summary")
	}
	if sel.IncludePayload() {
		t.Error("should not include payload")
	}
}

func TestNormalizeTelemetryRequest_UnknownKey(t *testing.T) {
	_, err := NormalizeTelemetryRequest(map[string]any{"unknown": true})
	if err == nil {
		t.Error("expected error for unknown key")
	}
}

func TestNormalizeTelemetryRequest_BadSummaryType(t *testing.T) {
	_, err := NormalizeTelemetryRequest(map[string]any{"summary": "yes"})
	if err == nil {
		t.Error("expected error for non-bool summary")
	}
}

func TestNormalizeTelemetryRequest_BadType(t *testing.T) {
	_, err := NormalizeTelemetryRequest(42)
	if err == nil {
		t.Error("expected error for int")
	}
}
