package console

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

// Config holds settings for the console BFF.
type Config struct {
	UpstreamBaseURL   string
	WriteEnabled      bool
	RequestTimeoutSec float64
	CORSOrigins       []string
	APIKey            string
	ConsoleSecret     string // Root API Key required to access the console
}

// sessionStore manages console authentication sessions.
type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]time.Time)}
}

func (s *sessionStore) create() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(24 * time.Hour)
	s.mu.Unlock()
	return token
}

func (s *sessionStore) valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.RLock()
	exp, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return false
	}
	return true
}

func (s *sessionStore) revoke(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		UpstreamBaseURL:   "http://127.0.0.1:1933",
		WriteEnabled:      false,
		RequestTimeoutSec: 30.0,
		CORSOrigins:       []string{"*"},
	}
}

var safePathSegment = regexp.MustCompile(`^[\w.@+\-]+$`)

var allowedForwardHeaders = map[string]bool{
	"accept":               true,
	"x-api-key":            true,
	"authorization":        true,
	"x-openviking-account": true,
	"x-openviking-user":    true,
	"x-openviking-agent":   true,
	"content-type":         true,
}

// Handler returns an http.Handler that serves the console UI and proxies API requests.
func Handler(cfg Config) http.Handler {
	mux := http.NewServeMux()
	sessions := newSessionStore()

	client := &http.Client{Timeout: time.Duration(cfg.RequestTimeoutSec * float64(time.Second))}

	// Health (always public)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "viking-console"})
	})

	// Auth endpoints (public)
	mux.HandleFunc("POST /console/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if cfg.ConsoleSecret == "" {
			writeJSON(w, 200, map[string]any{"status": "ok", "message": "auth disabled"})
			return
		}
		var body struct{ Key string `json:"key"` }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, 400, "INVALID_REQUEST", "invalid body")
			return
		}
		if !hmac.Equal([]byte(body.Key), []byte(cfg.ConsoleSecret)) {
			writeError(w, 401, "INVALID_KEY", "密钥无效")
			return
		}
		token := sessions.create()
		http.SetCookie(w, &http.Cookie{
			Name:     "ov_session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   86400,
		})
		writeJSON(w, 200, map[string]any{"status": "ok"})
	})

	mux.HandleFunc("POST /console/api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("ov_session"); err == nil {
			sessions.revoke(c.Value)
		}
		http.SetCookie(w, &http.Cookie{
			Name:   "ov_session",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
		writeJSON(w, 200, map[string]any{"status": "ok"})
	})

	mux.HandleFunc("GET /console/api/v1/auth/status", func(w http.ResponseWriter, r *http.Request) {
		if cfg.ConsoleSecret == "" {
			writeJSON(w, 200, map[string]any{"authenticated": true, "auth_required": false})
			return
		}
		c, err := r.Cookie("ov_session")
		authenticated := err == nil && sessions.valid(c.Value)
		writeJSON(w, 200, map[string]any{"authenticated": authenticated, "auth_required": true})
	})

	// Runtime capabilities
	mux.HandleFunc("GET /console/api/v1/runtime/capabilities", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"status": "ok",
			"result": runtimeCapabilities(cfg),
		})
	})

	// Read proxy routes
	readRoutes := map[string]string{
		"GET /console/api/v1/ov/fs/ls":   "/api/v1/fs/ls",
		"GET /console/api/v1/ov/fs/tree": "/api/v1/fs/tree",
		"GET /console/api/v1/ov/fs/stat": "/api/v1/fs/stat",
	}
	for pattern, upstream := range readRoutes {
		up := upstream
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			forwardRequest(client, cfg, w, r, up)
		})
	}

	mux.HandleFunc("POST /console/api/v1/ov/search/find", func(w http.ResponseWriter, r *http.Request) {
		forwardRequest(client, cfg, w, r, "/api/v1/search/find")
	})
	mux.HandleFunc("GET /console/api/v1/ov/content/read", func(w http.ResponseWriter, r *http.Request) {
		forwardRequest(client, cfg, w, r, "/api/v1/content/read")
	})

	// Observer
	mux.HandleFunc("GET /console/api/v1/ov/observer/{component}", func(w http.ResponseWriter, r *http.Request) {
		comp := r.PathValue("component")
		if !safePathSegment.MatchString(comp) {
			writeError(w, 400, "INVALID_PARAMETER", "Invalid component")
			return
		}
		forwardRequest(client, cfg, w, r, "/api/v1/observer/"+comp)
	})

	// Debug proxy
	mux.HandleFunc("GET /console/api/v1/ov/debug/health", func(w http.ResponseWriter, r *http.Request) {
		forwardRequest(client, cfg, w, r, "/api/v1/debug/health")
	})
	mux.HandleFunc("GET /console/api/v1/ov/debug/vector/scroll", func(w http.ResponseWriter, r *http.Request) {
		forwardRequest(client, cfg, w, r, "/api/v1/debug/vector/scroll")
	})
	mux.HandleFunc("GET /console/api/v1/ov/debug/vector/count", func(w http.ResponseWriter, r *http.Request) {
		forwardRequest(client, cfg, w, r, "/api/v1/debug/vector/count")
	})

	// Stats proxy
	mux.HandleFunc("GET /console/api/v1/ov/stats/memories", func(w http.ResponseWriter, r *http.Request) {
		forwardRequest(client, cfg, w, r, "/api/v1/stats/memories")
	})

	// Tasks proxy
	mux.HandleFunc("GET /console/api/v1/ov/tasks", func(w http.ResponseWriter, r *http.Request) {
		forwardRequest(client, cfg, w, r, "/api/v1/tasks")
	})
	mux.HandleFunc("GET /console/api/v1/ov/tasks/{task_id}", func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("task_id")
		if !safePathSegment.MatchString(taskID) {
			writeError(w, 400, "INVALID_PARAMETER", "Invalid task_id")
			return
		}
		forwardRequest(client, cfg, w, r, "/api/v1/tasks/"+taskID)
	})

	// Write proxy routes (gated by WriteEnabled)
	writeProxy := func(upstream string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !cfg.WriteEnabled {
				writeError(w, 403, "WRITE_DISABLED", "Console write mode is disabled")
				return
			}
			forwardRequest(client, cfg, w, r, upstream)
		}
	}

	mux.HandleFunc("POST /console/api/v1/ov/fs/mkdir", writeProxy("/api/v1/fs/mkdir"))
	mux.HandleFunc("POST /console/api/v1/ov/fs/mv", writeProxy("/api/v1/fs/mv"))
	mux.HandleFunc("DELETE /console/api/v1/ov/fs", writeProxy("/api/v1/fs"))
	mux.HandleFunc("POST /console/api/v1/ov/resources", writeProxy("/api/v1/resources"))
	mux.HandleFunc("POST /console/api/v1/ov/content/write", writeProxy("/api/v1/content/write"))

	// Static file serving for SPA
	sub, _ := fs.Sub(staticFiles, "static")
	fileServer := http.FileServer(http.FS(sub))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/console" || r.URL.Path == "/console/" {
			w.Header().Set("Cache-Control", "no-store")
			serveIndex(w)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/console/")
		if path == r.URL.Path {
			path = strings.TrimPrefix(r.URL.Path, "/")
		}

		if strings.HasPrefix(path, "api/") {
			writeError(w, 404, "NOT_FOUND", "Not found")
			return
		}

		r.URL.Path = "/" + path
		fileServer.ServeHTTP(w, r)
	})

	var handler http.Handler = mux
	if cfg.ConsoleSecret != "" {
		handler = consoleAuthMiddleware(cfg.ConsoleSecret, sessions, mux)
	}
	if len(cfg.CORSOrigins) > 0 {
		return corsMiddleware(cfg.CORSOrigins, handler)
	}
	return handler
}

