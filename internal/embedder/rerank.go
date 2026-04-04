package embedder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RerankResult holds the score for a single document after reranking.
type RerankResult struct {
	Index int     `json:"index"`
	Score float64 `json:"relevance_score"`
}

// Reranker scores documents against a query using a reranking model.
type Reranker interface {
	Rerank(query string, documents []string) ([]float64, error)
}

// RerankConfig configures a reranker.
type RerankConfig struct {
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	APIKey    string  `json:"api_key"`
	APIBase   string  `json:"api_base"`
	Threshold float64 `json:"threshold"`
}

// IsAvailable returns true if the reranker is configured with enough info
// to make API calls.
func (rc *RerankConfig) IsAvailable() bool {
	return rc != nil && rc.APIKey != "" && rc.APIBase != ""
}

// NewReranker creates a Reranker from configuration.
// Returns nil if the config is not available.
// Supports providers: "cohere" (native API), default (OpenAI-compatible API).
func NewReranker(cfg RerankConfig) Reranker {
	if cfg.APIKey == "" {
		return nil
	}

	switch cfg.Provider {
	case "cohere":
		if cfg.Model == "" {
			cfg.Model = "rerank-english-v3.0"
		}
		return &cohereReranker{
			apiKey: cfg.APIKey,
			model:  cfg.Model,
			client: &http.Client{Timeout: 30 * time.Second},
		}
	default:
		if cfg.APIBase == "" {
			return nil
		}
		if cfg.Model == "" {
			cfg.Model = "rerank-v1"
		}
		return &openAIReranker{
			cfg: cfg,
			client: &http.Client{
				Timeout: 30 * time.Second,
			},
		}
	}
}

type openAIReranker struct {
	cfg    RerankConfig
	client *http.Client
}

type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type rerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (r *openAIReranker) Rerank(query string, documents []string) ([]float64, error) {
	if len(documents) == 0 {
		return nil, nil
	}

	reqBody := rerankRequest{
		Model:     r.cfg.Model,
		Query:     query,
		Documents: documents,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal rerank request: %w", err)
	}

	req, err := http.NewRequest("POST", r.cfg.APIBase, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.APIKey)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read rerank response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var apiResp rerankResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal rerank response: %w", err)
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("rerank API error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Results) != len(documents) {
		return nil, fmt.Errorf("rerank result count mismatch: expected %d, got %d",
			len(documents), len(apiResp.Results))
	}

	scores := make([]float64, len(documents))
	for _, item := range apiResp.Results {
		if item.Index >= 0 && item.Index < len(documents) {
			scores[item.Index] = item.RelevanceScore
		}
	}

	return scores, nil
}

// --- Cohere Native Reranker ---

type cohereReranker struct {
	apiKey string
	model  string
	client *http.Client
}

type cohereRerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

type cohereRerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
	Meta struct {
		BilledUnits struct {
			SearchUnits int `json:"search_units"`
		} `json:"billed_units"`
	} `json:"meta"`
}

func (r *cohereReranker) Rerank(query string, documents []string) ([]float64, error) {
	if len(documents) == 0 {
		return nil, nil
	}

	reqBody := cohereRerankRequest{
		Model:     r.model,
		Query:     query,
		Documents: documents,
		TopN:      len(documents),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal cohere rerank: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.cohere.com/v1/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create cohere rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cohere rerank http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read cohere rerank response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cohere rerank API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var apiResp cohereRerankResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal cohere rerank response: %w", err)
	}

	scores := make([]float64, len(documents))
	for _, item := range apiResp.Results {
		if item.Index >= 0 && item.Index < len(documents) {
			scores[item.Index] = item.RelevanceScore
		}
	}

	return scores, nil
}
