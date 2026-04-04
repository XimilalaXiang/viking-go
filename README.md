# viking-go

Lightweight Go reimplementation of [OpenViking](https://github.com/volcengine/OpenViking) — a hierarchical context database for AI agents with long-term memory, knowledge retrieval, and skill management.

**Single binary, ~14 MB, targeting 30–80 MB runtime memory.**

> [中文文档](README_CN.md)

## Architecture

```
viking-go (31,000+ LOC, 98 source files, 363 tests)
├── cmd/viking-go/              # CLI entry point
├── pkg/
│   ├── uri/                    # viking:// URI parsing & normalization
│   └── client/                 # HTTP & embedded client SDK
└── internal/
    ├── config/                 # JSON + env-var configuration
    ├── context/                # Context model (L0/L1/L2, types, identity)
    ├── storage/                # SQLite + sqlite-vec storage layer
    │   ├── backend.go          # Multi-backend (SQLite/HTTP/memory)
    │   ├── transaction.go      # Path-level LockManager & redo log
    │   └── filter.go           # Filter expressions (Eq/And/In/PathScope)
    ├── vikingfs/               # Local filesystem (URI → path mapping)
    ├── embedder/               # Multi-provider embedding & reranking
    │   ├── embedder.go         # OpenAI-compatible + provider router
    │   ├── voyage.go           # Voyage AI native API
    │   ├── jina.go             # Jina AI native API
    │   ├── cohere.go           # Cohere native API
    │   └── rerank.go           # Reranker (OpenAI-compatible + Cohere native)
    ├── llm/                    # LLM chat completion + tool calling
    ├── vlm/                    # Vision-Language Model (image understanding)
    ├── retriever/              # Hierarchical BFS retriever + hotness scoring
    ├── indexer/                # Content → vector pipeline
    ├── session/                # Session management, archiving & compression
    │   ├── compressor.go       # V1 basic memory extraction
    │   └── compressor_v2.go    # V2 schema-driven with ReAct loop
    ├── memory/                 # Memory extraction, schema & merge
    │   ├── mergeop/            # Field merge strategies (patch/immutable/sum)
    │   ├── schema.go           # YAML-driven memory type registry
    │   ├── generator.go        # YAML → Go struct code generator
    │   ├── extractor.go        # LLM-based memory extraction
    │   └── deduplicator.go     # Embedding-based deduplication
    ├── intent/                 # LLM-driven query planning
    ├── tree/                   # In-memory context tree
    ├── bootstrap/              # Directory structure initialization
    ├── content/                # Content write coordinator
    ├── prompts/                # Prompt template manager (YAML templates)
    ├── message/                # Message model & assembler
    ├── parse/                  # Document import & parsing (18+ formats)
    │   ├── ast_extract.go      # Code AST extraction (9 languages)
    │   ├── diff_sync.go        # Incremental directory sync
    │   ├── parse_image.go      # VLM-powered image understanding
    │   ├── parse_audio.go      # Whisper API transcription
    │   ├── parse_video.go      # ffmpeg + VLM frame analysis
    │   ├── parse_feishu.go     # Feishu/Lark document parser
    │   ├── parse_legacy_doc.go # Legacy .doc (OLE2) parser
    │   └── registry.go         # Parser registry
    ├── server/                 # HTTP API server (67 endpoints)
    │   ├── apikeys.go          # Multi-key authentication
    │   ├── routes_pack.go      # Pack export/import (.ovpack)
    │   ├── routes_debug.go     # Debug & vector inspection
    │   ├── routes_observer.go  # Component health observer
    │   ├── routes_resources.go # Resource import with filtering
    │   ├── routes_stats.go     # Memory & session statistics
    │   └── routes_tasks.go     # Background task tracking
    ├── mcpserver/              # MCP protocol server (streamable-http)
    ├── bot/                    # OpenAI-compatible chat API with RAG
    ├── console/                # Web management UI (embedded SPA)
    ├── watch/                  # Directory/URL/Git watch & sync
    ├── agent/                  # Agent lifecycle bridge (hooks)
    ├── queue/                  # Async processing queues
    │   ├── queue.go            # Embedding worker pool
    │   ├── semantic.go         # Semantic DAG executor
    │   ├── named_queue.go      # Named queue with status tracking
    │   └── queue_manager.go    # Multi-queue manager
    ├── eval/                   # RAGAS-style retrieval & RAG evaluation
    ├── skill/                  # SKILL.md parser + MCP→Skill conversion
    ├── integrations/           # Langfuse LLM tracing
    ├── observer/               # Component health monitoring
    ├── resilience/             # Circuit breaker & retry
    ├── telemetry/              # Operation tracing & runtime metrics
    └── metrics/                # Prometheus-format observability
```

## Key Features

### Core Retrieval
- **Hierarchical BFS Retrieval** — Score propagation across L0 (abstract), L1 (overview), L2 (detail) levels
- **Vector Search** — SQLite + sqlite-vec for dense vector similarity
- **Hotness Scoring** — Access frequency + recency decay for ranking
- **Reranking** — Optional reranker pass (OpenAI-compatible + Cohere native)

### Memory System
- **8-category structured extraction** — Profile, preferences, entities, events, cases, patterns, tools, skills
- **Schema-driven** — YAML definitions with field-level merge operations (patch, immutable, sum)
- **ReAct-style orchestration** — CompressorV2 with LLM-driven multi-step extraction
- **Deduplication** — Embedding-based semantic deduplication
- **Code generation** — Auto-generate Go structs from YAML schema definitions

### Document Parsing (18+ formats)
| Format | Parser | Notes |
|--------|--------|-------|
| Markdown | Built-in | Section splitting, metadata extraction |
| HTML | Built-in | Content extraction with tag stripping |
| PDF | Built-in | Text extraction |
| Word (.docx) | Built-in | XML-based extraction |
| Word (.doc) | Legacy parser | OLE2 byte-level analysis |
| Excel | Built-in | Sheet/cell extraction |
| EPUB | Built-in | Chapter extraction |
| PowerPoint | Built-in | Slide extraction |
| ZIP | Built-in | Recursive archive extraction |
| Code | AST extraction | Python, JS, TS, Java, Rust, Ruby, C#, Go, PHP, C/C++ |
| Text | Built-in | Plain text with encoding detection |
| Feishu/Lark | Built-in | Exported JSON → Markdown conversion |
| Images | VLM-powered | jpg, png, gif, webp, bmp, tiff, svg |
| Audio | Whisper API | mp3, wav, ogg, flac, m4a, wma, aac, opus |
| Video | ffmpeg + VLM | mp4, avi, mkv, mov, wmv, flv, webm |

### Embedding Providers
| Provider | Type | Notes |
|----------|------|-------|
| OpenAI | Native API | Default, text-embedding-3-small |
| Voyage AI | Native API | voyage-3, voyage-code-3 |
| Jina AI | Native API | jina-embeddings-v3 |
| Cohere | Native API | embed-english-v3.0 |
| SiliconFlow | OpenAI-compatible | Qwen3-8B embedding |
| Any OpenAI-compatible | API proxy | Via api_base configuration |

### AI Integration
- **MCP Server** — 11 tools via streamable-http protocol
- **Bot OpenAPI** — OpenAI-compatible `/v1/chat/completions` with built-in RAG (streaming + non-streaming)
- **Agent Bridge** — Lifecycle hooks for transparent memory injection/extraction
- **Skill Loader** — Parse SKILL.md files and convert MCP tools to skill definitions
- **Langfuse Tracing** — Async buffered LLM call tracing
- **VLM Support** — OpenAI Vision API-compatible image understanding

### Infrastructure
- **Multi-tenancy** — Account + owner_space isolation
- **API Key Auth** — Multi-key management
- **Watch & Sync** — Monitor local directories, HTTP URLs, or Git repositories
- **Evaluation Framework** — RAGAS-style metrics (Precision@K, Recall@K, MRR, NDCG, Faithfulness, Answer Correctness)
- **Observability** — Prometheus metrics, Langfuse tracing, component health monitoring
- **Resilience** — Circuit breaker + exponential backoff retry
- **Console UI** — Embedded web dashboard (Dashboard, Search, Browse, Watch, Metrics)

## Quick Start

### Docker (Recommended)

```bash
git clone https://github.com/XimilalaXiang/viking-go.git
cd viking-go

OPENAI_API_KEY=sk-xxx docker-compose up -d

# Health check
curl http://localhost:6920/health

# Console UI
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

## Configuration

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
    "provider": "voyage",
    "model": "voyage-3",
    "api_key": "pa-...",
    "dimension": 1024
  },
  "rerank": {
    "provider": "cohere",
    "model": "rerank-english-v3.0",
    "api_key": "...",
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

### Embedding Provider Examples

```json
// Voyage AI
{ "provider": "voyage", "model": "voyage-3", "api_key": "pa-...", "dimension": 1024 }

// Jina AI
{ "provider": "jina", "model": "jina-embeddings-v3", "api_key": "jina_...", "dimension": 1024 }

// Cohere
{ "provider": "cohere", "model": "embed-english-v3.0", "api_key": "...", "dimension": 1024 }

// SiliconFlow (Qwen)
{ "provider": "openai", "model": "Qwen/Qwen3-8B", "api_key": "sk-...", "api_base": "https://api.siliconflow.cn/v1", "dimension": 4096 }

// OpenAI
{ "provider": "openai", "model": "text-embedding-3-small", "api_key": "sk-...", "dimension": 1536 }
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

## REST API (67 Endpoints)

<details>
<summary>Click to expand full API reference</summary>

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
| POST | `/api/v1/resources` | Add resource (path/URL/temp, with filtering) |
| POST | `/api/v1/resources/temp_upload` | Upload temporary file |

### Pack (Export/Import)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/pack/export` | Export as .ovpack archive |
| POST | `/api/v1/pack/import` | Import .ovpack archive |

### Debug

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/debug/health` | Storage health check |
| GET | `/api/v1/debug/vector/scroll` | Paginated vector listing |
| GET | `/api/v1/debug/vector/count` | Vector record count |

### Observer

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/observer/queue` | Queue health |
| GET | `/api/v1/observer/storage` | Storage health |
| GET | `/api/v1/observer/models` | Model availability |
| GET | `/api/v1/observer/system` | Full system health |

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
| GET | `/api/v1/sessions/{id}/context` | Session with messages |
| POST | `/api/v1/sessions/{id}/messages` | Add message |
| POST | `/api/v1/sessions/{id}/commit` | Archive & clear |
| DELETE | `/api/v1/sessions/{id}` | Delete session |

### Watch

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/watch` | Create watch task |
| GET | `/api/v1/watch` | List watch tasks |
| DELETE | `/api/v1/watch/{id}` | Cancel watch task |

### Agent Bridge

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/agent/start` | Retrieve context for agent |
| POST | `/api/v1/agent/end` | Extract memories & archive |
| POST | `/api/v1/agent/compact` | Extract before compaction |

### Bot (OpenAI-Compatible Chat)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/v1/chat/completions` | Chat with RAG (stream/non-stream) |
| POST | `/bot/chat` | Alias for chat completions |

### Console

| Path | Description |
|------|-------------|
| `/console/` | Web management dashboard |
| `/console/api/v1/*` | Console API proxy |

</details>

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
- **L0** (`.abstract.md`) — One-line summary, used for BFS scoring
- **L1** (`.overview.md`) — Medium-detail overview
- **L2** (`content.md`) — Full content

Relations are stored as `.relations.json` files linking URIs bidirectionally.

## Testing

```bash
go test ./... -v -count=1
```

363 tests across 23 packages.

## Dependencies

- [github.com/mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) — SQLite3 driver (CGO)
- [github.com/google/uuid](https://github.com/google/uuid) — UUID generation
- [github.com/mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) — MCP protocol (streamable-http)
- [gopkg.in/yaml.v3](https://gopkg.in/yaml.v3) — YAML parsing

Optional at runtime:
- [sqlite-vec](https://github.com/asg017/sqlite-vec) — Vector similarity extension for SQLite
- OpenAI-compatible API — For embeddings, reranking, and LLM features
- ffmpeg — For video frame extraction
- Whisper API — For audio transcription

## License

AGPL-3.0 — See [LICENSE](LICENSE) file.
