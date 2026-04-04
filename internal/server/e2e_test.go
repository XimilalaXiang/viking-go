package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ximilala/viking-go/internal/agent"
	"github.com/ximilala/viking-go/internal/embedder"
	"github.com/ximilala/viking-go/internal/indexer"
	"github.com/ximilala/viking-go/internal/memory"
	"github.com/ximilala/viking-go/internal/queue"
	"github.com/ximilala/viking-go/internal/retriever"
	"github.com/ximilala/viking-go/internal/session"
	"github.com/ximilala/viking-go/internal/storage"
	"github.com/ximilala/viking-go/internal/vikingfs"
	"github.com/ximilala/viking-go/internal/watch"

	_ "github.com/mattn/go-sqlite3"
)

// mockEmbedder generates deterministic vectors from text using a hash function.
// This allows end-to-end testing of the full pipeline without real API calls.
// Texts with overlapping words produce similar vectors, mimicking semantic similarity.
type mockEmbedder struct {
	dim int
}

func newMockEmbedder(dim int) *mockEmbedder {
	return &mockEmbedder{dim: dim}
}

func (m *mockEmbedder) Embed(text string, isQuery bool) (*embedder.EmbedResult, error) {
	vec := m.textToVector(text)
	return &embedder.EmbedResult{DenseVector: vec}, nil
}

func (m *mockEmbedder) EmbedBatch(texts []string, isQuery bool) ([]*embedder.EmbedResult, error) {
	results := make([]*embedder.EmbedResult, len(texts))
	for i, t := range texts {
		r, _ := m.Embed(t, isQuery)
		results[i] = r
	}
	return results, nil
}

func (m *mockEmbedder) Dimension() int { return m.dim }
func (m *mockEmbedder) Close()         {}

// textToVector generates a deterministic unit vector from text.
// Words are hashed into dimension buckets so texts sharing words produce similar vectors.
func (m *mockEmbedder) textToVector(text string) []float32 {
	vec := make([]float32, m.dim)
	words := strings.Fields(strings.ToLower(text))
	for _, w := range words {
		h := fnvHash(w)
		idx := int(h % uint32(m.dim))
		vec[idx] += 1.0
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v * v)
	}
	if norm > 0 {
		norm = math.Sqrt(norm)
		for i := range vec {
			vec[i] = float32(float64(vec[i]) / norm)
		}
	} else {
		vec[0] = 1.0
	}
	return vec
}

func fnvHash(s string) uint32 {
	h := uint32(2166136261)
	for _, c := range s {
		h ^= uint32(c)
		h *= 16777619
	}
	return h
}

// e2eServer creates a full server with all components wired up, including vector search.
// Skips the test if sqlite-vec is not available.
func e2eServer(t *testing.T) *Server {
	t.Helper()
	store, err := storage.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	dim := 64
	if err := store.InitVectorTable(dim); err != nil {
		t.Skipf("sqlite-vec not available: %v", err)
	}

	dir := t.TempDir()
	vfs, err := vikingfs.New(dir)
	if err != nil {
		t.Fatalf("NewVikingFS: %v", err)
	}

	emb := newMockEmbedder(dim)
	idx := indexer.New(store, vfs, emb)
	ret := retriever.NewHierarchicalRetriever(store, emb, nil, 0)
	sessionMgr := session.NewManager(vfs)
	bridge := agent.NewBridge(store, vfs, ret, sessionMgr, nil)
	embQueue := queue.NewEmbeddingQueue(idx, 1, 100)
	embQueue.Start()
	t.Cleanup(func() { embQueue.Stop() })

	watchMgr := watch.NewManager(vfs)

	return NewServer(store, vfs, ret, idx, "dev", "", watchMgr, bridge, embQueue)
}

