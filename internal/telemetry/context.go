package telemetry

import "context"

type ctxKey struct{}

var noopTelemetry = &OperationTelemetry{
	Enabled:  false,
	counters: make(map[string]float64),
	gauges:   make(map[string]any),
}

// WithTelemetry returns a new context carrying the given OperationTelemetry.
func WithTelemetry(ctx context.Context, t *OperationTelemetry) context.Context {
	return context.WithValue(ctx, ctxKey{}, t)
}

// FromContext retrieves the OperationTelemetry bound to this context.
// Returns a disabled no-op instance if none is bound.
func FromContext(ctx context.Context) *OperationTelemetry {
	if t, ok := ctx.Value(ctxKey{}).(*OperationTelemetry); ok && t != nil {
		return t
	}
	return noopTelemetry
}
