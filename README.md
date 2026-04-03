# viking-go

Lightweight Go reimplementation of [OpenViking](https://github.com/AgiMaulana/OpenViking) — a hierarchical context retrieval system with long-term memory, designed for AI agents.

Single binary, ~13 MB, targeting 30–80 MB runtime memory.

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
    ├── retriever/          # Hierarchical BFS retriever
    ├── indexer/            # Content → vector pipeline
    ├── session/            # Session management & archiving
    ├── memory/             # Memory extraction & deduplication
    ├── intent/             # LLM-driven query planning
    ├── tree/               # In-memory context tree
    └── server/             # HTTP API server (16+ endpoints)
```

## Key Features

- **Hierarchical Retrieval**: BFS-based search with score propagation across L0 (abstract), L1 (overview), L2 (detail) levels
- **Vector Search**: SQLite + sqlite-vec for dense vector similarity
- **Memory System**: 8-category memory extraction (profile, preferences, entities, events, cases, patterns, tools, skills) with LLM-driven deduplication
- **Intent Analysis**: LLM-powered query plan generation from session context
- **VikingFS**: URI-based filesystem abstraction with relation management
- **Multi-tenancy**: Account/user/agent space isolation and access control
- **Session Management**: Create, archive, and query conversation sessions

## Quick Start

### Build

```bash
# Requires Go 1.24+ and CGO (for sqlite3)
go build -o viking-go ./cmd/viking-go/

# Run with defaults (port 8080, data in ./data)
./viking-go

# Custom config
./viking-go --config config.json --port 9090
```

### Configuration

Create `config.json`:

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8080,
    "api_key": "your-secret-key",
    "mode": "dev"
  },
  "storage": {
    "db_path": "./data/viking.db",
    "data_dir": "./data"
  },
  "embedding": {
    "model": "text-embedding-3-small",
    "api_key": "sk-...",
    "api_base": "https://api.openai.com/v1",
    "dimension": 1536
  },
  "rerank": {
    "model": "rerank-v1",
    "api_key": "sk-...",
    "api_base": "https://api.openai.com/v1"
  },
  "llm": {
    "model": "gpt-4o-mini",
    "api_key": "sk-...",
    "api_base": "https://api.openai.com/v1",
    "temperature": 0.3
  }
}
```

Environment variable overrides: `VIKING_API_KEY`, `VIKING_EMBEDDING_KEY`, `VIKING_EMBEDDING_BASE`, `VIKING_LLM_KEY`, `VIKING_LLM_BASE`.

## API Reference

### Health & Status

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| GET | `/api/v1/system/status` | System statistics |

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
| POST | `/api/v1/relations/unlink` | Remove relation |

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

## URI Scheme

```
viking://{scope}/{space}/{path...}

Scopes: user, agent, shared, session
Examples:
  viking://user/alice/docs/readme.md
  viking://agent/alice-agent/memories/cases/case_001.md
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

Currently 44 tests across 9 packages covering URI parsing, storage, VikingFS, HTTP server, sessions, memory extraction, intent analysis, and tree structures.

## Dependencies

- [github.com/mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) — SQLite3 driver (CGO)
- [github.com/google/uuid](https://github.com/google/uuid) — UUID generation

Optional at runtime:
- [sqlite-vec](https://github.com/asg017/sqlite-vec) — Vector similarity extension for SQLite
- OpenAI-compatible API — For embeddings, reranking, and LLM features

## License

See LICENSE file.