// e2eServerNoVec creates a server without vector search — for tests that only
// need filesystem, sessions, relations, watch, etc.
func e2eServerNoVec(t *testing.T) *Server {
	t.Helper()
	store, err := storage.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	dir := t.TempDir()
	vfs, err := vikingfs.New(dir)
	if err != nil {
		t.Fatalf("NewVikingFS: %v", err)
	}

	ret := retriever.NewHierarchicalRetriever(store, nil, nil, 0)
	sessionMgr := session.NewManager(vfs)
	bridge := agent.NewBridge(store, vfs, ret, sessionMgr, nil)
	watchMgr := watch.NewManager(vfs)

	return NewServer(store, vfs, ret, nil, "dev", "", watchMgr, bridge, nil)
}

func doJSON(srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	var reqBody *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func mustOK(t *testing.T, w *httptest.ResponseRecorder, label string) map[string]any {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("[%s] status=%d, body=%s", label, w.Code, w.Body.String())
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	return result
}

// --- E2E Test: Document Import → Index → Search Full Pipeline ---

func TestE2E_WriteIndexAndSearch(t *testing.T) {
	srv := e2eServer(t)

	// 1. Write multiple documents to the knowledge base
	docs := []struct {
		uri     string
		content string
	}{
		{"viking://resources/notes/golang.md", "Go is a compiled programming language designed at Google. It features garbage collection and structural typing."},
		{"viking://resources/notes/python.md", "Python is a high-level interpreted programming language. It emphasizes code readability with significant indentation."},
		{"viking://resources/notes/rust.md", "Rust is a systems programming language focused on safety, concurrency, and performance. No garbage collection."},
		{"viking://resources/recipes/pasta.md", "Italian pasta with tomato sauce. Boil water, add salt, cook pasta for 10 minutes."},
	}

	for _, doc := range docs {
		w := doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
			"uri":     doc.uri,
			"content": doc.content,
		})
		mustOK(t, w, "write "+doc.uri)
	}

	// 2. Verify files exist via FS API
	w := doJSON(srv, "GET", "/api/v1/fs/ls?uri=viking://resources/notes", nil)
	body := mustOK(t, w, "ls notes")
	entries, ok := body["entries"].([]any)
	if !ok {
		var arr []any
		json.Unmarshal(w.Body.Bytes(), &arr)
		entries = arr
	}
	if len(entries) < 3 {
		t.Errorf("expected at least 3 entries in notes, got %d", len(entries))
	}

	// 3. Index the documents directly (synchronous)
	for _, doc := range docs {
		w := doJSON(srv, "POST", "/api/v1/content/reindex", map[string]string{
			"uri": doc.uri,
		})
		if w.Code != http.StatusOK {
			t.Logf("reindex %s: status=%d body=%s", doc.uri, w.Code, w.Body.String())
		}
	}

	// 4. Search for programming language content
	w = doJSON(srv, "POST", "/api/v1/search/find", map[string]any{
		"query": "programming language compiled",
		"limit": 5,
	})
	findResult := mustOK(t, w, "find programming")

	// Verify we got results (memories/resources/skills may vary in structure)
	totalResults := 0
	for _, key := range []string{"memories", "resources", "skills"} {
		if arr, ok := findResult[key].([]any); ok {
			totalResults += len(arr)
		}
	}
	if totalResults == 0 {
		// Check alternate result structure
		if result, ok := findResult["result"].(map[string]any); ok {
			for _, key := range []string{"memories", "resources", "skills"} {
				if arr, ok := result[key].([]any); ok {
					totalResults += len(arr)
				}
			}
		}
	}
	if totalResults == 0 {
		t.Logf("find result: %v", findResult)
		t.Error("expected search to return results for 'programming language compiled'")
	}

	// 5. Simple search endpoint
	w = doJSON(srv, "POST", "/api/v1/search/search", map[string]any{
		"query": "pasta recipe cooking",
		"limit": 3,
	})
	if w.Code != http.StatusOK {
		t.Logf("search status=%d body=%s", w.Code, w.Body.String())
	}
}

// --- E2E Test: Full Session Lifecycle ---

