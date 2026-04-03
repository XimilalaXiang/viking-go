package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/indexer"
	"github.com/ximilala/viking-go/internal/retriever"
	"github.com/ximilala/viking-go/internal/session"
	"github.com/ximilala/viking-go/internal/storage"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

// Server is the HTTP API server for viking-go.
type Server struct {
	store      *storage.Store
	vfs        *vikingfs.VikingFS
	retriever  *retriever.HierarchicalRetriever
	indexer    *indexer.Indexer
	sessionMgr *session.Manager
	mux        *http.ServeMux
	authMode   string
	rootKey    string
}

// NewServer creates a new API server.
func NewServer(store *storage.Store, vfs *vikingfs.VikingFS, ret *retriever.HierarchicalRetriever, idx *indexer.Indexer, authMode, rootKey string) *Server {
	s := &Server{
		store:      store,
		vfs:        vfs,
		retriever:  ret,
		indexer:    idx,
		sessionMgr: session.NewManager(vfs),
		mux:        http.NewServeMux(),
		authMode:   authMode,
		rootKey:    rootKey,
	}
	s.registerRoutes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.mux.ServeHTTP(w, r)
	log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	log.Printf("viking-go API listening on %s", addr)
	return http.ListenAndServe(addr, s)
}

func (s *Server) registerRoutes() {
	// System
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /api/v1/system/status", s.withAuth(s.handleStatus))

	// Search
	s.mux.HandleFunc("POST /api/v1/search/find", s.withAuth(s.handleFind))
	s.mux.HandleFunc("POST /api/v1/search/search", s.withAuth(s.handleSearch))

	// Content
	s.mux.HandleFunc("GET /api/v1/content/read", s.withAuth(s.handleRead))
	s.mux.HandleFunc("GET /api/v1/content/abstract", s.withAuth(s.handleAbstract))
	s.mux.HandleFunc("GET /api/v1/content/overview", s.withAuth(s.handleOverview))
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
	s.mux.HandleFunc("DELETE /api/v1/sessions/{id}", s.withAuth(s.handleDeleteSession))

	// Relations
	s.mux.HandleFunc("GET /api/v1/relations", s.withAuth(s.handleRelations))
	s.mux.HandleFunc("POST /api/v1/relations/link", s.withAuth(s.handleLink))
	s.mux.HandleFunc("DELETE /api/v1/relations/link", s.withAuth(s.handleUnlink))
}

// --- Auth middleware ---

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authMode == "dev" {
			next(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth == "" {
			writeError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}
		key := strings.TrimPrefix(auth, "Bearer ")
		if key != s.rootKey {
			writeError(w, http.StatusForbidden, "invalid API key")
			return
		}
		next(w, r)
	}
}

func (s *Server) reqCtx(r *http.Request) *ctx.RequestContext {
	accountID := r.Header.Get("X-Account-ID")
	userID := r.Header.Get("X-User-ID")
	agentID := r.Header.Get("X-Agent-ID")

	if accountID == "" {
		accountID = "default"
	}
	if userID == "" {
		userID = "default"
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
	if s.authMode == "dev" {
		role = ctx.RoleRoot
	}
	return ctx.NewRequestContext(user, role)
}

// --- System handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "viking-go",
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
	URI string `json:"uri"`
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
	result, err := s.indexer.IndexDirectory(req.URI, s.reqCtx(r))
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

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.sessionMgr.Delete(id, s.reqCtx(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
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
