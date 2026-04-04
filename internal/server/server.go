package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ximilala/viking-go/internal/agent"
	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/fns"
	"github.com/ximilala/viking-go/internal/indexer"
	"github.com/ximilala/viking-go/internal/metrics"
	"github.com/ximilala/viking-go/internal/queue"
	"github.com/ximilala/viking-go/internal/retriever"
	"github.com/ximilala/viking-go/internal/session"
	"github.com/ximilala/viking-go/internal/storage"
	"github.com/ximilala/viking-go/internal/vikingfs"
	"github.com/ximilala/viking-go/internal/watch"
)

// HealthReporter provides health status for observability.
type HealthReporter interface {
	Health() map[string]any
	IsAvailable() bool
}

// Server is the HTTP API server for viking-go.
type Server struct {
	store       *storage.Store
	vfs         *vikingfs.VikingFS
	retriever   *retriever.HierarchicalRetriever
	indexer     *indexer.Indexer
	sessionMgr  *session.Manager
	watchMgr    *watch.Manager
	fnsSyncer   *fns.Syncer
	agentBridge *agent.Bridge
	apiKeyMgr   *APIKeyManager
	taskTracker *TaskTracker
	embQueue    *queue.EmbeddingQueue
	mux         *http.ServeMux
	authMode    string
	rootKey     string
	startTime   time.Time

	embedderHealth HealthReporter
	llmHealth      HealthReporter
}

// NewServer creates a new API server.
func NewServer(store *storage.Store, vfs *vikingfs.VikingFS, ret *retriever.HierarchicalRetriever, idx *indexer.Indexer, authMode, rootKey string, watchMgr *watch.Manager, bridge *agent.Bridge, embQueue *queue.EmbeddingQueue) *Server {
	s := &Server{
		store:       store,
		vfs:         vfs,
		retriever:   ret,
		indexer:     idx,
		sessionMgr:  session.NewManager(vfs),
		watchMgr:    watchMgr,
		agentBridge: bridge,
		taskTracker: NewTaskTracker(),
		embQueue:    embQueue,
		mux:         http.NewServeMux(),
		authMode:    authMode,
		rootKey:     rootKey,
		startTime:   time.Now(),
	}

	if authMode == "api_key" && rootKey != "" {
		s.apiKeyMgr = NewAPIKeyManager(rootKey, vfs.RootDir())
		if err := s.apiKeyMgr.Load(); err != nil {
			log.Printf("Warning: failed to load API keys: %v", err)
		}
	}

	s.registerRoutes()
	return s
}

// SetHealthReporters attaches optional health reporters for observability.
func (s *Server) SetHealthReporters(embedder, llm HealthReporter) {
	s.embedderHealth = embedder
	s.llmHealth = llm
}

// SetFNSSyncer attaches the Fast Note Sync syncer.
func (s *Server) SetFNSSyncer(syncer *fns.Syncer) {
	s.fnsSyncer = syncer
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.mux.ServeHTTP(w, r)
	elapsed := time.Since(start)
	log.Printf("%s %s %s", r.Method, r.URL.Path, elapsed)
	metrics.Inc("viking_http_requests_total")
	metrics.Observe("viking_http_request_duration_ms", elapsed)
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	log.Printf("viking-go API listening on %s", addr)
	return http.ListenAndServe(addr, s)
}