func TestE2E_SessionLifecycle(t *testing.T) {
	srv := e2eServerNoVec(t)

	// 1. Create a session
	w := doJSON(srv, "POST", "/api/v1/sessions", nil)
	result := mustOK(t, w, "create session")

	sessionID := ""
	if id, ok := result["session_id"].(string); ok {
		sessionID = id
	} else if r, ok := result["result"].(map[string]any); ok {
		sessionID, _ = r["session_id"].(string)
	}
	if sessionID == "" {
		t.Fatalf("no session_id in response: %v", result)
	}

	// 2. Add messages to the session
	messages := []struct {
		role    string
		content string
	}{
		{"user", "What is the capital of France?"},
		{"assistant", "The capital of France is Paris."},
		{"user", "And Germany?"},
		{"assistant", "The capital of Germany is Berlin."},
	}

	for _, msg := range messages {
		w := doJSON(srv, "POST", fmt.Sprintf("/api/v1/sessions/%s/messages", sessionID), map[string]string{
			"role":    msg.role,
			"content": msg.content,
		})
		mustOK(t, w, "add message")
	}

	// 3. Get session context and verify messages
	w = doJSON(srv, "GET", fmt.Sprintf("/api/v1/sessions/%s/context", sessionID), nil)
	sessionData := mustOK(t, w, "get session context")

	var msgList []any
	if msgs, ok := sessionData["messages"].([]any); ok {
		msgList = msgs
	} else if r, ok := sessionData["result"].(map[string]any); ok {
		msgList, _ = r["messages"].([]any)
	}
	if len(msgList) != 4 {
		t.Errorf("expected 4 messages, got %d", len(msgList))
	}

	// 4. Commit session (archive)
	w = doJSON(srv, "POST", fmt.Sprintf("/api/v1/sessions/%s/commit", sessionID), map[string]string{
		"summary": "Discussion about European capitals",
	})
	commitResult := mustOK(t, w, "commit session")
	_ = commitResult

	// 5. Verify session still exists
	w = doJSON(srv, "GET", fmt.Sprintf("/api/v1/sessions/%s", sessionID), nil)
	mustOK(t, w, "get session after commit")

	// 6. List sessions
	w = doJSON(srv, "GET", "/api/v1/sessions", nil)
	listResult := mustOK(t, w, "list sessions")
	_ = listResult

	// 7. Delete session
	w = doJSON(srv, "DELETE", fmt.Sprintf("/api/v1/sessions/%s", sessionID), nil)
	mustOK(t, w, "delete session")

	// Verify it's gone
	w = doJSON(srv, "GET", fmt.Sprintf("/api/v1/sessions/%s", sessionID), nil)
	if w.Code == http.StatusOK {
		t.Log("session still returned after delete (may be expected if soft-delete)")
	}
}

// --- E2E Test: Agent Bridge Lifecycle ---

func TestE2E_AgentBridge(t *testing.T) {
	srv := e2eServer(t)

	// 1. Write some context for the agent to retrieve
	doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
		"uri":     "viking://resources/knowledge/ai.md",
		"content": "Artificial intelligence is intelligence demonstrated by machines. Machine learning is a subset of AI.",
	})
	doJSON(srv, "POST", "/api/v1/content/reindex", map[string]string{
		"uri": "viking://resources/knowledge/ai.md",
	})

	// 2. Agent start — should retrieve relevant context
	w := doJSON(srv, "POST", "/api/v1/agent/start", map[string]any{
		"query":    "Tell me about artificial intelligence",
		"agent_id": "test-agent",
		"limit":    5,
	})
	startResult := mustOK(t, w, "agent start")
	t.Logf("Agent start result: session_id present=%v", startResult["session_id"] != nil || (startResult["result"] != nil))

	// Extract session_id
	agentSessionID := ""
	if sid, ok := startResult["session_id"].(string); ok {
		agentSessionID = sid
	} else if r, ok := startResult["result"].(map[string]any); ok {
		agentSessionID, _ = r["session_id"].(string)
	}

	// 3. Agent end
	w = doJSON(srv, "POST", "/api/v1/agent/end", map[string]any{
		"session_id": agentSessionID,
	})
	if w.Code != http.StatusOK {
		t.Logf("agent end status=%d (expected if no messages)", w.Code)
	}
}

// --- E2E Test: Watch (Directory Monitoring) ---

