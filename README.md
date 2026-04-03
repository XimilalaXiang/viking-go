# viking-go

Lightweight Go reimplementation of [OpenViking](https://github.com/volcengine/OpenViking) — a hierarchical context retrieval system with long-term memory, designed for AI agents.

Single binary, ~14 MB, targeting 30–80 MB runtime memory.

## Architecture

```
viking-go
├── cmd/viking-go/          # CLI entry point
├── pkg/uri/                # viking:// URI parsing & normalization
└── internal/
    ├── config/             # JSON + env-var configuration
    ├── context/            # Context model (L0/L1/L2, types, identity)
    ├── storage/            # SQLite + sqlite-vec storage layer
    ├── vikingfs/           # Local filesystem (URI → path mapping)
    ├── embedder/           # Embedding & reranking (OpenAI-compatible)
    ├── llm/                # LLM chat completion client
    ├── retriever/          # Hierarchical BFS retriever + hotness scoring
    ├── indexer/            # Content → vector pipeline
    ├── session/            # Session management, archiving & compression
    ├── memory/             # Memory extraction & deduplication
    ├── intent/             # LLM-driven query planning
    ├── tree/               # In-memory context tree
    ├── bootstrap/          # Directory structure initialization
    ├── content/            # Content write coordinator
    ├── prompts/            # Prompt template manager
    ├── parse/              # Document import & parsing
    ├── server/             # HTTP API server (26+ endpoints)
    ├── mcpserver/          # MCP protocol server (streamable-http)
    ├── watch/              # Directory watch & incremental sync
    ├── agent/              # Agent lifecycle bridge (hooks)
    ├── queue/              # Async embedding worker pool
    └── metrics/            # Prometheus-format observability
```

## Key Features

- **Hierarchical Retrieval**: BFS-based search with score propagation across L0 (abstract), L1 (overview), L2 (detail) levels
- **Vector Search**: SQLite + sqlite-vec for dense vector similarity
- **Memory System**: 8-category memory extraction (profile, preferences, entities, events, cases, patterns, tools, skills) with LLM-driven deduplication
- **Hotness Scoring**: Blends semantic relevance with access frequency and recency
- **MCP Server**: 11 tools via streamable-http — query, search, add_resource, read, list_directory, tree, status, watch_create/list/cancel, queue_status
- **Watch & Sync**: Monitor local directories for changes (SHA256 hash), auto-sync and reindex
- **Agent Bridge**: Lifecycle hooks (BeforeAgentStart, AfterAgentEnd, BeforeCompaction) for transparent memory injection/extraction
- **Embedding Queue**: Async worker pool for non-blocking bulk vectorization
- **Observability**: Prometheus-format metrics at `/metrics` (request counts, latencies, embedding stats)
- **Intent Analysis**: LLM-powered query plan generation from session context
- **VikingFS**: URI-based filesystem abstraction with relation management
- **Multi-tenancy**: Account/user/agent space isolation and access control
- **Session Management**: Create, archive, and query conversation sessions

## Quick Start

### Docker (Recommended)

```bash
# Clone and build
git clone https://github.com/XimilalaXiang/viking-go.git
cd viking-go

# Start with docker-compose
OPENAI_API_KEY=sk-xxx docker-compose up -d

# Check health
curl http://localhost:6920/health
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
| `watch_create` | Create directory watch task |
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

### Watch (Directory Sync)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/watch` | Create watch task |
| GET | `/api/v1/watch` | List watch tasks |
| DELETE | `/api/v1/watch/{id}` | Cancel watch task |

### Agent Bridge

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/agent/start` | Retrieve context for agent session |
| POST | `/api/v1/agent/end` | Extract memories & archive session |
| POST | `/api/v1/agent/compact` | Extract memories before compaction |

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

60+ tests across 11 packages covering URI parsing, storage, VikingFS, HTTP server, sessions, memory extraction, intent analysis, tree structures, directory initialization, prompts, and document parsing.

## Dependencies

- [github.com/mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) — SQLite3 driver (CGO)
- [github.com/google/uuid](https://github.com/google/uuid) — UUID generation
- [github.com/mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) — MCP protocol (streamable-http)

Optional at runtime:
- [sqlite-vec](https://github.com/asg017/sqlite-vec) — Vector similarity extension for SQLite
- OpenAI-compatible API — For embeddings, reranking, and LLM features

## License

AGPL-3.0 — See LICENSE file.