func serveIndex(w http.ResponseWriter) {
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "console UI not available", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func forwardRequest(client *http.Client, cfg Config, w http.ResponseWriter, r *http.Request, upstreamPath string) {
	upstreamURL := strings.TrimRight(cfg.UpstreamBaseURL, "/") + upstreamPath
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	var body io.Reader
	if r.Body != nil && r.Method != "GET" && r.Method != "HEAD" {
		body = r.Body
	}

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, body)
	if err != nil {
		writeError(w, 502, "UPSTREAM_UNAVAILABLE", fmt.Sprintf("Failed to create request: %v", err))
		return
	}

	for key, vals := range r.Header {
		if allowedForwardHeaders[strings.ToLower(key)] {
			for _, v := range vals {
				upReq.Header.Add(key, v)
			}
		}
	}
	if cfg.APIKey != "" && upReq.Header.Get("X-API-Key") == "" {
		upReq.Header.Set("X-API-Key", cfg.APIKey)
	}

	resp, err := client.Do(upReq)
	if err != nil {
		writeError(w, 502, "UPSTREAM_UNAVAILABLE", fmt.Sprintf("Failed to reach upstream: %v", err))
		return
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func runtimeCapabilities(cfg Config) map[string]any {
	modules := []string{"fs.read", "search.find", "admin.read", "monitor.read"}
	if cfg.WriteEnabled {
		modules = append(modules, "fs.write", "admin.write", "resources.write")
	}
	return map[string]any{
		"write_enabled":   cfg.WriteEnabled,
		"allowed_modules": modules,
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, errCode, message string) {
	writeJSON(w, code, map[string]any{
		"status": "error",
		"error": map[string]any{
			"code":    errCode,
			"message": message,
		},
	})
}

func consoleAuthMiddleware(secret string, sessions *sessionStore, next http.Handler) http.Handler {
	secretHash := sha256Hash(secret)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Public paths: health, auth endpoints, OPTIONS
		if path == "/health" ||
			strings.HasPrefix(path, "/console/api/v1/auth/") ||
			r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		// Check session cookie
		if c, err := r.Cookie("ov_session"); err == nil && sessions.valid(c.Value) {
			next.ServeHTTP(w, r)
			return
		}

		// Check X-Api-Key or Authorization header (for API calls)
		apiKey := r.Header.Get("X-Api-Key")
		if apiKey == "" && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			apiKey = r.Header.Get("Authorization")[7:]
		}
		if apiKey != "" && hmac.Equal([]byte(sha256Hash(apiKey)), []byte(secretHash)) {
			next.ServeHTTP(w, r)
			return
		}

		// For API requests, return 401 JSON
		if strings.Contains(path, "/api/") {
			writeError(w, 401, "UNAUTHORIZED", "请先登录控制台")
			return
		}

		// For page requests, serve login page
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte(loginPage))
	})
}