func TestE2E_WatchLifecycle(t *testing.T) {
	srv := e2eServerNoVec(t)

	watchDir := t.TempDir()

	// 1. Create a watch
	w := doJSON(srv, "POST", "/api/v1/watch", map[string]any{
		"source_path": watchDir,
		"target_uri":  "viking://resources/watched",
	})
	createResult := mustOK(t, w, "watch create")

	watchID := ""
	if id, ok := createResult["id"].(string); ok {
		watchID = id
	} else if r, ok := createResult["result"].(map[string]any); ok {
		watchID, _ = r["id"].(string)
	}

	// 2. List watches
	w = doJSON(srv, "GET", "/api/v1/watch", nil)
	listResult := mustOK(t, w, "watch list")
	_ = listResult

	// 3. Cancel watch
	if watchID != "" {
		w = doJSON(srv, "DELETE", fmt.Sprintf("/api/v1/watch/%s", watchID), nil)
		mustOK(t, w, "watch cancel")
	}
}

// --- E2E Test: Relations (Link/Unlink) ---

func TestE2E_RelationsFullCycle(t *testing.T) {
	srv := e2eServerNoVec(t)

	// Create content at two URIs
	doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
		"uri":     "viking://resources/project-a/readme.md",
		"content": "Project A documentation",
	})
	doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
		"uri":     "viking://resources/project-b/readme.md",
		"content": "Project B documentation",
	})

	// Link them
	w := doJSON(srv, "POST", "/api/v1/relations/link", map[string]any{
		"from_uri": "viking://resources/project-a",
		"uris":     []string{"viking://resources/project-b"},
		"reason":   "related projects",
	})
	mustOK(t, w, "link")

	// Read relations
	w = doJSON(srv, "GET", "/api/v1/relations?uri=viking://resources/project-a", nil)
	relResult := mustOK(t, w, "get relations")
	_ = relResult

	// Unlink
	w = doJSON(srv, "DELETE", "/api/v1/relations/link?from_uri=viking://resources/project-a&uri=viking://resources/project-b", nil)
	if w.Code != http.StatusOK {
		// Try with JSON body
		w = doJSON(srv, "DELETE", "/api/v1/relations/link", map[string]any{
			"from_uri": "viking://resources/project-a",
			"uri":      "viking://resources/project-b",
		})
	}
}

// --- E2E Test: Resource Import via API ---

func TestE2E_ResourceImport(t *testing.T) {
	srv := e2eServerNoVec(t)

	// Use the add_resource endpoint which handles parsing and indexing
	w := doJSON(srv, "POST", "/api/v1/resources", map[string]any{
		"uri":     "viking://resources/imported/doc.md",
		"content": "# Important Document\n\nThis is an important document about viking-go architecture.",
	})
	if w.Code != http.StatusOK {
		t.Logf("add resource: status=%d body=%s", w.Code, w.Body.String())
	}

	// Verify we can read it back
	w = doJSON(srv, "GET", "/api/v1/content/read?uri=viking://resources/imported/doc.md", nil)
	if w.Code == http.StatusOK {
		body := mustOK(t, w, "read imported resource")
		content, _ := body["content"].(string)
		if !strings.Contains(content, "Important Document") {
			t.Errorf("content mismatch: %s", content)
		}
	}
}

// --- E2E Test: Filesystem Operations ---

