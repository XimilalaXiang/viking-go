package telemetry

import "context"

// ExecutionResult wraps an operation result with optional telemetry payload.
type ExecutionResult[T any] struct {
	Result    T
	Telemetry map[string]any
	Selection TelemetrySelection
}

// BuildPayload finalizes a collector and returns a serializable telemetry map
// respecting the given selection flags. Returns nil if telemetry is disabled
// or excluded by selection.
func BuildPayload(collector *OperationTelemetry, sel TelemetrySelection, status string) map[string]any {
	snap := collector.Finish(status)
	if snap == nil || !sel.IncludePayload() {
		return nil
	}

	payload := map[string]any{"id": snap.TelemetryID}
	if sel.IncludeSummary {
		payload["summary"] = snap.Summary
	}
	return payload
}

// AttachPayload attaches a telemetry payload to a result map under the
// "telemetry" key. If the payload is nil, the result is returned unchanged.
func AttachPayload(result map[string]any, payload map[string]any) map[string]any {
	if payload == nil {
		return result
	}
	if result == nil {
		result = make(map[string]any)
	}
	result["telemetry"] = payload
	return result
}

// RunWithTelemetry creates a collector, binds it to the context, executes fn,
// and returns the result plus telemetry.
func RunWithTelemetry[T any](
	ctx context.Context,
	operation string,
	rawTelemetryReq any,
	fn func(ctx context.Context) (T, error),
) (ExecutionResult[T], error) {
	sel, err := NormalizeTelemetryRequest(rawTelemetryReq)
	if err != nil {
		var zero T
		return ExecutionResult[T]{Result: zero}, err
	}

	collector := New(operation, sel.IncludePayload())
	tctx := WithTelemetry(ctx, collector)

	result, fnErr := fn(tctx)
	if fnErr != nil {
		collector.SetError(operation, "error", fnErr.Error())
		collector.Finish("error")
		return ExecutionResult[T]{Result: result, Selection: sel}, fnErr
	}

	payload := BuildPayload(collector, sel, "ok")
	return ExecutionResult[T]{
		Result:    result,
		Telemetry: payload,
		Selection: sel,
	}, nil
}
