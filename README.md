# viking-go

Lightweight Go reimplementation of [OpenViking](https://github.com/volcengine/OpenViking) — a hierarchical context retrieval system with long-term memory, designed for AI agents.

Single binary, ~14 MB, targeting 30–80 MB runtime memory.

## Architecture

```
viking-go
├── cmd/viking-go/          # CLI entry point
├── pkg/
│   ├── uri/                # viking:// URI parsing & normalization
│   └── client/             # HTTP & local client SDK
└── internal/
    ├── config/             # JSON + env-var configuration
    ├── context/            # Context model (L0/L1/L2, types, identity)
    ├── storage/            # SQLite + sqlite-vec storage layer
    │   ├── backend.go      # Multi-backend abstraction (SQLite/HTTP/memory)
    │   ├── transaction.go  # Path-level LockManager & redo log
    │   └── filter.go       # Filter expressions (Eq/And/In/PathScope)
    ├── vikingfs/           # Local filesystem (URI → path mapping)
    ├── embedder/           # Embedding & reranking (OpenAI-compatible)
    ├── llm/                # LLM chat completion + tool calling client
    ├── retriever/          # Hierarchical BFS retriever + hotness scoring
    ├── indexer/            # Content → vector pipeline
    ├── session/            # Session management, archiving & compression
    │   ├── compressor.go   # V1 basic memory extraction
    │   └── compressor_v2.go # V2 schema-driven extraction with ReAct loop
    ├── memory/             # Memory extraction, schema & merge operations
    │   ├── mergeop/        # Field merge strategies (patch/immutable/sum)
    │   ├── schema.go       # YAML-driven memory type registry
    │   ├── updater.go      # Structured memory writer with merge ops
    │   ├── extractor.go    # LLM-based memory extraction
    │   └── deduplicator.go # Embedding-based deduplication
    ├── intent/             # LLM-driven query planning
    ├── tree/               # In-memory context tree
    ├── bootstrap/          # Directory structure initialization
    ├── content/            # Content write coordinator
    ├── prompts/            # Prompt template manager (YAML templates)
    ├── message/            # Message model & assembler
    ├── parse/              # Document import & parsing
    │   ├── ast_extract.go  # Code AST extraction (9 languages)
    │   ├── tree_builder.go # Parsed tree → VikingFS finalization
    │   ├── diff_sync.go    # Incremental top-down directory sync
    │   ├── directory_scan.go # Pre-scan validation & classification
    │   ├── parse_zip.go    # ZIP archive parser
    │   ├── parse_pptx.go   # PowerPoint parser
    │   └── registry.go     # Parser registry (12 formats)
    ├── server/             # HTTP API server (40+ endpoints)
    │   ├── routes_pack.go  # Pack export/import (.ovpack)
    │   ├── routes_debug.go # Debug & vector inspection
    │   ├── routes_observer.go # Component health observer
    │   ├── routes_stats.go # Memory & session statistics
    │   └── routes_tasks.go # Background task tracking
    ├── mcpserver/          # MCP protocol server (streamable-http)
    ├── console/            # Web management UI + API proxy
    ├── watch/              # Directory/URL/Git watch & sync
    │   ├── watch.go        # Task manager & scheduler
    │   └── source.go       # URL download & Git clone support
    ├── agent/              # Agent lifecycle bridge (hooks)
    ├── queue/              # Async processing queues
    │   ├── queue.go        # Embedding worker pool
    │   ├── semantic.go     # Semantic DAG executor (bottom-up summarization)
    │   ├── named_queue.go  # Named queue with status tracking & hooks
    │   ├── queue_manager.go # Multi-queue manager with worker pools
    │   └── embedding_tracker.go # Cross-queue task coordination
    ├── observer/           # Component health monitoring framework
    ├── resilience/         # Circuit breaker & retry with exponential backoff
    ├── telemetry/          # Operation tracing & telemetry
    │   ├── telemetry.go    # Core counters, gauges, snapshots
    │   ├── request.go      # Telemetry request normalization
    │   ├── context.go      # Context-bound goroutine-local telemetry
    │   ├── execution.go    # Telemetry-wrapped execution helpers
    │   └── runtime.go      # Global runtime meter (counters/gauges/histograms)
    └── metrics/            # Prometheus-format observability
```

## Key Features

- **Hierarchical Retrieval**: BFS-based search with score propagation across L0 (abstract), L1 (overview), L2 (detail) levels
- **Vector Search**: SQLite + sqlite-vec for dense vector similarity, with multi-backend abstraction
- **Memory System**: 8-category structured memory extraction (profile, preferences, entities, events, cases, patterns, tools, skills)
  - Schema-driven extraction via YAML definitions
  - ReAct-style LLM orchestration (CompressorV2)
  - Merge operations (patch, immutable, sum) for field-level updates
  - LLM-driven deduplication
- **Transaction System**: Path-level locking (point/subtree) with deadlock prevention and redo log recovery
- **Console UI**: Embedded SPA web dashboard with API proxy, CORS, and read/write permission control
- **Multi-format Parsing**: 12+ formats — Markdown, HTML, PDF, Word, Excel, EPUB, PowerPoint, ZIP, code (regex AST for Python/JS/TS/Java/Rust/Ruby/C#/Go/PHP/C/C++)
- **Tree Builder**: Parsed document tree finalization from temp VikingFS to permanent URI with unique name resolution and code hosting URL detection
- **Diff Sync**: Incremental top-down recursive directory synchronization with add/delete/update detection
- **Directory Scan**: Pre-scan validation with file classification (processable/unsupported), ignore dirs, include/exclude glob patterns
- **Resource Import**: File upload (temp_upload), add_resource with filtering, watch integration
- **Watch & Sync**: Monitor local directories, HTTP URLs, or Git repositories for changes; auto-sync and reindex
- **MCP Server**: 11 tools via streamable-http
- **Agent Bridge**: Lifecycle hooks for transparent memory injection/extraction
- **Multi-backend Storage**: Pluggable backend interface (SQLite, HTTP remote, in-memory)
- **Named Queue System**: Generic named queue abstraction with enqueue hooks, dequeue handlers, status tracking, centralized QueueManager with concurrent worker pools, and cross-queue EmbeddingTaskTracker coordination
- **Semantic DAG Executor**: Bottom-up directory summarization with file filtering, vectorize task collection, and content persistence
- **Observer System**: Health monitoring framework with concrete observers for queue, storage, models, locks, and retrieval subsystems
- **Resilience**: Circuit breaker (CLOSED/OPEN/HALF_OPEN) with permanent/transient error classification, and retry with exponential backoff + jitter
- **Observability**: Prometheus metrics, operation telemetry (request selection, context binding, execution helpers, global runtime meter), component health observer
- **Multi-tenancy**: Account/user/agent space isolation and access control
- **Client SDK**: HTTP client and local embedded client for Go applications

## Quick Start

### Docker (Recommended)

```bash
git clone https://github.com/XimilalaXiang/viking-go.git
cd viking-go

OPENAI_API_KEY=sk-xxx docker-compose up -d

# Check health
curl http://localhost:6920/health

# Open console UI
open http://localhost:6920/console/
```

### Build from Source

```bash
# Requires Go 1.24+ and CGO (for sqlite3)
go build -o viking-go ./cmd/viking-go/

# Run with defaults (port 6920, data in ~/.viking-go/data)
./viking-go

# Custom config
./viking-go --config config.json --port 6920
```

### MCP Client Configuration

Add to your MCP client config (Claude Desktop, Cursor, etc.):

```json
{
  "mcpServers": {
    "viking-go": {
      "type": "streamableHttp",
      "url": "http://127.0.0.1:6920/mcp"
    }
  }
}
```

### Configuration

Create `config.json`:

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 6920,
    "auth_mode": "dev",
    "mcp_enabled": true,
    "mcp_path": "/mcp"
  },
  "storage": {
    "data_dir": "/data",
    "db_path": "/data/viking.db"
  },
  "embedding": {
    "provider": "openai",
    "model": "text-embedding-3-small",
    "api_key": "sk-...",
    "api_base": "https://api.openai.com/v1",
    "dimension": 1536
  },
  "rerank": {
    "provider": "openai",
    "model": "rerank-v1",
    "api_key": "sk-...",
    "api_base": "https://api.openai.com/v1",
    "threshold": 0.3
  },
  "llm": {
    "provider": "openai",
    "model": "gpt-4o-mini",
    "api_key": "sk-...",
    "api_base": "https://api.openai.com/v1"
  }
}
```

Environment variable overrides: `OPENAI_API_KEY`, `VIKING_DATA_DIR`.

## MCP Tools

| Tool | Description |
|------|-------------|
| `query` | Hierarchical directory-recursive retrieval (memories, resources, skills) |
| `search` | Flat semantic vector search |
| `add_resource` | Write content + auto-index (async via queue) |
| `read` | Read content at L0/L1/L2 detail level |
| `list_directory` | Browse knowledge base structure |
| `tree` | Directory tree with abstracts |
| `status` | System statistics |
| `watch_create` | Create watch task (local dir, URL, or Git repo) |
| `watch_list` | List watch tasks |
| `watch_cancel` | Cancel watch task |
| `queue_status` | Embedding queue status |

## REST API Reference

### Health & Status

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| GET | `/api/v1/system/status` | System statistics |
| GET | `/metrics` | Prometheus-format metrics |

### Search

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/search/find` | Hierarchical retrieval with BFS |
| POST | `/api/v1/search/search` | Direct vector search |