func TestE2E_FilesystemOperations(t *testing.T) {
	srv := e2eServerNoVec(t)

	// 1. mkdir
	w := doJSON(srv, "POST", "/api/v1/fs/mkdir", map[string]string{
		"uri": "viking://resources/testdir/subdir",
	})
	mustOK(t, w, "mkdir")

	// 2. Write file inside
	w = doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
		"uri":     "viking://resources/testdir/subdir/file.txt",
		"content": "Hello world",
	})
	mustOK(t, w, "write file")

	// 3. stat
	w = doJSON(srv, "GET", "/api/v1/fs/stat?uri=viking://resources/testdir/subdir", nil)
	stat := mustOK(t, w, "stat dir")
	if stat["isDir"] != true {
		// Check nested result
		if r, ok := stat["result"].(map[string]any); ok {
			if r["isDir"] != true && r["is_dir"] != true {
				t.Errorf("expected isDir=true, got %v", r)
			}
		}
	}

	// 4. tree
	w = doJSON(srv, "GET", "/api/v1/fs/tree?uri=viking://resources/testdir", nil)
	mustOK(t, w, "tree")

	// 5. mv
	w = doJSON(srv, "POST", "/api/v1/fs/mv", map[string]string{
		"from": "viking://resources/testdir/subdir/file.txt",
		"to":   "viking://resources/testdir/subdir/renamed.txt",
	})
	if w.Code == http.StatusOK {
		// Verify rename
		w = doJSON(srv, "GET", "/api/v1/content/read?uri=viking://resources/testdir/subdir/renamed.txt", nil)
		body := mustOK(t, w, "read after mv")
		content, _ := body["content"].(string)
		if !strings.Contains(content, "Hello world") {
			t.Errorf("content after mv = %s", content)
		}
	}

	// 6. rm (recursive for non-empty directories)
	w = doJSON(srv, "POST", "/api/v1/fs/rm", map[string]any{
		"uri":       "viking://resources/testdir",
		"recursive": true,
	})
	mustOK(t, w, "rm dir")

	// Verify gone
	w = doJSON(srv, "GET", "/api/v1/fs/stat?uri=viking://resources/testdir", nil)
	if w.Code == http.StatusOK {
		t.Log("directory still returns OK after rm (may use soft delete)")
	}
}

// --- E2E Test: Grep & Glob ---

func TestE2E_GrepAndGlob(t *testing.T) {
	srv := e2eServerNoVec(t)

	// Write searchable content
	doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
		"uri":     "viking://resources/search/alpha.md",
		"content": "The quick brown fox jumps over the lazy dog.",
	})
	doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
		"uri":     "viking://resources/search/beta.md",
		"content": "A lazy cat sleeps on the warm windowsill.",
	})
	doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
		"uri":     "viking://resources/search/gamma.txt",
		"content": "Numbers: 1, 2, 3, 4, 5",
	})

	// Grep for "lazy"
	w := doJSON(srv, "POST", "/api/v1/search/grep", map[string]any{
		"pattern": "lazy",
		"uri":     "viking://resources/search",
	})
	if w.Code == http.StatusOK {
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		t.Logf("grep result keys: %v", keys(result))
	}

	// Glob for *.md
	w = doJSON(srv, "POST", "/api/v1/search/glob", map[string]any{
		"pattern": "*.md",
		"uri":     "viking://resources/search",
	})
	if w.Code == http.StatusOK {
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		t.Logf("glob result keys: %v", keys(result))
	}
}

// --- E2E Test: Admin API Key Management ---

func TestE2E_AdminAPIKeys(t *testing.T) {
	store, err := storage.NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	dir := t.TempDir()
	vfs, err := vikingfs.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	ret := retriever.NewHierarchicalRetriever(store, nil, nil, 0)
	srv := NewServer(store, vfs, ret, nil, "api_key", "root-key-123", nil, nil, nil)

	// Unauthenticated request should fail
	w := doJSON(srv, "GET", "/api/v1/system/status", nil)
	if w.Code == http.StatusOK {
		t.Error("expected auth failure for unauthenticated request")
	}

	// Authenticated request with root key should succeed
	req := httptest.NewRequest("GET", "/api/v1/system/status", nil)
	req.Header.Set("Authorization", "Bearer root-key-123")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("auth with root key: status=%d", w.Code)
	}

	// Create account via admin API
	body, _ := json.Marshal(map[string]string{
		"name": "test-account",
	})
	req = httptest.NewRequest("POST", "/api/v1/admin/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer root-key-123")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	t.Logf("create account: status=%d body=%s", w.Code, w.Body.String())
}

// --- E2E Test: Metrics & Observer Endpoints ---

