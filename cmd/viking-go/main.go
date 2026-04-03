package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/ximilala/viking-go/internal/config"
	"github.com/ximilala/viking-go/internal/embedder"
	"github.com/ximilala/viking-go/internal/retriever"
	"github.com/ximilala/viking-go/internal/server"
	"github.com/ximilala/viking-go/internal/storage"
	"github.com/ximilala/viking-go/internal/vikingfs"

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

	// Start server
	srv := server.NewServer(store, vfs, ret, cfg.Server.AuthMode, cfg.Server.RootAPIKey)
	addr := server.Addr(cfg.Server.Host, cfg.Server.Port)
	if err := srv.ListenAndServe(addr); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