### Content

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/content/read` | Read content by URI |
| GET | `/api/v1/content/abstract` | Read L0 abstract |
| GET | `/api/v1/content/overview` | Read L1 overview |
| POST | `/api/v1/content/write` | Write content (L0/L1/L2) |
| POST | `/api/v1/content/reindex` | Reindex a directory |

### Filesystem

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/fs/ls` | List directory contents |
| GET | `/api/v1/fs/tree` | Recursive tree listing |
| GET | `/api/v1/fs/stat` | File/directory info |
| POST | `/api/v1/fs/mkdir` | Create directory |
| POST | `/api/v1/fs/rm` | Remove file/directory |
| POST | `/api/v1/fs/mv` | Move/rename |

### Resources

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/resources` | Add resource (path/URL/temp_file_id, with filtering) |
| POST | `/api/v1/resources/temp_upload` | Upload temporary file for add_resource |

### Pack (Export/Import)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/pack/export` | Export directory as .ovpack archive |
| POST | `/api/v1/pack/import` | Import .ovpack archive |

### Debug

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/debug/health` | Storage health check |
| GET | `/api/v1/debug/vector/scroll` | Paginated vector record listing |
| GET | `/api/v1/debug/vector/count` | Vector record count |

### Observer

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/observer/queue` | Embedding queue health |
| GET | `/api/v1/observer/storage` | Storage backend health |
| GET | `/api/v1/observer/models` | Model availability |
| GET | `/api/v1/observer/system` | Full system health + runtime stats |