func (s *Server) registerRoutes() {
	// System
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /ready", s.handleReady)
	s.mux.HandleFunc("GET /api/v1/system/status", s.withAuth(s.handleStatus))
	s.mux.HandleFunc("POST /api/v1/system/wait", s.withAuth(s.handleSystemWait))

	// Search
	s.mux.HandleFunc("POST /api/v1/search/find", s.withAuth(s.handleFind))
	s.mux.HandleFunc("POST /api/v1/search/search", s.withAuth(s.handleSearch))
	s.mux.HandleFunc("POST /api/v1/search/grep", s.withAuth(s.handleGrep))
	s.mux.HandleFunc("POST /api/v1/search/glob", s.withAuth(s.handleGlob))

	// Content
	s.mux.HandleFunc("GET /api/v1/content/read", s.withAuth(s.handleRead))
	s.mux.HandleFunc("GET /api/v1/content/abstract", s.withAuth(s.handleAbstract))
	s.mux.HandleFunc("GET /api/v1/content/overview", s.withAuth(s.handleOverview))
	s.mux.HandleFunc("GET /api/v1/content/download", s.withAuth(s.handleDownload))
	s.mux.HandleFunc("POST /api/v1/content/write", s.withAuth(s.handleWrite))
	s.mux.HandleFunc("POST /api/v1/content/reindex", s.withAuth(s.handleReindex))

	// Filesystem
	s.mux.HandleFunc("GET /api/v1/fs/ls", s.withAuth(s.handleLs))
	s.mux.HandleFunc("GET /api/v1/fs/tree", s.withAuth(s.handleTree))
	s.mux.HandleFunc("GET /api/v1/fs/stat", s.withAuth(s.handleStat))
	s.mux.HandleFunc("POST /api/v1/fs/mkdir", s.withAuth(s.handleMkdir))
	s.mux.HandleFunc("DELETE /api/v1/fs", s.withAuth(s.handleRmQuery))
	s.mux.HandleFunc("POST /api/v1/fs/rm", s.withAuth(s.handleRm))
	s.mux.HandleFunc("POST /api/v1/fs/mv", s.withAuth(s.handleMv))

	// Sessions
	s.mux.HandleFunc("POST /api/v1/sessions", s.withAuth(s.handleCreateSession))
	s.mux.HandleFunc("GET /api/v1/sessions", s.withAuth(s.handleListSessions))
	s.mux.HandleFunc("GET /api/v1/sessions/{id}", s.withAuth(s.handleGetSession))
	s.mux.HandleFunc("GET /api/v1/sessions/{id}/context", s.withAuth(s.handleGetSessionContext))
	s.mux.HandleFunc("POST /api/v1/sessions/{id}/messages", s.withAuth(s.handleAddMessage))
	s.mux.HandleFunc("POST /api/v1/sessions/{id}/commit", s.withAuth(s.handleCommitSession))
	s.mux.HandleFunc("POST /api/v1/sessions/{id}/extract", s.withAuth(s.handleExtractSession))
	s.mux.HandleFunc("POST /api/v1/sessions/{id}/used", s.withAuth(s.handleSessionUsed))
	s.mux.HandleFunc("GET /api/v1/sessions/{id}/archives/{archive_id}", s.withAuth(s.handleGetArchive))
	s.mux.HandleFunc("DELETE /api/v1/sessions/{id}", s.withAuth(s.handleDeleteSession))

	// Relations
	s.mux.HandleFunc("GET /api/v1/relations", s.withAuth(s.handleRelations))
	s.mux.HandleFunc("POST /api/v1/relations/link", s.withAuth(s.handleLink))
	s.mux.HandleFunc("DELETE /api/v1/relations/link", s.withAuth(s.handleUnlink))

	// Watch
	s.mux.HandleFunc("POST /api/v1/watch", s.withAuth(s.handleWatchCreate))
	s.mux.HandleFunc("GET /api/v1/watch", s.withAuth(s.handleWatchList))
	s.mux.HandleFunc("DELETE /api/v1/watch/{id}", s.withAuth(s.handleWatchCancel))

	// FNS (Fast Note Sync)
	s.mux.HandleFunc("POST /api/v1/fns/sync", s.withAuth(s.handleFNSSync))
	s.mux.HandleFunc("GET /api/v1/fns/status", s.withAuth(s.handleFNSStatus))

	// Agent bridge
	s.mux.HandleFunc("POST /api/v1/agent/start", s.withAuth(s.handleAgentStart))
	s.mux.HandleFunc("POST /api/v1/agent/end", s.withAuth(s.handleAgentEnd))
	s.mux.HandleFunc("POST /api/v1/agent/compact", s.withAuth(s.handleAgentCompact))

	// Admin (root-only)
	s.mux.HandleFunc("GET /api/v1/admin/accounts", s.withAuth(s.handleListAccounts))
	s.mux.HandleFunc("POST /api/v1/admin/accounts", s.withAuth(s.handleCreateAccount))
	s.mux.HandleFunc("GET /api/v1/admin/accounts/{account_id}/users", s.withAuth(s.handleListUsers))
	s.mux.HandleFunc("POST /api/v1/admin/accounts/{account_id}/users", s.withAuth(s.handleRegisterUser))
	s.mux.HandleFunc("PUT /api/v1/admin/accounts/{account_id}/users/{user_id}/role", s.withAuth(s.handleSetUserRole))
	s.mux.HandleFunc("DELETE /api/v1/admin/accounts/{account_id}/users/{user_id}", s.withAuth(s.handleDeleteUser))
	s.mux.HandleFunc("DELETE /api/v1/admin/accounts/{account_id}", s.withAuth(s.handleDeleteAccount))
	s.mux.HandleFunc("POST /api/v1/admin/accounts/{account_id}/users/{user_id}/key", s.withAuth(s.handleRegenerateKey))

	// Resources
	s.mux.HandleFunc("POST /api/v1/resources", s.withAuth(s.handleAddResource))
	s.mux.HandleFunc("POST /api/v1/resources/temp_upload", s.withAuth(s.handleTempUpload))
	s.mux.HandleFunc("POST /api/v1/resources/skills", s.withAuth(s.handleAddSkill))

	// Pack (export/import)
	s.mux.HandleFunc("POST /api/v1/pack/export", s.withAuth(s.handlePackExport))
	s.mux.HandleFunc("POST /api/v1/pack/import", s.withAuth(s.handlePackImport))

	// Debug
	s.mux.HandleFunc("GET /api/v1/debug/health", s.withAuth(s.handleDebugHealth))
	s.mux.HandleFunc("GET /api/v1/debug/vector/scroll", s.withAuth(s.handleDebugVectorScroll))
	s.mux.HandleFunc("GET /api/v1/debug/vector/count", s.withAuth(s.handleDebugVectorCount))

	// Observer
	s.mux.HandleFunc("GET /api/v1/observer/queue", s.withAuth(s.handleObserverQueue))
	s.mux.HandleFunc("GET /api/v1/observer/storage", s.withAuth(s.handleObserverStorage))
	s.mux.HandleFunc("GET /api/v1/observer/models", s.withAuth(s.handleObserverModels))
	s.mux.HandleFunc("GET /api/v1/observer/lock", s.withAuth(s.handleObserverLock))
	s.mux.HandleFunc("GET /api/v1/observer/retrieval", s.withAuth(s.handleObserverRetrieval))
	s.mux.HandleFunc("GET /api/v1/observer/vikingdb", s.withAuth(s.handleObserverVikingDB))
	s.mux.HandleFunc("GET /api/v1/observer/system", s.withAuth(s.handleObserverSystem))

	// Stats
	s.mux.HandleFunc("GET /api/v1/stats/memories", s.withAuth(s.handleStatsMemories))
	s.mux.HandleFunc("GET /api/v1/stats/sessions/{session_id}", s.withAuth(s.handleStatsSession))

	// Tasks
	s.mux.HandleFunc("GET /api/v1/tasks", s.withAuth(s.handleListTasks))
	s.mux.HandleFunc("GET /api/v1/tasks/{task_id}", s.withAuth(s.handleGetTask))

	// Prometheus metrics
	s.mux.Handle("GET /metrics", metrics.Handler())
}

