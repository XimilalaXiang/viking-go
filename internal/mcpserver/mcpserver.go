package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/indexer"
	"github.com/ximilala/viking-go/internal/queue"
	"github.com/ximilala/viking-go/internal/retriever"
	"github.com/ximilala/viking-go/internal/storage"
	"github.com/ximilala/viking-go/internal/vikingfs"
	"github.com/ximilala/viking-go/internal/watch"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer wraps the viking-go services and exposes them as MCP tools.
type MCPServer struct {
	store     *storage.Store
	vfs       *vikingfs.VikingFS
	retriever *retriever.HierarchicalRetriever
	indexer   *indexer.Indexer
	watchMgr  *watch.Manager
	embQueue  *queue.EmbeddingQueue
	mcpSrv    *server.MCPServer
}

// New creates a new MCPServer with all viking-go MCP tools registered.
func New(store *storage.Store, vfs *vikingfs.VikingFS, ret *retriever.HierarchicalRetriever, idx *indexer.Indexer, watchMgr *watch.Manager, embQueue *queue.EmbeddingQueue) *MCPServer {
	mcpSrv := server.NewMCPServer("viking-go", "0.1.0",
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, false),
	)

	ms := &MCPServer{
		store:     store,
		vfs:       vfs,
		retriever: ret,
		indexer:   idx,
		watchMgr:  watchMgr,
		embQueue:  embQueue,
		mcpSrv:    mcpSrv,
	}

	ms.registerTools()
	ms.registerResources()
	return ms
}

// MCPServerInstance returns the underlying MCP server for transport binding.
func (ms *MCPServer) MCPServerInstance() *server.MCPServer {
	return ms.mcpSrv
}

func (ms *MCPServer) defaultReqCtx() *ctx.RequestContext {
	user := &ctx.UserIdentifier{
		AccountID: "default",
		UserID:    "default",
		AgentID:   "default",
	}
	return ctx.NewRequestContext(user, ctx.RoleRoot)
}