### Stats

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/stats/memories` | Memory counts by category |
| GET | `/api/v1/stats/sessions/{id}` | Session extraction stats |

### Tasks

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/tasks` | List background tasks |
| GET | `/api/v1/tasks/{id}` | Get task status |

### Relations

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/relations` | List relations |
| POST | `/api/v1/relations/link` | Create relation |
| DELETE | `/api/v1/relations/link` | Remove relation |

### Sessions

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/sessions` | Create session |
| GET | `/api/v1/sessions` | List sessions |
| GET | `/api/v1/sessions/{id}` | Get session info |
| GET | `/api/v1/sessions/{id}/context` | Get session with messages |
| POST | `/api/v1/sessions/{id}/messages` | Add message |
| POST | `/api/v1/sessions/{id}/commit` | Archive & clear messages |
| DELETE | `/api/v1/sessions/{id}` | Delete session |

### Watch (Directory/URL/Git Sync)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/watch` | Create watch task (local, URL, or Git) |
| GET | `/api/v1/watch` | List watch tasks |
| DELETE | `/api/v1/watch/{id}` | Cancel watch task |

### Agent Bridge

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/agent/start` | Retrieve context for agent session |
| POST | `/api/v1/agent/end` | Extract memories & archive session |
| POST | `/api/v1/agent/compact` | Extract memories before compaction |

### Console

| Path | Description |
|------|-------------|
| `/console/` | Web management dashboard |
| `/console/api/v1/*` | Console API proxy |

## URI Scheme

```
viking://{scope}/{space}/{path...}

Scopes: user, agent, shared, session, resources
Examples:
  viking://user/alice/memories/profile.md
  viking://agent/alice-agent/memories/cases/case_001.md
  viking://resources/obsidian/my-note.md
  viking://session/default/sess_abc123/
```

## Storage Model

Each directory in VikingFS stores three levels of content:
- **L0** (`.abstract.md`): One-line summary, used for BFS scoring
- **L1** (`.overview.md`): Medium-detail overview
- **L2** (`content.md`): Full content

Relations are stored as `.relations.json` files linking URIs bidirectionally.

## Testing

```bash
go test ./... -v -count=1
```

170+ tests across 22 packages covering URI parsing, storage, VikingFS, HTTP server, sessions, memory extraction, merge operations, intent analysis, tree structures, directory initialization, prompts, document parsing (including AST for 9 languages), console, watch, telemetry, multi-backend storage, named queues, queue manager, tree builder, observer, and resilience.

## Dependencies

- [github.com/mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) — SQLite3 driver (CGO)
- [github.com/google/uuid](https://github.com/google/uuid) — UUID generation
- [github.com/mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) — MCP protocol (streamable-http)
- [gopkg.in/yaml.v3](https://gopkg.in/yaml.v3) — YAML parsing for schemas and templates

Optional at runtime:
- [sqlite-vec](https://github.com/asg017/sqlite-vec) — Vector similarity extension for SQLite
- OpenAI-compatible API — For embeddings, reranking, and LLM features

## License

AGPL-3.0 — See LICENSE file.