// --- Auth middleware ---

const reqCtxKey = "viking_req_ctx"

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authMode == "dev" || s.authMode == "trusted" {
			next(w, r)
			return
		}

		rawKey := extractAPIKey(
			r.Header.Get("X-Api-Key"),
			r.Header.Get("Authorization"),
		)
		if rawKey == "" {
			writeError(w, http.StatusUnauthorized, "missing API key")
			return
		}

		if s.apiKeyMgr != nil {
			entry := s.apiKeyMgr.Authenticate(rawKey)
			if entry == nil {
				writeError(w, http.StatusForbidden, "invalid API key")
				return
			}
			r.Header.Set("X-Resolved-Role", string(entry.Role))
			if entry.AccountID != "" {
				r.Header.Set("X-Resolved-Account", entry.AccountID)
			}
			if entry.UserID != "" {
				r.Header.Set("X-Resolved-User", entry.UserID)
			}
		} else {
			if rawKey != s.rootKey {
				writeError(w, http.StatusForbidden, "invalid API key")
				return
			}
			r.Header.Set("X-Resolved-Role", string(ctx.RoleRoot))
		}

		next(w, r)
	}
}

func (s *Server) reqCtx(r *http.Request) *ctx.RequestContext {
	resolvedRole := r.Header.Get("X-Resolved-Role")
	resolvedAccount := r.Header.Get("X-Resolved-Account")
	resolvedUser := r.Header.Get("X-Resolved-User")

	accountID := resolvedAccount
	if accountID == "" {
		accountID = r.Header.Get("X-Account-ID")
	}
	if accountID == "" {
		accountID = r.Header.Get("X-OpenViking-Account")
	}
	if accountID == "" {
		accountID = "default"
	}

	userID := resolvedUser
	if userID == "" {
		userID = r.Header.Get("X-User-ID")
	}
	if userID == "" {
		userID = r.Header.Get("X-OpenViking-User")
	}
	if userID == "" {
		userID = "default"
	}

	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		agentID = r.Header.Get("X-OpenViking-Agent")
	}
	if agentID == "" {
		agentID = "default"
	}

	user := &ctx.UserIdentifier{
		AccountID: accountID,
		UserID:    userID,
		AgentID:   agentID,
	}

	role := ctx.RoleUser
	switch ctx.Role(resolvedRole) {
	case ctx.RoleRoot:
		role = ctx.RoleRoot
	case ctx.RoleAdmin:
		role = ctx.RoleAdmin
	default:
		if s.authMode == "dev" || s.authMode == "trusted" {
			role = ctx.RoleRoot
		}
	}

	return ctx.NewRequestContext(user, role)
}