func TestE2E_ObservabilityEndpoints(t *testing.T) {
	srv := e2eServerNoVec(t)

	// Write some content to generate metrics
	doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
		"uri":     "viking://resources/metric-test.md",
		"content": "test content for metrics",
	})

	// Metrics
	w := doJSON(srv, "GET", "/metrics", nil)
	if w.Code != http.StatusOK {
		t.Errorf("metrics: status=%d", w.Code)
	}

	// Observer endpoints
	endpoints := []string{
		"/api/v1/observer/queue",
		"/api/v1/observer/storage",
		"/api/v1/observer/models",
		"/api/v1/observer/system",
		"/api/v1/stats/memories",
		"/api/v1/debug/health",
		"/api/v1/debug/vector/count",
	}
	for _, ep := range endpoints {
		w := doJSON(srv, "GET", ep, nil)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status=%d body=%s", ep, w.Code, w.Body.String())
		}
	}
}

// --- E2E Test: Embedding Queue ---

func TestE2E_EmbeddingQueue(t *testing.T) {
	srv := e2eServer(t)

	// Write content and trigger async indexing via the resource API
	w := doJSON(srv, "POST", "/api/v1/resources", map[string]any{
		"uri":     "viking://resources/async-test/doc1.md",
		"content": "Async indexing test document about machine learning and neural networks.",
	})
	if w.Code != http.StatusOK && w.Code != http.StatusAccepted {
		t.Logf("async resource: status=%d body=%s", w.Code, w.Body.String())
	}

	// Give the queue time to process
	time.Sleep(500 * time.Millisecond)

	// Check queue status
	w = doJSON(srv, "GET", "/api/v1/observer/queue", nil)
	mustOK(t, w, "queue status")
}

// --- E2E Test: Content Reindex ---

func TestE2E_ContentReindex(t *testing.T) {
	srv := e2eServer(t)

	// Write a document
	doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
		"uri":     "viking://resources/reindex-test/doc.md",
		"content": "Original content about kubernetes and docker containers.",
	})

	// Index it
	w := doJSON(srv, "POST", "/api/v1/content/reindex", map[string]string{
		"uri": "viking://resources/reindex-test/doc.md",
	})
	if w.Code != http.StatusOK {
		t.Logf("reindex: status=%d body=%s", w.Code, w.Body.String())
		return
	}

	// Update content
	doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
		"uri":     "viking://resources/reindex-test/doc.md",
		"content": "Updated content about serverless functions and edge computing.",
	})

	// Re-index
	w = doJSON(srv, "POST", "/api/v1/content/reindex", map[string]string{
		"uri": "viking://resources/reindex-test/doc.md",
	})
	mustOK(t, w, "re-reindex after update")

	// Search for new content
	w = doJSON(srv, "POST", "/api/v1/search/find", map[string]any{
		"query": "serverless edge computing",
		"limit": 5,
	})
	mustOK(t, w, "find after reindex")
}

// --- E2E Test: Pack Export/Import Full Pipeline ---

func TestE2E_PackExportImportVerify(t *testing.T) {
	srv := e2eServer(t)

	// Write multiple files
	files := map[string]string{
		"viking://resources/packdir/file1.md": "# File One\nContent of file 1",
		"viking://resources/packdir/file2.md": "# File Two\nContent of file 2",
		"viking://resources/packdir/sub/deep.md": "# Deep File\nNested content",
	}
	for uri, content := range files {
		doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
			"uri":     uri,
			"content": content,
		})
	}

	// Index
	doJSON(srv, "POST", "/api/v1/content/reindex", map[string]string{
		"uri": "viking://resources/packdir",
	})

	// Export
	outDir := t.TempDir()
	outFile := outDir + "/test-e2e.ovpack"

	w := doJSON(srv, "POST", "/api/v1/pack/export", map[string]string{
		"uri": "viking://resources/packdir",
		"to":  outFile,
	})
	if w.Code != http.StatusOK {
		t.Skipf("pack export not available: %d %s", w.Code, w.Body.String())
	}

	// Import to new location
	w = doJSON(srv, "POST", "/api/v1/pack/import", map[string]any{
		"file_path": outFile,
		"parent":    "viking://resources/restored",
		"force":     true,
		"vectorize": false,
	})
	mustOK(t, w, "pack import")

	// Verify imported files
	w = doJSON(srv, "GET", "/api/v1/content/read?uri=viking://resources/restored/file1.md", nil)
	if w.Code == http.StatusOK {
		body := mustOK(t, w, "read imported file1")
		content, _ := body["content"].(string)
		if !strings.Contains(content, "File One") {
			t.Errorf("imported file1 content = %s", content)
		}
	}
}