func (ms *MCPServer) registerTools() {
	// --- query: Hierarchical context retrieval ---
	ms.mcpSrv.AddTool(
		mcp.NewTool("query",
			mcp.WithDescription("Search the viking-go context database using hierarchical directory-recursive retrieval. Returns memories, resources, and skills ranked by relevance."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Natural language query to search for")),
			mcp.WithString("target_uri", mcp.Description("Optional viking:// URI to scope the search (e.g. viking://resources, viking://user/default/memories)")),
			mcp.WithNumber("limit", mcp.Description("Maximum number of results to return (default 10)")),
		),
		ms.handleQuery,
	)

	// --- search: Simpler flat vector search ---
	ms.mcpSrv.AddTool(
		mcp.NewTool("search",
			mcp.WithDescription("Perform a flat semantic search across all indexed content. Faster but less precise than query."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Natural language search query")),
			mcp.WithString("context_type", mcp.Description("Filter by type: memory, resource, or skill")),
			mcp.WithNumber("limit", mcp.Description("Maximum number of results (default 10)")),
		),
		ms.handleSearch,
	)

	// --- add_resource: Import content into the knowledge base ---
	ms.mcpSrv.AddTool(
		mcp.NewTool("add_resource",
			mcp.WithDescription("Write content to the viking-go knowledge base at a given URI, then index it for semantic search."),
			mcp.WithString("uri", mcp.Required(), mcp.Description("Target viking:// URI (e.g. viking://resources/notes/my-note)")),
			mcp.WithString("content", mcp.Required(), mcp.Description("The text content to store")),
			mcp.WithBoolean("reindex", mcp.Description("Whether to immediately vectorize and index the content (default true)")),
		),
		ms.handleAddResource,
	)

	// --- read_resource: Read content by URI ---
	ms.mcpSrv.AddTool(
		mcp.NewTool("read",
			mcp.WithDescription("Read the content of a resource at a given viking:// URI."),
			mcp.WithString("uri", mcp.Required(), mcp.Description("The viking:// URI to read")),
			mcp.WithString("level", mcp.Description("Detail level: abstract (L0), overview (L1), or full (L2, default)")),
		),
		ms.handleRead,
	)

	// --- list_directory: Browse the knowledge filesystem ---
	ms.mcpSrv.AddTool(
		mcp.NewTool("list_directory",
			mcp.WithDescription("List contents of a viking:// directory. Use to explore the knowledge base structure."),
			mcp.WithString("uri", mcp.Description("Directory URI to list (default: viking:// root)")),
		),
		ms.handleListDirectory,
	)

	// --- tree: Show directory tree ---
	ms.mcpSrv.AddTool(
		mcp.NewTool("tree",
			mcp.WithDescription("Show the directory tree structure of the viking-go knowledge base."),
			mcp.WithString("uri", mcp.Description("Root URI for the tree (default: viking://)")),
			mcp.WithNumber("depth", mcp.Description("Maximum tree depth (default 3)")),
		),
		ms.handleTree,
	)

	// --- status: System status ---
	ms.mcpSrv.AddTool(
		mcp.NewTool("status",
			mcp.WithDescription("Get viking-go system status including storage statistics."),
		),
		ms.handleStatus,
	)

	// --- queue_status: Embedding queue status ---
	ms.mcpSrv.AddTool(
		mcp.NewTool("queue_status",
			mcp.WithDescription("Get the status of the background embedding queue (pending, running, completed, failed jobs)."),
		),
		ms.handleQueueStatus,
	)

	// --- watch_create: Create a directory watch task ---
	ms.mcpSrv.AddTool(
		mcp.NewTool("watch_create",
			mcp.WithDescription("Create a watch task to monitor a local directory for changes and automatically sync to the knowledge base."),
			mcp.WithString("source_path", mcp.Required(), mcp.Description("Local directory path to watch (e.g. /data/obsidian-vault)")),
			mcp.WithString("target_uri", mcp.Required(), mcp.Description("Target viking:// URI to sync into (e.g. viking://resources/obsidian)")),
			mcp.WithNumber("interval_minutes", mcp.Description("Sync interval in minutes (default 60)")),
			mcp.WithString("reason", mcp.Description("Reason for monitoring")),
		),
		ms.handleWatchCreate,
	)

	// --- watch_list: List watch tasks ---
	ms.mcpSrv.AddTool(
		mcp.NewTool("watch_list",
			mcp.WithDescription("List all directory watch tasks."),
			mcp.WithBoolean("active_only", mcp.Description("Only show active tasks (default true)")),
		),
		ms.handleWatchList,
	)

	// --- watch_cancel: Cancel a watch task ---
	ms.mcpSrv.AddTool(
		mcp.NewTool("watch_cancel",
			mcp.WithDescription("Cancel (deactivate) a watch task by ID."),
			mcp.WithString("task_id", mcp.Required(), mcp.Description("The watch task ID to cancel")),
		),
		ms.handleWatchCancel,
	)
}

func (ms *MCPServer) registerResources() {
	ms.mcpSrv.AddResource(
		mcp.NewResource(
			"viking://overview",
			"Viking-Go Knowledge Base Overview",
			mcp.WithResourceDescription("High-level overview of the viking-go knowledge base structure and statistics"),
			mcp.WithMIMEType("text/plain"),
		),
		ms.handleOverviewResource,
	)
}

// --- Tool handlers ---

func (ms *MCPServer) handleQuery(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := stringArg(req, "query")
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	targetURI := stringArg(req, "target_uri")
	limit := intArg(req, "limit", 10)

	contextType := vikingfs.InferContextType(targetURI)
	var targetDirs []string
	if targetURI != "" {
		targetDirs = []string{targetURI}
	}

	tq := retriever.TypedQuery{
		Query:             query,
		ContextType:       contextType,
		TargetDirectories: targetDirs,
	}

	result, err := ms.retriever.Retrieve(tq, ms.defaultReqCtx(), limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("retrieval failed: %v", err)), nil
	}

	fr := categorizeFindResult(result.MatchedContexts)
	out := formatFindResult(fr, query)
	return mcp.NewToolResultText(out), nil
}

func (ms *MCPServer) handleSearch(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := stringArg(req, "query")
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	contextType := stringArg(req, "context_type")
	limit := intArg(req, "limit", 10)

	tq := retriever.TypedQuery{
		Query:       query,
		ContextType: contextType,
	}

	result, err := ms.retriever.Retrieve(tq, ms.defaultReqCtx(), limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
	}

	out := formatSearchResults(result.MatchedContexts, query)
	return mcp.NewToolResultText(out), nil
}

func (ms *MCPServer) handleAddResource(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	uri := stringArg(req, "uri")
	content := stringArg(req, "content")
	if uri == "" || content == "" {
		return mcp.NewToolResultError("uri and content are required"), nil
	}

	reqCtx := ms.defaultReqCtx()
	if err := ms.vfs.WriteString(uri, content, reqCtx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("write failed: %v", err)), nil
	}

	reindex := boolArg(req, "reindex", true)
	if reindex {
		parentURI := parentOf(uri)
		if parentURI == "" {
			parentURI = uri
		}

		// Prefer async queue if available
		if ms.embQueue != nil {
			if err := ms.embQueue.Enqueue(parentURI, reqCtx); err != nil {
				log.Printf("[MCP] queue enqueue warning: %v", err)
			} else {
				return mcp.NewToolResultText(fmt.Sprintf("Content written to %s. Indexing queued (async).", uri)), nil
			}
		}

		// Fallback to sync indexing
		if ms.indexer != nil {
			idxResult, err := ms.indexer.IndexDirectory(parentURI, reqCtx)
			if err != nil {
				log.Printf("[MCP] reindex warning: %v", err)
				return mcp.NewToolResultText(fmt.Sprintf("Content written to %s. Reindex failed: %v", uri, err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Content written to %s and indexed (%d items indexed, %d skipped)", uri, idxResult.Indexed, idxResult.Skipped)), nil
		}
	}

	return mcp.NewToolResultText(fmt.Sprintf("Content written to %s", uri)), nil
}

func (ms *MCPServer) handleRead(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	uri := stringArg(req, "uri")
	if uri == "" {
		return mcp.NewToolResultError("uri is required"), nil
	}

	level := stringArg(req, "level")
	reqCtx := ms.defaultReqCtx()

	var content string
	var err error

	switch level {
	case "abstract", "l0":
		content, err = ms.vfs.Abstract(uri, reqCtx)
	case "overview", "l1":
		content, err = ms.vfs.Overview(uri, reqCtx)
	default:
		content, err = ms.vfs.ReadFile(uri, reqCtx)
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("read failed: %v", err)), nil
	}

	return mcp.NewToolResultText(content), nil
}

func (ms *MCPServer) handleListDirectory(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	uri := stringArg(req, "uri")
	if uri == "" {
		uri = "viking://"
	}

	entries, err := ms.vfs.Ls(uri, ms.defaultReqCtx())
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("ls failed: %v", err)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Contents of %s (%d entries):\n\n", uri, len(entries)))
	for _, e := range entries {
		kind := "FILE"
		if e.IsDir {
			kind = "DIR "
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s  (%s)\n", kind, e.Name, e.URI))
	}

	return mcp.NewToolResultText(sb.String()), nil
}

func (ms *MCPServer) handleTree(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	uri := stringArg(req, "uri")
	if uri == "" {
		uri = "viking://"
	}
	depth := intArg(req, "depth", 3)

	entries, err := ms.vfs.Tree(uri, 500, depth, ms.defaultReqCtx())
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("tree failed: %v", err)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Tree of %s (%d nodes, max depth %d):\n\n", uri, len(entries), depth))
	for _, e := range entries {
		indent := strings.Count(e.RelPath, "/")
		prefix := strings.Repeat("  ", indent)
		kind := ""
		if e.IsDir {
			kind = "/"
		}
		line := fmt.Sprintf("%s%s%s", prefix, e.Name, kind)
		if e.Abstract != "" {
			line += fmt.Sprintf("  -- %s", truncate(e.Abstract, 80))
		}
		sb.WriteString(line + "\n")
	}

	return mcp.NewToolResultText(sb.String()), nil
}

func (ms *MCPServer) handleStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	stats, err := ms.store.Stats()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("stats failed: %v", err)), nil
	}

	b, _ := json.MarshalIndent(stats, "", "  ")
	return mcp.NewToolResultText(fmt.Sprintf("Viking-Go Status:\n%s", string(b))), nil
}

