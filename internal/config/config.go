package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the main configuration for viking-go.
type Config struct {
	Server    ServerConfig    `json:"server"`
	Storage   StorageConfig   `json:"storage"`
	Embedding EmbeddingConfig `json:"embedding"`
	Rerank    RerankConfig    `json:"rerank"`
	LLM       LLMConfig       `json:"llm"`
}

type ServerConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	AuthMode   string `json:"auth_mode"`
	RootAPIKey string `json:"root_api_key"`
	MCPEnabled bool   `json:"mcp_enabled"`
	MCPPath    string `json:"mcp_path"`
}

type StorageConfig struct {
	DataDir   string `json:"data_dir"`
	DBPath    string `json:"db_path"`
}

type EmbeddingConfig struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	APIKey    string `json:"api_key"`
	APIBase   string `json:"api_base"`
	Dimension int    `json:"dimension"`
}

type RerankConfig struct {
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	APIKey    string  `json:"api_key"`
	APIBase   string  `json:"api_base"`
	Threshold float64 `json:"threshold"`
}

type LLMConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
	APIBase  string `json:"api_base"`
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".viking-go", "data")

	return &Config{
		Server: ServerConfig{
			Host:       "127.0.0.1",
			Port:       6920,
			AuthMode:   "dev",
			MCPEnabled: true,
			MCPPath:    "/mcp",
		},
		Storage: StorageConfig{
			DataDir: dataDir,
			DBPath:  filepath.Join(dataDir, "viking.db"),
		},
		Embedding: EmbeddingConfig{
			Provider:  "openai",
			Model:     "text-embedding-3-small",
			Dimension: 1536,
		},
		LLM: LLMConfig{
			Provider: "openai",
			Model:    "gpt-4o-mini",
		},
	}
}

// LoadConfig loads configuration from a JSON file, with defaults for missing fields.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Override from environment variables
	if v := os.Getenv("OPENAI_API_KEY"); v != "" && cfg.Embedding.APIKey == "" {
		cfg.Embedding.APIKey = v
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" && cfg.LLM.APIKey == "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("VIKING_DATA_DIR"); v != "" {
		cfg.Storage.DataDir = v
		cfg.Storage.DBPath = filepath.Join(v, "viking.db")
	}

	return cfg, nil
}

// EnsureDataDir creates the data directory if it doesn't exist.
func (c *Config) EnsureDataDir() error {
	return os.MkdirAll(c.Storage.DataDir, 0755)
}
