package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	body := w.Body.String()
	if !strings.Contains(body, "Viking") {
		t.Error("missing Viking in page title")
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

// --- Authentication Tests ---

func authConfig() Config {
	cfg := DefaultConfig()
	cfg.ConsoleSecret = "test-root-api-key-12345"
	return cfg
}

func TestAuth_LoginPageServedWhenUnauthenticated(t *testing.T) {
	h := Handler(authConfig())
	req := httptest.NewRequest("GET", "/console/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "loginForm") {
		t.Error("expected login page with loginForm element")
	}
}

func TestAuth_APIReturns401WhenUnauthenticated(t *testing.T) {
	h := Handler(authConfig())
	req := httptest.NewRequest("GET", "/console/api/v1/runtime/capabilities", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "UNAUTHORIZED" {
		t.Errorf("error code = %v", errObj["code"])
	}
}

func TestAuth_HealthEndpointPublic(t *testing.T) {
	h := Handler(authConfig())
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("health should be public, got status %d", w.Code)
	}
}

func TestAuth_AuthEndpointsPublic(t *testing.T) {
	h := Handler(authConfig())
	req := httptest.NewRequest("GET", "/console/api/v1/auth/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("auth/status should be public, got status %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["auth_required"] != true {
		t.Error("auth_required should be true when ConsoleSecret is set")
	}
	if body["authenticated"] != false {
		t.Error("should not be authenticated without session")
	}
}

func TestAuth_LoginSuccess(t *testing.T) {
	cfg := authConfig()
	h := Handler(cfg)

	req := httptest.NewRequest("POST", "/console/api/v1/auth/login",
		strings.NewReader(`{"key":"test-root-api-key-12345"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "ov_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected ov_session cookie to be set")
	}
	if sessionCookie.HttpOnly != true {
		t.Error("session cookie should be HttpOnly")
	}
}

func TestAuth_LoginWrongKey(t *testing.T) {
	h := Handler(authConfig())
	req := httptest.NewRequest("POST", "/console/api/v1/auth/login",
		strings.NewReader(`{"key":"wrong-key"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAuth_SessionCookieGrantsAccess(t *testing.T) {
	cfg := authConfig()
	h := Handler(cfg)

	// Login first
	loginReq := httptest.NewRequest("POST", "/console/api/v1/auth/login",
		strings.NewReader(`{"key":"test-root-api-key-12345"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	h.ServeHTTP(loginW, loginReq)

	var sessionToken string
	for _, c := range loginW.Result().Cookies() {
		if c.Name == "ov_session" {
			sessionToken = c.Value
		}
	}

	// Use session to access protected route
	req := httptest.NewRequest("GET", "/console/api/v1/runtime/capabilities", nil)
	req.AddCookie(&http.Cookie{Name: "ov_session", Value: sessionToken})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 with valid session", w.Code)
	}
}

func TestAuth_ApiKeyHeaderGrantsAccess(t *testing.T) {
	cfg := authConfig()
	h := Handler(cfg)

	req := httptest.NewRequest("GET", "/console/api/v1/runtime/capabilities", nil)
	req.Header.Set("X-Api-Key", "test-root-api-key-12345")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 with valid X-Api-Key", w.Code)
	}
}

func TestAuth_BearerTokenGrantsAccess(t *testing.T) {
	cfg := authConfig()
	h := Handler(cfg)

	req := httptest.NewRequest("GET", "/console/api/v1/runtime/capabilities", nil)
	req.Header.Set("Authorization", "Bearer test-root-api-key-12345")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 with valid Bearer token", w.Code)
	}
}

func TestAuth_Logout(t *testing.T) {
	cfg := authConfig()
	h := Handler(cfg)

	// Login
	loginReq := httptest.NewRequest("POST", "/console/api/v1/auth/login",
		strings.NewReader(`{"key":"test-root-api-key-12345"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	h.ServeHTTP(loginW, loginReq)

	var sessionToken string
	for _, c := range loginW.Result().Cookies() {
		if c.Name == "ov_session" {
			sessionToken = c.Value
		}
	}

	// Logout
	logoutReq := httptest.NewRequest("POST", "/console/api/v1/auth/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: "ov_session", Value: sessionToken})
	logoutW := httptest.NewRecorder()
	h.ServeHTTP(logoutW, logoutReq)

	if logoutW.Code != 200 {
		t.Fatalf("logout status = %d", logoutW.Code)
	}

	// Verify old session is invalidated
	req := httptest.NewRequest("GET", "/console/api/v1/runtime/capabilities", nil)
	req.AddCookie(&http.Cookie{Name: "ov_session", Value: sessionToken})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d after logout, want 401", w.Code)
	}
}

func TestAuth_OPTIONSIsPublic(t *testing.T) {
	h := Handler(authConfig())
	req := httptest.NewRequest("OPTIONS", "/console/api/v1/runtime/capabilities", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Fatalf("OPTIONS should be public, got status %d", w.Code)
	}
}

func TestAuth_DisabledWhenNoSecret(t *testing.T) {
	cfg := DefaultConfig() // No ConsoleSecret
	h := Handler(cfg)

	req := httptest.NewRequest("GET", "/console/api/v1/runtime/capabilities", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 when auth disabled", w.Code)
	}
}

func TestAuth_StatusWhenDisabled(t *testing.T) {
	cfg := DefaultConfig()
	h := Handler(cfg)

	req := httptest.NewRequest("GET", "/console/api/v1/auth/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["auth_required"] != false {
		t.Error("auth_required should be false when ConsoleSecret is empty")
	}
	if body["authenticated"] != true {
		t.Error("authenticated should be true when auth is disabled")
	}
}

func TestSessionStore_Expiry(t *testing.T) {
	s := newSessionStore()
	token := s.create()
	if !s.valid(token) {
		t.Fatal("freshly created session should be valid")
	}

	// Manually expire the session
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(-1 * time.Second)
	s.mu.Unlock()

	if s.valid(token) {
		t.Error("expired session should not be valid")
	}
}

func TestSessionStore_Revoke(t *testing.T) {
	s := newSessionStore()
	token := s.create()
	s.revoke(token)
	if s.valid(token) {
		t.Error("revoked session should not be valid")
	}
}

func TestSessionStore_EmptyToken(t *testing.T) {
	s := newSessionStore()
	if s.valid("") {
		t.Error("empty token should not be valid")
	}
}

func TestSha256Hash(t *testing.T) {
	h := sha256Hash("test")
	if len(h) != 64 {
		t.Errorf("sha256 hex hash should be 64 chars, got %d", len(h))
	}
	if sha256Hash("test") != sha256Hash("test") {
		t.Error("same input should produce same hash")
	}
	if sha256Hash("a") == sha256Hash("b") {
		t.Error("different inputs should produce different hashes")
	}
}