// --- Queue handler ---

func (ms *MCPServer) handleQueueStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if ms.embQueue == nil {
		return mcp.NewToolResultText("Embedding queue not initialized (no embedder configured)"), nil
	}

	stats := ms.embQueue.Stats()
	return mcp.NewToolResultText(fmt.Sprintf(
		"Embedding Queue Status:\n  Pending: %d\n  Running: %d\n  Completed: %d\n  Failed: %d",
		stats.Pending, stats.Running, stats.Completed, stats.Failed,
	)), nil
}

// --- Watch handlers ---

func (ms *MCPServer) handleWatchCreate(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sourcePath := stringArg(req, "source_path")
	targetURI := stringArg(req, "target_uri")
	if sourcePath == "" || targetURI == "" {
		return mcp.NewToolResultError("source_path and target_uri are required"), nil
	}

	if ms.watchMgr == nil {
		return mcp.NewToolResultError("watch manager not initialized"), nil
	}

	interval := float64(intArg(req, "interval_minutes", 60))
	reason := stringArg(req, "reason")

	id, err := ms.watchMgr.Create(sourcePath, targetURI, reason, interval, true)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create watch failed: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Watch task created: %s\nSource: %s\nTarget: %s\nInterval: %.0f minutes", id, sourcePath, targetURI, interval)), nil
}

