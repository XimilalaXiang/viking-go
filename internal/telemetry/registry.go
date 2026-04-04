package telemetry

import "sync"

var (
	registered     = make(map[string]*OperationTelemetry)
	registeredLock sync.Mutex
)

// Register registers a telemetry handle for later lookup.
func Register(t *OperationTelemetry) {
	if !t.Enabled || t.TelemetryID == "" {
		return
	}
	registeredLock.Lock()
	registered[t.TelemetryID] = t
	registeredLock.Unlock()
}

// Resolve looks up a registered telemetry handle by ID.
func Resolve(telemetryID string) *OperationTelemetry {
	if telemetryID == "" {
		return nil
	}
	registeredLock.Lock()
	defer registeredLock.Unlock()
	return registered[telemetryID]
}

// Unregister removes a telemetry handle from the registry.
func Unregister(telemetryID string) {
	if telemetryID == "" {
		return
	}
	registeredLock.Lock()
	delete(registered, telemetryID)
	registeredLock.Unlock()
}

// ActiveCount returns the number of active telemetry handles.
func ActiveCount() int {
	registeredLock.Lock()
	defer registeredLock.Unlock()
	return len(registered)
}