func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func corsMiddleware(origins []string, next http.Handler) http.Handler {
	allowAll := len(origins) == 1 && origins[0] == "*"
	originSet := make(map[string]bool)
	for _, o := range origins {
		originSet[o] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowAll {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if originSet[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

const loginPage = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Viking 控制台 - 登录</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#0f172a;color:#e2e8f0;display:flex;align-items:center;justify-content:center;min-height:100vh}
.login-card{background:#1e293b;border-radius:12px;padding:40px;width:100%;max-width:400px;box-shadow:0 25px 50px rgba(0,0,0,0.25)}
.login-card h1{font-size:24px;margin-bottom:8px;color:#f8fafc}
.login-card p{color:#94a3b8;margin-bottom:24px;font-size:14px}
.form-group{margin-bottom:20px}
.form-group label{display:block;font-size:13px;color:#94a3b8;margin-bottom:6px}
.form-group input{width:100%;padding:10px 14px;border:1px solid #334155;border-radius:8px;background:#0f172a;color:#f8fafc;font-size:14px;outline:none;transition:border-color 0.2s}
.form-group input:focus{border-color:#3b82f6}
.btn{width:100%;padding:12px;border:none;border-radius:8px;background:#3b82f6;color:#fff;font-size:15px;font-weight:500;cursor:pointer;transition:background 0.2s}
.btn:hover{background:#2563eb}
.btn:disabled{background:#475569;cursor:not-allowed}
.error{color:#f87171;font-size:13px;margin-top:8px;display:none}
.logo{font-size:32px;margin-bottom:16px}
</style>
</head>
<body>
<div class="login-card">
<div class="logo">⚔️</div>
<h1>Viking 控制台</h1>
<p>请输入管理密钥以访问控制台</p>
<form id="loginForm">
<div class="form-group">
<label>API Key</label>
<input type="password" id="keyInput" placeholder="输入 Root API Key" autofocus required>
</div>
<button type="submit" class="btn" id="loginBtn">登 录</button>
<div class="error" id="errorMsg"></div>
</form>
</div>
<script>
document.getElementById('loginForm').addEventListener('submit', async function(e) {
  e.preventDefault();
  const btn = document.getElementById('loginBtn');
  const err = document.getElementById('errorMsg');
  const key = document.getElementById('keyInput').value.trim();
  if (!key) return;
  btn.disabled = true;
  btn.textContent = '验证中...';
  err.style.display = 'none';
  try {
    const resp = await fetch('/console/api/v1/auth/login', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({key: key})
    });
    const data = await resp.json();
    if (resp.ok && data.status === 'ok') {
      window.location.href = '/console/';
    } else {
      err.textContent = data.error?.message || '密钥无效';
      err.style.display = 'block';
    }
  } catch(e) {
    err.textContent = '网络错误';
    err.style.display = 'block';
  }
  btn.disabled = false;
  btn.textContent = '登 录';
});
</script>
</body>
</html>`
