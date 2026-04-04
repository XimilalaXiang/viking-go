package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDebugHealthEndpoint(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/api/v1/debug/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Errorf("status = %v", body["status"])
	}
}

func TestDebugVectorCount(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/api/v1/debug/vector/count", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	result := body["result"].(map[string]any)
	if result["count"].(float64) < 0 {
		t.Errorf("count should be >= 0")
	}
}

func TestDebugVectorScroll(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/api/v1/debug/vector/scroll?limit=10", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestObserverQueue(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/api/v1/observer/queue", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Errorf("status = %v", body["status"])
	}
}

func TestObserverStorage(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/api/v1/observer/storage", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestObserverModels(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/api/v1/observer/models", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestObserverSystem(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/api/v1/observer/system", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	result := body["result"].(map[string]any)
	if _, ok := result["uptime_seconds"]; !ok {
		t.Errorf("missing uptime_seconds")
	}
	if _, ok := result["components"]; !ok {
		t.Errorf("missing components")
	}
}

func TestStatsMemories(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/api/v1/stats/memories", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Errorf("status = %v", body["status"])
	}
}

func TestStatsMemoriesInvalidCategory(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/api/v1/stats/memories?category=bogus", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestTasksListEmpty(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	result := body["result"].([]any)
	if len(result) != 0 {
		t.Errorf("expected empty task list, got %d", len(result))
	}
}

func TestTasksGetNotFound(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/api/v1/tasks/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestPackExportImport(t *testing.T) {
	srv := testServer(t)

	// Create some content
	writeBody, _ := json.Marshal(map[string]string{
		"uri":     "viking://resources/packtest/hello.md",
		"content": "# Hello Pack",
	})
	wReq := httptest.NewRequest("POST", "/api/v1/content/write", bytes.NewReader(writeBody))
	wReq.Header.Set("Content-Type", "application/json")
	wW := httptest.NewRecorder()
	srv.ServeHTTP(wW, wReq)
	if wW.Code != http.StatusOK {
		t.Fatalf("write status = %d", wW.Code)
	}

	// Export
	outDir := t.TempDir()
	outFile := filepath.Join(outDir, "test.ovpack")

	exportBody, _ := json.Marshal(map[string]string{
		"uri": "viking://resources/packtest",
		"to":  outFile,
	})
	eReq := httptest.NewRequest("POST", "/api/v1/pack/export", bytes.NewReader(exportBody))
	eReq.Header.Set("Content-Type", "application/json")
	eW := httptest.NewRecorder()
	srv.ServeHTTP(eW, eReq)

	if eW.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", eW.Code, eW.Body.String())
	}

	// Verify file exists
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("exported file not found: %v", err)
	}

	// Import into a different URI
	importBody, _ := json.Marshal(map[string]any{
		"file_path": outFile,
		"parent":    "viking://resources/imported",
		"force":     true,
		"vectorize": false,
	})
	iReq := httptest.NewRequest("POST", "/api/v1/pack/import", bytes.NewReader(importBody))
	iReq.Header.Set("Content-Type", "application/json")
	iW := httptest.NewRecorder()
	srv.ServeHTTP(iW, iReq)

	if iW.Code != http.StatusOK {
		t.Fatalf("import status = %d, body = %s", iW.Code, iW.Body.String())
	}

	// Verify imported content
	rReq := httptest.NewRequest("GET", "/api/v1/content/read?uri=viking://resources/imported/hello.md", nil)
	rW := httptest.NewRecorder()
	srv.ServeHTTP(rW, rReq)

	if rW.Code != http.StatusOK {
		t.Fatalf("read imported status = %d", rW.Code)
	}
	var body map[string]any
	json.Unmarshal(rW.Body.Bytes(), &body)
	if body["content"] != "# Hello Pack" {
		t.Errorf("imported content = %v, want '# Hello Pack'", body["content"])
	}
}

func TestMetricsEndpoint(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("content-type = %s", ct)
	}
}
