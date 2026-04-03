package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ximilala/viking-go/internal/agent"
	"github.com/ximilala/viking-go/internal/config"
	"github.com/ximilala/viking-go/internal/console"
	"github.com/ximilala/viking-go/internal/embedder"
	"github.com/ximilala/viking-go/internal/indexer"
	"github.com/ximilala/viking-go/internal/llm"
	"github.com/ximilala/viking-go/internal/mcpserver"
	"github.com/ximilala/viking-go/internal/memory"
	"github.com/ximilala/viking-go/internal/metrics"
	"github.com/ximilala/viking-go/internal/queue"
	"github.com/ximilala/viking-go/internal/retriever"
	"github.com/ximilala/viking-go/internal/server"
	"github.com/ximilala/viking-go/internal/session"
	"github.com/ximilala/viking-go/internal/storage"
	"github.com/ximilala/viking-go/internal/vikingfs"
	"github.com/ximilala/viking-go/internal/watch"

	mcphttp "github.com/mark3labs/mcp-go/server"
	_ "github.com/mattn/go-sqlite3"
)

var version = "0.1.0"

func main() {
	configPath := flag.String("config", "", "Path to viking-go config file (JSON)")
	host := flag.String("host", "", "Host to bind to")
	port := flag.Int("port", 0, "Port to bind to")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("viking-go %s\n", version)
		os.Exit(0)
	}

	var cfg *config.Config
	var err error
	if *configPath != "" {
		cfg, err = config.LoadConfig(*configPath)
	} else {
		cfg = config.DefaultConfig()
	}
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *host != "" {
		cfg.Server.Host = *host
	}
	if *port != 0 {
		cfg.Server.Port = *port
	}

	if err := cfg.EnsureDataDir(); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	log.Printf("viking-go %s starting on %s:%d", version, cfg.Server.Host, cfg.Server.Port)
	log.Printf("Data directory: %s", cfg.Storage.DataDir)

	// Initialize SQLite store
	store, err := storage.NewStore(cfg.Storage.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	// Initialize vector table
	if err := store.InitVectorTable(cfg.Embedding.Dimension); err != nil {
		log.Printf("Warning: vector table init: %v (sqlite-vec may not be loaded)", err)
	}

	// Initialize VikingFS
	vfs, err := vikingfs.New(cfg.Storage.DataDir)
	if err != nil {
		log.Fatalf("Failed to initialize VikingFS: %v", err)
	}

	// Initialize embedder (best-effort; search won't work without it)
	var emb embedder.Embedder
	if cfg.Embedding.APIKey != "" || cfg.Embedding.APIBase != "" {
		emb, err = embedder.NewEmbedder(embedder.Config{
			Provider:  cfg.Embedding.Provider,
			Model:     cfg.Embedding.Model,
			APIKey:    cfg.Embedding.APIKey,
			APIBase:   cfg.Embedding.APIBase,
			Dimension: cfg.Embedding.Dimension,
		})
		if err != nil {
			log.Printf("Warning: embedder init failed: %v", err)
		} else {
			log.Printf("Embedder initialized: %s/%s (dim=%d)", cfg.Embedding.Provider, cfg.Embedding.Model, cfg.Embedding.Dimension)
		}
	} else {
		log.Println("No embedding API key configured — semantic search disabled")
	}

	// Initialize reranker (optional)
	var reranker embedder.Reranker
	if cfg.Rerank.APIKey != "" && cfg.Rerank.APIBase != "" {
		reranker = embedder.NewReranker(embedder.RerankConfig{
			Provider:  cfg.Rerank.Provider,
			Model:     cfg.Rerank.Model,
			APIKey:    cfg.Rerank.APIKey,
			APIBase:   cfg.Rerank.APIBase,
			Threshold: cfg.Rerank.Threshold,
		})
		if reranker != nil {
			log.Printf("Reranker initialized: %s/%s", cfg.Rerank.Provider, cfg.Rerank.Model)
		}
	}

	// Initialize retriever
	ret := retriever.NewHierarchicalRetriever(store, emb, reranker, cfg.Rerank.Threshold)

	// Initialize indexer
	var idx *indexer.Indexer
	if emb != nil {
		idx = indexer.New(store, vfs, emb)
		log.Println("Indexer initialized")
	}

	// Initialize LLM client (for memory extraction)
	var llmClient llm.Client
	if cfg.LLM.APIKey != "" || cfg.LLM.APIBase != "" {
		llmClient = llm.NewClient(llm.Config{
			Provider: cfg.LLM.Provider,
			Model:    cfg.LLM.Model,
			APIKey:   cfg.LLM.APIKey,
			APIBase:  cfg.LLM.APIBase,
		})
		log.Printf("LLM client initialized: %s/%s", cfg.LLM.Provider, cfg.LLM.Model)
	}

	// Initialize memory extractor
	var memExtractor *memory.Extractor
	if llmClient != nil {
		memExtractor = memory.NewExtractor(llmClient, vfs)
		log.Println("Memory extractor initialized")
	}

	// Initialize session manager and agent bridge
	sessionMgr := session.NewManager(vfs)
	var bridge *agent.Bridge
	bridge = agent.NewBridge(store, vfs, ret, sessionMgr, memExtractor)
	log.Println("Agent bridge initialized")

	// Initialize embedding queue
	var embQueue *queue.EmbeddingQueue
	if idx != nil {
		embQueue = queue.NewEmbeddingQueue(idx, 2, 1000)
		embQueue.Start()
		defer embQueue.Stop()
	}

	// Initialize watch scheduler
	watchMgr := watch.NewManager(vfs)
	var watchSched *watch.Scheduler
	if idx != nil {
		watchSched = watch.NewScheduler(watchMgr, idx, vfs)
		watchSched.Start()
		defer watchSched.Stop()
	}

	// Build HTTP mux: REST API + optional MCP endpoint
	srv := server.NewServer(store, vfs, ret, idx, cfg.Server.AuthMode, cfg.Server.RootAPIKey, watchMgr, bridge)
	addr := server.Addr(cfg.Server.Host, cfg.Server.Port)

	if cfg.Server.MCPEnabled {
		mcpSrv := mcpserver.New(store, vfs, ret, idx, watchMgr, embQueue)
		mcpPath := cfg.Server.MCPPath
		if mcpPath == "" {
			mcpPath = "/mcp"
		}

		httpHandler := mcphttp.NewStreamableHTTPServer(mcpSrv.MCPServerInstance(),
			mcphttp.WithEndpointPath(mcpPath),
			mcphttp.WithSessionIdleTTL(10*time.Minute),
		)

		mux := http.NewServeMux()
		mux.Handle(mcpPath, httpHandler)
		mux.Handle(mcpPath+"/", httpHandler)
		mux.Handle("/metrics", metrics.Handler())
		mux.Handle("/console/", http.StripPrefix("/console/", console.Handler()))
		mux.Handle("/console", http.RedirectHandler("/console/", http.StatusMovedPermanently))
		mux.Handle("/", srv)

		log.Printf("MCP server enabled at %s%s", addr, mcpPath)
		log.Printf("viking-go API listening on %s", addr)

		httpSrv := &http.Server{
			Addr:    addr,
			Handler: mux,
		}
		if err := httpSrv.ListenAndServe(); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	} else {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler())
		mux.Handle("/console/", http.StripPrefix("/console/", console.Handler()))
		mux.Handle("/console", http.RedirectHandler("/console/", http.StatusMovedPermanently))
		mux.Handle("/", srv)

		log.Printf("viking-go API listening on %s", addr)
		httpSrv := &http.Server{
			Addr:    addr,
			Handler: mux,
		}
		if err := httpSrv.ListenAndServe(); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}
}