func (ms *MCPServer) handleWatchList(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if ms.watchMgr == nil {
		return mcp.NewToolResultError("watch manager not initialized"), nil
	}

	activeOnly := boolArg(req, "active_only", true)
	tasks := ms.watchMgr.List(activeOnly)

	if len(tasks) == 0 {
		return mcp.NewToolResultText("No watch tasks found."), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Watch tasks (%d):\n\n", len(tasks)))
	for _, t := range tasks {
		status := "ACTIVE"
		if !t.Active {
			status = "INACTIVE"
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s\n", status, t.ID))
		sb.WriteString(fmt.Sprintf("    Source: %s\n", t.SourcePath))
		sb.WriteString(fmt.Sprintf("    Target: %s\n", t.TargetURI))
		sb.WriteString(fmt.Sprintf("    Interval: %.0f min | Files: %d\n", t.Interval, len(t.FileHashes)))
		if !t.LastRun.IsZero() {
			sb.WriteString(fmt.Sprintf("    Last run: %s\n", t.LastRun.Format("2006-01-02 15:04:05")))
		}
		sb.WriteString(fmt.Sprintf("    Next run: %s\n\n", t.NextRun.Format("2006-01-02 15:04:05")))
	}

	return mcp.NewToolResultText(sb.String()), nil
}

func (ms *MCPServer) handleWatchCancel(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID := stringArg(req, "task_id")
	if taskID == "" {
		return mcp.NewToolResultError("task_id is required"), nil
	}

	if ms.watchMgr == nil {
		return mcp.NewToolResultError("watch manager not initialized"), nil
	}

	if err := ms.watchMgr.Cancel(taskID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cancel failed: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Watch task %s cancelled", taskID)), nil
}

// --- Resource handler ---

func (ms *MCPServer) handleOverviewResource(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	stats, err := ms.store.Stats()
	if err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}

	b, _ := json.MarshalIndent(stats, "", "  ")
	text := fmt.Sprintf("Viking-Go Knowledge Base\n========================\n\nStatistics:\n%s", string(b))

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/plain",
			Text:     text,
		},
	}, nil
}

// --- Formatting helpers ---

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

func formatFindResult(fr *retriever.FindResult, query string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Query: %s\nTotal results: %d\n", query, fr.Total()))

	if len(fr.Memories) > 0 {
		sb.WriteString(fmt.Sprintf("\n## Memories (%d)\n", len(fr.Memories)))
		for i, m := range fr.Memories {
			sb.WriteString(fmt.Sprintf("%d. [%.2f] %s\n   %s\n", i+1, m.Score, m.URI, truncate(m.Abstract, 200)))
		}
	}
	if len(fr.Resources) > 0 {
		sb.WriteString(fmt.Sprintf("\n## Resources (%d)\n", len(fr.Resources)))
		for i, m := range fr.Resources {
			sb.WriteString(fmt.Sprintf("%d. [%.2f] %s\n   %s\n", i+1, m.Score, m.URI, truncate(m.Abstract, 200)))
		}
	}
	if len(fr.Skills) > 0 {
		sb.WriteString(fmt.Sprintf("\n## Skills (%d)\n", len(fr.Skills)))
		for i, m := range fr.Skills {
			sb.WriteString(fmt.Sprintf("%d. [%.2f] %s\n   %s\n", i+1, m.Score, m.URI, truncate(m.Abstract, 200)))
		}
	}

	return sb.String()
}

func formatSearchResults(matched []retriever.MatchedContext, query string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search: %s\nResults: %d\n\n", query, len(matched)))
	for i, m := range matched {
		sb.WriteString(fmt.Sprintf("%d. [%.2f] [%s] %s\n   %s\n", i+1, m.Score, m.ContextType, m.URI, truncate(m.Abstract, 200)))
	}
	return sb.String()
}

// --- Argument helpers (wrapping mcp.Parse* utilities) ---

func stringArg(req mcp.CallToolRequest, key string) string {
	return mcp.ParseString(req, key, "")
}

func intArg(req mcp.CallToolRequest, key string, def int) int {
	args := req.GetArguments()
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok {
		return def
	}
	f, ok := v.(float64)
	if !ok {
		return def
	}
	return int(f)
}

func boolArg(req mcp.CallToolRequest, key string, def bool) bool {
	args := req.GetArguments()
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func parentOf(uri string) string {
	uri = strings.TrimRight(uri, "/")
	idx := strings.LastIndex(uri, "/")
	if idx <= len("viking://") {
		return ""
	}
	return uri[:idx]
}
