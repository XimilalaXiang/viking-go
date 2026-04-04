package console

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
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

	client := &http.Client{Timeout: time.Duration(cfg.RequestTimeoutSec * float64(time.Second))}

	// Health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "viking-console"})
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

	if len(cfg.CORSOrigins) > 0 {
		return corsMiddleware(cfg.CORSOrigins, mux)
	}
	return mux
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
