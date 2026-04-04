package telemetry

import "fmt"

// TelemetrySelection holds normalized flags controlling what telemetry data
// gets included in API responses.
type TelemetrySelection struct {
	IncludeSummary bool
}

// IncludePayload returns true if any telemetry data should be attached.
func (s TelemetrySelection) IncludePayload() bool {
	return s.IncludeSummary
}

// NormalizeTelemetryRequest converts a raw telemetry request value (bool,
// map[string]any, or nil) into an explicit TelemetrySelection.
//
// Accepted formats:
//   - nil or false → no telemetry
//   - true → include full summary
//   - map with optional "summary" bool key
func NormalizeTelemetryRequest(raw any) (TelemetrySelection, error) {
	if raw == nil {
		return TelemetrySelection{}, nil
	}

	switch v := raw.(type) {
	case bool:
		return TelemetrySelection{IncludeSummary: v}, nil
	case map[string]any:
		return parseMapRequest(v)
	}

	return TelemetrySelection{}, fmt.Errorf("telemetry must be a boolean or an object")
}

var allowedTelemetryKeys = map[string]bool{"summary": true}

func parseMapRequest(m map[string]any) (TelemetrySelection, error) {
	for k := range m {
		if !allowedTelemetryKeys[k] {
			return TelemetrySelection{}, fmt.Errorf("unsupported telemetry option: %q", k)
		}
	}

	includeSummary := true
	if v, ok := m["summary"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return TelemetrySelection{}, fmt.Errorf("telemetry.summary must be a boolean")
		}
		includeSummary = b
	}

	return TelemetrySelection{IncludeSummary: includeSummary}, nil
}