func (s *Server) requireRole(r *http.Request, minRole ctx.Role) bool {
	rc := s.reqCtx(r)
	switch minRole {
	case ctx.RoleRoot:
		return rc.Role == ctx.RoleRoot
	case ctx.RoleAdmin:
		return rc.Role == ctx.RoleRoot || rc.Role == ctx.RoleAdmin
	default:
		return true
	}
}

// --- System handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"healthy": true,
		"service": "viking-go",
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}

	if _, err := s.vfs.Ls("viking://", nil); err != nil {
		checks["vikingfs"] = fmt.Sprintf("error: %v", err)
	} else {
		checks["vikingfs"] = "ok"
	}

	if s.store != nil {
		if _, err := s.store.Stats(); err != nil {
			checks["store"] = fmt.Sprintf("error: %v", err)
		} else {
			checks["store"] = "ok"
		}
	} else {
		checks["store"] = "not_configured"
	}

	if s.apiKeyMgr != nil {
		checks["api_key_manager"] = "ok"
	} else {
		checks["api_key_manager"] = "not_configured"
	}

	allOK := true
	for _, v := range checks {
		if v != "ok" && v != "not_configured" {
			allOK = false
			break
		}
	}

	status := "ready"
	code := http.StatusOK
	if !allOK {
		status = "not_ready"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{"status": status, "checks": checks})
}