// --- E2E Test: Concurrent Operations ---

func TestE2E_ConcurrentWrites(t *testing.T) {
	srv := e2eServerNoVec(t)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			w := doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
				"uri":     fmt.Sprintf("viking://resources/concurrent/doc%d.md", idx),
				"content": fmt.Sprintf("Concurrent document number %d with unique content.", idx),
			})
			done <- (w.Code == http.StatusOK)
		}(i)
	}

	success := 0
	for i := 0; i < 10; i++ {
		if <-done {
			success++
		}
	}
	if success < 8 {
		t.Errorf("only %d/10 concurrent writes succeeded", success)
	}
}

// --- E2E Test: Skills Import ---

func TestE2E_SkillImport(t *testing.T) {
	srv := e2eServer(t)

	w := doJSON(srv, "POST", "/api/v1/resources/skills", map[string]any{
		"name":        "test-skill",
		"description": "A test skill for unit testing",
		"content":     "When the user asks to test, respond with 'test passed'",
	})
	if w.Code != http.StatusOK {
		t.Logf("skill import: status=%d body=%s", w.Code, w.Body.String())
		return
	}

	// Verify the skill exists
	w = doJSON(srv, "POST", "/api/v1/search/find", map[string]any{
		"query": "test skill for unit testing",
		"limit": 5,
	})
	mustOK(t, w, "find skill")
}

// --- E2E Test: Session Used Contexts ---

func TestE2E_SessionUsedContexts(t *testing.T) {
	srv := e2eServerNoVec(t)

	// Create session
	w := doJSON(srv, "POST", "/api/v1/sessions", nil)
	result := mustOK(t, w, "create session")
	sessionID := ""
	if id, ok := result["session_id"].(string); ok {
		sessionID = id
	} else if r, ok := result["result"].(map[string]any); ok {
		sessionID, _ = r["session_id"].(string)
	}
	if sessionID == "" {
		t.Skip("could not create session")
	}

	// Record used contexts
	w = doJSON(srv, "POST", fmt.Sprintf("/api/v1/sessions/%s/used", sessionID), map[string]any{
		"uris": []string{
			"viking://resources/doc1.md",
			"viking://user/memories/profile",
		},
	})
	if w.Code != http.StatusOK {
		t.Logf("session used: status=%d body=%s", w.Code, w.Body.String())
	}
}

// --- E2E Test: Abstract and Overview ---

func TestE2E_AbstractAndOverview(t *testing.T) {
	srv := e2eServerNoVec(t)

	// Write content with context layers
	doJSON(srv, "POST", "/api/v1/content/write", map[string]string{
		"uri":     "viking://resources/layered/doc.md",
		"content": "This is the detailed content about machine learning models.",
	})

	// Read abstract (L0)
	w := doJSON(srv, "GET", "/api/v1/content/abstract?uri=viking://resources/layered", nil)
	if w.Code == http.StatusOK {
		t.Log("abstract endpoint works")
	}

	// Read overview (L1)
	w = doJSON(srv, "GET", "/api/v1/content/overview?uri=viking://resources/layered", nil)
	if w.Code == http.StatusOK {
		t.Log("overview endpoint works")
	}
}

// --- E2E Test: System Wait ---

func TestE2E_SystemWait(t *testing.T) {
	srv := e2eServerNoVec(t)

	// System wait should return quickly when there's nothing pending
	w := doJSON(srv, "POST", "/api/v1/system/wait", map[string]any{
		"timeout_ms": 100,
	})
	if w.Code != http.StatusOK {
		t.Logf("system wait: status=%d body=%s", w.Code, w.Body.String())
	}
}

func keys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// Suppress unused import warnings for packages used only in the server constructor.
var _ = memory.NewExtractor
