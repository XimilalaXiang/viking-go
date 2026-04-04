# viking-go

[OpenViking](https://github.com/volcengine/OpenViking) 的轻量级 Go 重新实现 —— 面向 AI Agent 的层次化上下文数据库，支持长期记忆、知识检索和技能管理。

**单一二进制文件，约 14 MB，运行时内存 30–80 MB。**

> [English Documentation](README.md)

## 项目架构

```
viking-go (31,000+ 行代码, 98 个源文件, 363 个测试)
├── cmd/viking-go/              # 命令行入口
├── pkg/
│   ├── uri/                    # viking:// URI 解析与规范化
│   └── client/                 # HTTP 和嵌入式客户端 SDK
└── internal/
    ├── config/                 # JSON + 环境变量配置
    ├── context/                # 上下文模型 (L0/L1/L2, 类型, 身份)
    ├── storage/                # SQLite + sqlite-vec 存储层
    ├── vikingfs/               # 本地文件系统 (URI → 路径映射)
    ├── embedder/               # 多提供商向量嵌入和重排序
    │   ├── embedder.go         # OpenAI 兼容 + 提供商路由
    │   ├── voyage.go           # Voyage AI 原生 API
    │   ├── jina.go             # Jina AI 原生 API
    │   ├── cohere.go           # Cohere 原生 API
    │   └── rerank.go           # 重排序器 (OpenAI 兼容 + Cohere 原生)
    ├── llm/                    # LLM 对话补全 + 工具调用
    ├── vlm/                    # 视觉语言模型 (图像理解)
    ├── retriever/              # 层次化 BFS 检索器 + 热度评分
    ├── indexer/                # 内容 → 向量处理管线
    ├── session/                # 会话管理、归档与压缩
    ├── memory/                 # 记忆提取、Schema 与合并操作
    │   ├── mergeop/            # 字段合并策略 (patch/immutable/sum)
    │   ├── schema.go           # YAML 驱动的记忆类型注册表
    │   ├── generator.go        # YAML → Go 结构体代码生成器
    │   ├── extractor.go        # 基于 LLM 的记忆提取
    │   └── deduplicator.go     # 基于嵌入的语义去重
    ├── intent/                 # LLM 驱动的查询规划
    ├── parse/                  # 文档导入与解析 (18+ 格式)
    ├── server/                 # HTTP API 服务器 (67 个端点)
    ├── mcpserver/              # MCP 协议服务器 (streamable-http)
    ├── bot/                    # OpenAI 兼容的 Chat API (含 RAG)
    ├── console/                # Web 管理控制台 (内嵌 SPA)
    ├── watch/                  # 目录/URL/Git 监控与同步
    ├── agent/                  # Agent 生命周期桥接 (钩子)
    ├── queue/                  # 异步处理队列
    ├── eval/                   # RAGAS 风格的检索和 RAG 评估
    ├── skill/                  # SKILL.md 解析 + MCP→Skill 转换
    ├── integrations/           # Langfuse LLM 调用追踪
    ├── observer/               # 组件健康监控
    ├── resilience/             # 熔断器与重试机制
    ├── telemetry/              # 操作追踪与运行时指标
    └── metrics/                # Prometheus 格式可观测性
```

## 核心特性

### 检索引擎
- **层次化 BFS 检索** —— 跨 L0（摘要）、L1（概览）、L2（详情）三个层级进行分数传播
- **向量搜索** —— SQLite + sqlite-vec 密集向量相似度检索
- **热度评分** —— 基于访问频率和时间衰减的排序策略
- **重排序** —— 可选的重排序阶段（OpenAI 兼容 + Cohere 原生）

### 记忆系统
- **8 类结构化记忆提取** —— 人物画像、偏好设置、实体、事件、案例、模式、工具、技能
- **Schema 驱动** —— YAML 定义 + 字段级合并操作（patch、immutable、sum）
- **ReAct 式编排** —— CompressorV2 支持 LLM 驱动的多步提取
- **语义去重** —— 基于嵌入向量的去重
- **代码生成** —— 从 YAML Schema 自动生成 Go 结构体

### 文档解析 (18+ 格式)

| 格式 | 解析器 | 说明 |
|------|--------|------|
| Markdown | 内置 | 章节分割、元数据提取 |
| HTML | 内置 | 标签剥离的内容提取 |
| PDF | 内置 | 文本提取 |
| Word (.docx) | 内置 | 基于 XML 的提取 |
| Word (.doc) | Legacy 解析器 | OLE2 字节级分析 |
| Excel | 内置 | 工作表/单元格提取 |
| EPUB | 内置 | 章节提取 |
| PowerPoint | 内置 | 幻灯片提取 |
| ZIP | 内置 | 递归归档文件提取 |
| 代码文件 | AST 提取 | Python, JS, TS, Java, Rust, Ruby, C#, Go, PHP, C/C++ |
| 纯文本 | 内置 | 编码检测 |
| 飞书/Lark | 内置 | 导出 JSON → Markdown 转换 |
| 图片 | VLM 驱动 | jpg, png, gif, webp, bmp, tiff, svg |
| 音频 | Whisper API | mp3, wav, ogg, flac, m4a, wma, aac, opus |
| 视频 | ffmpeg + VLM | mp4, avi, mkv, mov, wmv, flv, webm |

### 嵌入模型提供商

| 提供商 | 类型 | 说明 |
|--------|------|------|
| OpenAI | 原生 API | 默认，text-embedding-3-small |
| Voyage AI | 原生 API | voyage-3, voyage-code-3 |
| Jina AI | 原生 API | jina-embeddings-v3 |
| Cohere | 原生 API | embed-english-v3.0 |
| 硅基流动 | OpenAI 兼容 | Qwen3-8B 嵌入模型 |
| 任何 OpenAI 兼容服务 | API 代理 | 通过 api_base 配置 |

### AI 集成
- **MCP 服务器** —— 11 个工具，通过 streamable-http 协议暴露
- **Bot OpenAPI** —— OpenAI 兼容的 `/v1/chat/completions`，内置 RAG（支持流式和非流式）
- **Agent 桥接** —— 生命周期钩子，透明注入/提取记忆上下文
- **Skill 加载器** —— 解析 SKILL.md 文件，MCP 工具转换为 Skill 定义
- **Langfuse 追踪** —— 异步缓冲的 LLM 调用追踪
- **VLM 支持** —— OpenAI Vision API 兼容的图像理解

### 基础设施
- **多租户** —— account_id + owner_space 隔离
- **API Key 认证** —— 多密钥管理
- **监控与同步** —— 监控本地目录、HTTP URL 或 Git 仓库的变化
- **评估框架** —— RAGAS 风格指标（Precision@K, Recall@K, MRR, NDCG, Faithfulness, Answer Correctness）
- **可观测性** —— Prometheus 指标、Langfuse 追踪、组件健康监控
- **弹性机制** —— 熔断器 + 指数退避重试
- **管理控制台** —— 内嵌 Web 仪表盘（Dashboard, 搜索, 浏览, 监控, 指标）

## 快速开始

### Docker（推荐）

```bash
git clone https://github.com/XimilalaXiang/viking-go.git
cd viking-go

OPENAI_API_KEY=sk-xxx docker-compose up -d

# 健康检查
curl http://localhost:6920/health

# 打开管理控制台
open http://localhost:6920/console/
```

### 从源码构建

```bash
# 需要 Go 1.24+ 和 CGO（sqlite3 依赖）
go build -o viking-go ./cmd/viking-go/

# 使用默认配置运行（端口 6920，数据存储在 ~/.viking-go/data）
./viking-go

# 自定义配置
./viking-go --config config.json --port 6920
```

### MCP 客户端配置

添加到你的 MCP 客户端配置（Claude Desktop、Cursor 等）：

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

## 配置说明

创建 `config.json`：

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

### 嵌入模型配置示例

```jsonc
// Voyage AI
{ "provider": "voyage", "model": "voyage-3", "api_key": "pa-...", "dimension": 1024 }

// Jina AI
{ "provider": "jina", "model": "jina-embeddings-v3", "api_key": "jina_...", "dimension": 1024 }

// Cohere
{ "provider": "cohere", "model": "embed-english-v3.0", "api_key": "...", "dimension": 1024 }

// 硅基流动 (Qwen)
{ "provider": "openai", "model": "Qwen/Qwen3-8B", "api_key": "sk-...", "api_base": "https://api.siliconflow.cn/v1", "dimension": 4096 }

// OpenAI
{ "provider": "openai", "model": "text-embedding-3-small", "api_key": "sk-...", "dimension": 1536 }
```

环境变量覆盖：`OPENAI_API_KEY`、`VIKING_DATA_DIR`。

## MCP 工具

| 工具 | 描述 |
|------|------|
| `query` | 层次化目录递归检索（记忆、资源、技能） |
| `search` | 平面语义向量搜索 |
| `add_resource` | 写入内容 + 自动索引（通过队列异步处理） |
| `read` | 按 L0/L1/L2 层级读取内容 |
| `list_directory` | 浏览知识库结构 |
| `tree` | 带摘要的目录树 |
| `status` | 系统状态统计 |
| `watch_create` | 创建监控任务（本地目录、URL 或 Git 仓库） |
| `watch_list` | 列出监控任务 |
| `watch_cancel` | 取消监控任务 |
| `queue_status` | 嵌入队列状态 |

## REST API (67 个端点)

<details>
<summary>点击展开完整 API 参考</summary>

### 健康与状态

| 方法 | 端点 | 描述 |
|------|------|------|
| GET | `/health` | 健康检查 |
| GET | `/api/v1/system/status` | 系统统计 |
| GET | `/metrics` | Prometheus 格式指标 |

### 搜索

| 方法 | 端点 | 描述 |
|------|------|------|
| POST | `/api/v1/search/find` | 层次化 BFS 检索 |
| POST | `/api/v1/search/search` | 直接向量搜索 |

### 内容

| 方法 | 端点 | 描述 |
|------|------|------|
| GET | `/api/v1/content/read` | 按 URI 读取内容 |
| GET | `/api/v1/content/abstract` | 读取 L0 摘要 |
| GET | `/api/v1/content/overview` | 读取 L1 概览 |
| POST | `/api/v1/content/write` | 写入内容 |
| POST | `/api/v1/content/reindex` | 重新索引目录 |

### 文件系统

| 方法 | 端点 | 描述 |
|------|------|------|
| GET | `/api/v1/fs/ls` | 列出目录内容 |
| GET | `/api/v1/fs/tree` | 递归树形列表 |
| GET | `/api/v1/fs/stat` | 文件/目录信息 |
| POST | `/api/v1/fs/mkdir` | 创建目录 |
| POST | `/api/v1/fs/rm` | 删除文件/目录 |
| POST | `/api/v1/fs/mv` | 移动/重命名 |

### 资源

| 方法 | 端点 | 描述 |
|------|------|------|
| POST | `/api/v1/resources` | 添加资源（支持路径/URL/临时文件） |
| POST | `/api/v1/resources/temp_upload` | 上传临时文件 |

### 导入导出

| 方法 | 端点 | 描述 |
|------|------|------|
| POST | `/api/v1/pack/export` | 导出为 .ovpack 归档 |
| POST | `/api/v1/pack/import` | 导入 .ovpack 归档 |

### 调试

| 方法 | 端点 | 描述 |
|------|------|------|
| GET | `/api/v1/debug/health` | 存储健康检查 |
| GET | `/api/v1/debug/vector/scroll` | 分页向量记录 |
| GET | `/api/v1/debug/vector/count` | 向量记录数量 |

### 监控

| 方法 | 端点 | 描述 |
|------|------|------|
| GET | `/api/v1/observer/queue` | 队列健康 |
| GET | `/api/v1/observer/storage` | 存储健康 |
| GET | `/api/v1/observer/models` | 模型可用性 |
| GET | `/api/v1/observer/system` | 完整系统健康 |

### 统计

| 方法 | 端点 | 描述 |
|------|------|------|
| GET | `/api/v1/stats/memories` | 按类别统计记忆数量 |
| GET | `/api/v1/stats/sessions/{id}` | 会话提取统计 |

### 任务

| 方法 | 端点 | 描述 |
|------|------|------|
| GET | `/api/v1/tasks` | 列出后台任务 |
| GET | `/api/v1/tasks/{id}` | 获取任务状态 |

### 关联关系

| 方法 | 端点 | 描述 |
|------|------|------|
| GET | `/api/v1/relations` | 列出关联 |
| POST | `/api/v1/relations/link` | 创建关联 |
| DELETE | `/api/v1/relations/link` | 删除关联 |

### 会话

| 方法 | 端点 | 描述 |
|------|------|------|
| POST | `/api/v1/sessions` | 创建会话 |
| GET | `/api/v1/sessions` | 列出会话 |
| GET | `/api/v1/sessions/{id}` | 获取会话信息 |
| GET | `/api/v1/sessions/{id}/context` | 获取会话及消息 |
| POST | `/api/v1/sessions/{id}/messages` | 添加消息 |
| POST | `/api/v1/sessions/{id}/commit` | 归档并清除消息 |
| DELETE | `/api/v1/sessions/{id}` | 删除会话 |

### 监控同步

| 方法 | 端点 | 描述 |
|------|------|------|
| POST | `/api/v1/watch` | 创建监控任务 |
| GET | `/api/v1/watch` | 列出监控任务 |
| DELETE | `/api/v1/watch/{id}` | 取消监控任务 |

### Agent 桥接

| 方法 | 端点 | 描述 |
|------|------|------|
| POST | `/api/v1/agent/start` | 为 Agent 检索上下文 |
| POST | `/api/v1/agent/end` | 提取记忆并归档 |
| POST | `/api/v1/agent/compact` | 压缩前提取记忆 |

### Bot (OpenAI 兼容对话)

| 方法 | 端点 | 描述 |
|------|------|------|
| POST | `/v1/chat/completions` | RAG 增强对话（流式/非流式） |
| POST | `/bot/chat` | 对话补全别名 |

### 管理控制台

| 路径 | 描述 |
|------|------|
| `/console/` | Web 管理仪表盘 |
| `/console/api/v1/*` | 控制台 API 代理 |

</details>

## URI 方案

```
viking://{scope}/{space}/{path...}

作用域: user, agent, shared, session, resources
示例:
  viking://user/alice/memories/profile.md
  viking://agent/alice-agent/memories/cases/case_001.md
  viking://resources/obsidian/my-note.md
  viking://session/default/sess_abc123/
```

## 存储模型

VikingFS 中的每个目录存储三个层级的内容：
- **L0** (`.abstract.md`) —— 单行摘要，用于 BFS 评分
- **L1** (`.overview.md`) —— 中等详细程度的概览
- **L2** (`content.md`) —— 完整内容

关联关系以 `.relations.json` 文件存储，双向链接 URI。

## 测试

```bash
go test ./... -v -count=1
```

23 个测试包中共 363 个测试。

## 依赖

- [github.com/mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) — SQLite3 驱动（CGO）
- [github.com/google/uuid](https://github.com/google/uuid) — UUID 生成
- [github.com/mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) — MCP 协议（streamable-http）
- [gopkg.in/yaml.v3](https://gopkg.in/yaml.v3) — YAML 解析

运行时可选：
- [sqlite-vec](https://github.com/asg017/sqlite-vec) — SQLite 向量相似度扩展
- OpenAI 兼容 API — 用于嵌入、重排序和 LLM 功能
- ffmpeg — 用于视频帧提取
- Whisper API — 用于音频转写

## 与 OpenViking 的对比

| 维度 | OpenViking (Python) | viking-go |
|------|-------------------|-----------|
| 语言 | Python 3.10+ | Go 1.24+ |
| 二进制大小 | ~数百 MB（含依赖） | ~14 MB |
| 运行时内存 | 300–800 MB | 30–80 MB |
| 数据库 | 外部向量数据库 | SQLite + sqlite-vec（嵌入式） |
| 部署 | 多组件 | 单一二进制 |
| MCP | 支持 | 支持 (streamable-http) |
| 文档解析 | 10+ 格式 | 18+ 格式（含多模态） |
| 嵌入提供商 | OpenAI 兼容 | 5+ 原生提供商 |
| 评估 | 无内置 | RAGAS 风格评估框架 |
| LLM 追踪 | 无内置 | Langfuse 集成 |

## 许可证

AGPL-3.0 — 详见 [LICENSE](LICENSE) 文件。