func (s *Server) handleSystemWait(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Timeout *float64 `json:"timeout,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if s.embQueue != nil {
		stats := s.embQueue.Stats()
		if stats.Pending > 0 || stats.Running > 0 {
			timeout := 60.0
			if req.Timeout != nil {
				timeout = *req.Timeout
			}
			deadline := time.After(time.Duration(timeout * float64(time.Second)))
			for {
				st := s.embQueue.Stats()
				if st.Pending == 0 && st.Running == 0 {
					break
				}
				select {
				case <-deadline:
					writeJSON(w, http.StatusOK, map[string]any{
						"status": "ok",
						"result": map[string]any{"completed": false, "reason": "timeout"},
					})
					return
				case <-time.After(200 * time.Millisecond):
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{"completed": true},
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.Stats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// --- Search handlers ---

type findRequest struct {
	Query          string  `json:"query"`
	TargetURI      string  `json:"target_uri"`
	Limit          int     `json:"limit"`
	ScoreThreshold *float64 `json:"score_threshold,omitempty"`
}

func (s *Server) handleFind(w http.ResponseWriter, r *http.Request) {
	var req findRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}

	contextType := vikingfs.InferContextType(req.TargetURI)
	var targetDirs []string
	if req.TargetURI != "" {
		targetDirs = []string{req.TargetURI}
	}

	tq := retriever.TypedQuery{
		Query:            req.Query,
		ContextType:      contextType,
		TargetDirectories: targetDirs,
	}

	result, err := s.retriever.Retrieve(tq, s.reqCtx(r), req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	fr := categorizeFindResult(result.MatchedContexts)
	writeJSON(w, http.StatusOK, fr)
}

type searchRequest struct {
	Query     string   `json:"query"`
	TargetURI any      `json:"target_uri"`
	Limit     int      `json:"limit"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}

	var targetDirs []string
	switch v := req.TargetURI.(type) {
	case string:
		if v != "" {
			targetDirs = []string{v}
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				targetDirs = append(targetDirs, s)
			}
		}
	}

	contextType := ""
	if len(targetDirs) > 0 {
		contextType = vikingfs.InferContextType(targetDirs[0])
	}

	tq := retriever.TypedQuery{
		Query:            req.Query,
		ContextType:      contextType,
		TargetDirectories: targetDirs,
	}

	result, err := s.retriever.Retrieve(tq, s.reqCtx(r), req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	fr := categorizeFindResult(result.MatchedContexts)
	writeJSON(w, http.StatusOK, fr)
}

type grepRequest struct {
	URI             string `json:"uri"`
	ExcludeURI      string `json:"exclude_uri,omitempty"`
	Pattern         string `json:"pattern"`
	CaseInsensitive bool   `json:"case_insensitive"`
	NodeLimit       *int   `json:"node_limit,omitempty"`
}

func (s *Server) handleGrep(w http.ResponseWriter, r *http.Request) {
	var req grepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	limit := 1000
	if req.NodeLimit != nil {
		limit = *req.NodeLimit
	}
	matches, err := s.vfs.Grep(req.URI, req.Pattern, req.CaseInsensitive, limit, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "result": matches})
}

type globRequest struct {
	Pattern   string `json:"pattern"`
	URI       string `json:"uri"`
	NodeLimit *int   `json:"node_limit,omitempty"`
}

func (s *Server) handleGlob(w http.ResponseWriter, r *http.Request) {
	var req globRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.URI == "" {
		req.URI = "viking://"
	}
	limit := 1000
	if req.NodeLimit != nil {
		limit = *req.NodeLimit
	}
	matches, err := s.vfs.Glob(req.Pattern, req.URI, limit, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "result": matches})
}

func categorizeFindResult(matched []retriever.MatchedContext) *retriever.FindResult {
	fr := &retriever.FindResult{}
	for _, m := range matched {
		switch m.ContextType {
		case "memory":
			fr.Memories = append(fr.Memories, m)
		case "resource":
			fr.Resources = append(fr.Resources, m)
		case "skill":
			fr.Skills = append(fr.Skills, m)
		default:
			fr.Resources = append(fr.Resources, m)
		}
	}
	return fr
}

// --- Content handlers ---

func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	if uri == "" {
		writeError(w, http.StatusBadRequest, "missing uri parameter")
		return
	}
	content, err := s.vfs.ReadFile(uri, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"uri":     uri,
		"content": content,
	})
}

