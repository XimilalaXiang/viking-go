package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	h := Handler(DefaultConfig())
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["service"] != "viking-console" {
		t.Errorf("service = %s", body["service"])
	}
}

func TestRuntimeCapabilities(t *testing.T) {
	cfg := DefaultConfig()
	h := Handler(cfg)

	req := httptest.NewRequest("GET", "/console/api/v1/runtime/capabilities", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	result := body["result"].(map[string]any)
	if result["write_enabled"].(bool) != false {
		t.Error("write_enabled should be false")
	}
}

func TestRuntimeCapabilities_WriteEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WriteEnabled = true
	h := Handler(cfg)

	req := httptest.NewRequest("GET", "/console/api/v1/runtime/capabilities", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	result := body["result"].(map[string]any)
	if result["write_enabled"].(bool) != true {
		t.Error("write_enabled should be true")
	}
	modules := result["allowed_modules"].([]any)
	hasWrite := false
	for _, m := range modules {
		if m.(string) == "fs.write" {
			hasWrite = true
		}
	}
	if !hasWrite {
		t.Error("should include fs.write when write_enabled")
	}
}

func TestWriteDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WriteEnabled = false
	h := Handler(cfg)

	req := httptest.NewRequest("POST", "/console/api/v1/ov/fs/mkdir", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "WRITE_DISABLED" {
		t.Errorf("error code = %v", errObj["code"])
	}
}

func TestIndexPage(t *testing.T) {
	h := Handler(DefaultConfig())
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("content-type = %s", ct)
	}
	if !strings.Contains(w.Body.String(), "Viking Console") {
		t.Error("missing Viking Console title")
	}
}

func TestConsolePath(t *testing.T) {
	h := Handler(DefaultConfig())
	req := httptest.NewRequest("GET", "/console/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestCORSPreflight(t *testing.T) {
	h := Handler(DefaultConfig())
	req := httptest.NewRequest("OPTIONS", "/console/api/v1/runtime/capabilities", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if acao := w.Header().Get("Access-Control-Allow-Origin"); acao != "*" {
		t.Errorf("ACAO = %s", acao)
	}
}

func TestProxyUpstreamUnavailable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UpstreamBaseURL = "http://127.0.0.1:1"
	cfg.RequestTimeoutSec = 1
	h := Handler(cfg)

	req := httptest.NewRequest("GET", "/console/api/v1/ov/debug/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 502 {
		t.Fatalf("status = %d, want 502", w.Code)
	}
}

func TestProxyForward(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "path": r.URL.Path})
	}))
	defer upstream.Close()

	cfg := DefaultConfig()
	cfg.UpstreamBaseURL = upstream.URL
	h := Handler(cfg)

	req := httptest.NewRequest("GET", "/console/api/v1/ov/debug/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["path"] != "/api/v1/debug/health" {
		t.Errorf("upstream path = %v", body["path"])
	}
}