func (s *Server) handleAbstract(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	if uri == "" {
		writeError(w, http.StatusBadRequest, "missing uri parameter")
		return
	}
	content, err := s.vfs.Abstract(uri, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"uri": uri, "content": content})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	if uri == "" {
		writeError(w, http.StatusBadRequest, "missing uri parameter")
		return
	}
	content, err := s.vfs.Overview(uri, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"uri": uri, "content": content})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	if uri == "" {
		writeError(w, http.StatusBadRequest, "missing uri parameter")
		return
	}
	content, err := s.vfs.ReadFile(uri, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	filename := "download.txt"
	parts := strings.Split(strings.TrimRight(uri, "/"), "/")
	if len(parts) > 0 {
		filename = parts[len(parts)-1]
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Write([]byte(content))
}

type writeRequest struct {
	URI     string `json:"uri"`
	Content string `json:"content"`
}

func (s *Server) handleWrite(w http.ResponseWriter, r *http.Request) {
	var req writeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.URI == "" {
		writeError(w, http.StatusBadRequest, "missing uri")
		return
	}
	if err := s.vfs.WriteString(req.URI, req.Content, s.reqCtx(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// --- Indexer handlers ---

type reindexRequest struct {
	URI       string `json:"uri"`
	Recursive bool   `json:"recursive"`
	MaxRPM    int    `json:"max_rpm"`
}

func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	var req reindexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.URI == "" {
		writeError(w, http.StatusBadRequest, "missing uri")
		return
	}
	if s.indexer == nil {
		writeError(w, http.StatusServiceUnavailable, "indexer not available (no embedder configured)")
		return
	}

	var result *indexer.IndexResult
	var err error
	if req.Recursive {
		result, err = s.indexer.IndexDirectoryRecursive(req.URI, s.reqCtx(r), req.MaxRPM)
	} else {
		result, err = s.indexer.IndexDirectory(req.URI, s.reqCtx(r))
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// --- Filesystem handlers ---

func (s *Server) handleLs(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	if uri == "" {
		uri = "viking://"
	}
	entries, err := s.vfs.Ls(uri, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	if uri == "" {
		uri = "viking://"
	}
	nodeLimit := queryInt(r, "node_limit", 1000)
	levelLimit := queryInt(r, "level_limit", 3)

	entries, err := s.vfs.Tree(uri, nodeLimit, levelLimit, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleStat(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	if uri == "" {
		writeError(w, http.StatusBadRequest, "missing uri parameter")
		return
	}
	info, err := s.vfs.Stat(uri, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    info.Name(),
		"size":    info.Size(),
		"isDir":   info.IsDir(),
		"modTime": info.ModTime().Format(time.RFC3339),
	})
}

type mkdirRequest struct {
	URI string `json:"uri"`
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	var req mkdirRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.vfs.Mkdir(req.URI, s.reqCtx(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleRmQuery(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	recursive := r.URL.Query().Get("recursive") == "true"
	if uri == "" {
		writeError(w, http.StatusBadRequest, "missing uri parameter")
		return
	}
	if err := s.vfs.Rm(uri, recursive, s.reqCtx(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleRm(w http.ResponseWriter, r *http.Request) {
	var req rmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.URI == "" {
		writeError(w, http.StatusBadRequest, "missing uri")
		return
	}
	if err := s.vfs.Rm(req.URI, req.Recursive, s.reqCtx(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

type mvRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type rmRequest struct {
	URI       string `json:"uri"`
	Recursive bool   `json:"recursive"`
}

func (s *Server) handleMv(w http.ResponseWriter, r *http.Request) {
	var req mvRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.From == "" || req.To == "" {
		writeError(w, http.StatusBadRequest, "from and to are required")
		return
	}
	if err := s.vfs.Mv(req.From, req.To, s.reqCtx(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// --- Relations handlers ---

func (s *Server) handleRelations(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	if uri == "" {
		writeError(w, http.StatusBadRequest, "missing uri parameter")
		return
	}
	entries, err := s.vfs.Relations(uri, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

type linkRequest struct {
	FromURI string   `json:"from_uri"`
	URIs    []string `json:"uris"`
	Reason  string   `json:"reason"`
}

func (s *Server) handleLink(w http.ResponseWriter, r *http.Request) {
	var req linkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.vfs.Link(req.FromURI, req.URIs, req.Reason, s.reqCtx(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

type unlinkRequest struct {
	FromURI string `json:"from_uri"`
	URI     string `json:"uri"`
}

func (s *Server) handleUnlink(w http.ResponseWriter, r *http.Request) {
	var req unlinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.vfs.Unlink(req.FromURI, req.URI, s.reqCtx(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// --- Session handlers ---

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := s.sessionMgr.Create(s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_id": sessionID})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.sessionMgr.List(s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sessions == nil {
		sessions = []session.SessionInfo{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	info, err := s.sessionMgr.Get(id, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleGetSessionContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	data, err := s.sessionMgr.GetContext(id, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, data)
}

type addMessageRequest struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (s *Server) handleAddMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req addMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Role == "" || req.Content == "" {
		writeError(w, http.StatusBadRequest, "role and content are required")
		return
	}
	if err := s.sessionMgr.AddMessage(id, req.Role, req.Content, s.reqCtx(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

type commitRequest struct {
	Summary string `json:"summary"`
}

func (s *Server) handleCommitSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req commitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	archive, err := s.sessionMgr.Commit(id, req.Summary, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, archive)
}

func (s *Server) handleExtractSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	result, err := s.sessionMgr.Extract(id, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "result": result})
}

func (s *Server) handleSessionUsed(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Contexts []string       `json:"contexts,omitempty"`
		Skill    map[string]any `json:"skill,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	result, err := s.sessionMgr.RecordUsed(id, req.Contexts, req.Skill, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "result": result})
}

func (s *Server) handleGetArchive(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	archiveID := r.PathValue("archive_id")
	result, err := s.sessionMgr.GetArchive(sessionID, archiveID, s.reqCtx(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "result": result})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.sessionMgr.Delete(id, s.reqCtx(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// --- Watch handlers ---

type watchCreateRequest struct {
	SourcePath      string  `json:"source_path"`
	TargetURI       string  `json:"target_uri"`
	IntervalMinutes float64 `json:"interval_minutes"`
	Reason          string  `json:"reason"`
}

func (s *Server) handleWatchCreate(w http.ResponseWriter, r *http.Request) {
	if s.watchMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "watch manager not initialized")
		return
	}
	var req watchCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.SourcePath == "" || req.TargetURI == "" {
		writeError(w, http.StatusBadRequest, "source_path and target_uri are required")
		return
	}
	if req.IntervalMinutes <= 0 {
		req.IntervalMinutes = 60
	}
	id, err := s.watchMgr.Create(req.SourcePath, req.TargetURI, req.Reason, req.IntervalMinutes, true)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": id})
}

func (s *Server) handleWatchList(w http.ResponseWriter, r *http.Request) {
	if s.watchMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "watch manager not initialized")
		return
	}
	activeOnly := r.URL.Query().Get("active_only") != "false"
	tasks := s.watchMgr.List(activeOnly)
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) handleWatchCancel(w http.ResponseWriter, r *http.Request) {
	if s.watchMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "watch manager not initialized")
		return
	}
	id := r.PathValue("id")
	if err := s.watchMgr.Cancel(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
}

// --- FNS handlers ---

func (s *Server) handleFNSSync(w http.ResponseWriter, r *http.Request) {
	if s.fnsSyncer == nil {
		writeError(w, http.StatusServiceUnavailable, "FNS syncer not configured")
		return
	}

	buildIndex := true
	if r.URL.Query().Get("build_index") == "false" {
		buildIndex = false
	}

	result, err := s.fnsSyncer.Sync(buildIndex)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleFNSStatus(w http.ResponseWriter, r *http.Request) {
	if s.fnsSyncer == nil {
		writeError(w, http.StatusServiceUnavailable, "FNS syncer not configured")
		return
	}
	writeJSON(w, http.StatusOK, s.fnsSyncer.Status())
}

// --- Agent bridge handlers ---

func (s *Server) handleAgentStart(w http.ResponseWriter, r *http.Request) {
	if s.agentBridge == nil {
		writeError(w, http.StatusServiceUnavailable, "agent bridge not initialized")
		return
	}
	var req agent.StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	resp, err := s.agentBridge.BeforeAgentStart(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAgentEnd(w http.ResponseWriter, r *http.Request) {
	if s.agentBridge == nil {
		writeError(w, http.StatusServiceUnavailable, "agent bridge not initialized")
		return
	}
	var req agent.EndRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	resp, err := s.agentBridge.AfterAgentEnd(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAgentCompact(w http.ResponseWriter, r *http.Request) {
	if s.agentBridge == nil {
		writeError(w, http.StatusServiceUnavailable, "agent bridge not initialized")
		return
	}
	var req agent.CompactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	resp, err := s.agentBridge.BeforeCompaction(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Admin handlers ---

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.requireRole(r, ctx.RoleRoot) {
		writeError(w, http.StatusForbidden, "root access required")
		return
	}
	if s.apiKeyMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "API key management not enabled")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": s.apiKeyMgr.ListAccounts()})
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requireRole(r, ctx.RoleRoot) {
		writeError(w, http.StatusForbidden, "root access required")
		return
	}
	if s.apiKeyMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "API key management not enabled")
		return
	}
	var req struct {
		AccountID   string `json:"account_id"`
		AdminUserID string `json:"admin_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.AccountID == "" || req.AdminUserID == "" {
		writeError(w, http.StatusBadRequest, "account_id and admin_user_id required")
		return
	}
	key, err := s.apiKeyMgr.CreateAccount(req.AccountID, req.AdminUserID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"account_id":   req.AccountID,
		"admin_user":   req.AdminUserID,
		"api_key":      key,
	})
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireRole(r, ctx.RoleRoot) {
		writeError(w, http.StatusForbidden, "root access required")
		return
	}
	if s.apiKeyMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "API key management not enabled")
		return
	}
	accountID := r.PathValue("account_id")
	users, err := s.apiKeyMgr.ListUsers(accountID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleRegisterUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireRole(r, ctx.RoleRoot) {
		writeError(w, http.StatusForbidden, "root access required")
		return
	}
	if s.apiKeyMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "API key management not enabled")
		return
	}
	accountID := r.PathValue("account_id")
	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}
	key, err := s.apiKeyMgr.RegisterUser(accountID, req.UserID, req.Role)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"user_id": req.UserID,
		"role":    req.Role,
		"api_key": key,
	})
}

func (s *Server) handleSetUserRole(w http.ResponseWriter, r *http.Request) {
	if !s.requireRole(r, ctx.RoleRoot) {
		writeError(w, http.StatusForbidden, "root access required")
		return
	}
	if s.apiKeyMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "API key management not enabled")
		return
	}
	accountID := r.PathValue("account_id")
	userID := r.PathValue("user_id")
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.apiKeyMgr.SetUserRole(accountID, userID, req.Role); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireRole(r, ctx.RoleRoot) {
		writeError(w, http.StatusForbidden, "root access required")
		return
	}
	if s.apiKeyMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "API key management not enabled")
		return
	}
	accountID := r.PathValue("account_id")
	userID := r.PathValue("user_id")
	if err := s.apiKeyMgr.DeleteUser(accountID, userID); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requireRole(r, ctx.RoleRoot) {
		writeError(w, http.StatusForbidden, "root access required")
		return
	}
	if s.apiKeyMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "API key management not enabled")
		return
	}
	accountID := r.PathValue("account_id")
	if err := s.apiKeyMgr.DeleteAccount(accountID); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleRegenerateKey(w http.ResponseWriter, r *http.Request) {
	if !s.requireRole(r, ctx.RoleRoot) {
		writeError(w, http.StatusForbidden, "root access required")
		return
	}
	if s.apiKeyMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "API key management not enabled")
		return
	}
	accountID := r.PathValue("account_id")
	userID := r.PathValue("user_id")
	newKey, err := s.apiKeyMgr.RegenerateKey(accountID, userID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "result": map[string]any{"api_key": newKey}})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func queryInt(r *http.Request, key string, def int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

// Addr builds the listen address string.
func Addr(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}
