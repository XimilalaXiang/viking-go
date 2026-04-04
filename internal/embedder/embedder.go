package embedder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"
)

// EmbedResult holds the output of an embedding request.
type EmbedResult struct {
	DenseVector  []float32          `json:"dense_vector,omitempty"`
	SparseVector map[string]float32 `json:"sparse_vector,omitempty"`
}

// Embedder is the interface all embedding providers must implement.
type Embedder interface {
	Embed(text string, isQuery bool) (*EmbedResult, error)
	EmbedBatch(texts []string, isQuery bool) ([]*EmbedResult, error)
	Dimension() int
	Close()
}

// Config configures an OpenAI-compatible embedder.
type Config struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	APIKey        string `json:"api_key"`
	APIBase       string `json:"api_base"`
	Dimension     int    `json:"dimension"`
	MaxRetries    int    `json:"max_retries"`
	QueryParam    string `json:"query_param"`
	DocumentParam string `json:"document_param"`
}

// NewEmbedder creates an embedder from configuration.
// Supports providers: "voyage", "jina", "cohere" (native APIs), default (OpenAI-compatible).
func NewEmbedder(cfg Config) (Embedder, error) {
	switch cfg.Provider {
	case "voyage":
		model := cfg.Model
		if model == "" {
			model = "voyage-3"
		}
		dim := cfg.Dimension
		if dim <= 0 {
			dim = 1024
		}
		return NewVoyageEmbedder(cfg.APIKey, model, dim), nil

	case "jina":
		model := cfg.Model
		if model == "" {
			model = "jina-embeddings-v3"
		}
		dim := cfg.Dimension
		if dim <= 0 {
			dim = 1024
		}
		return NewJinaEmbedder(cfg.APIKey, model, dim), nil

	case "cohere":
		model := cfg.Model
		if model == "" {
			model = "embed-english-v3.0"
		}
		dim := cfg.Dimension
		if dim <= 0 {
			dim = 1024
		}
		return NewCohereEmbedder(cfg.APIKey, model, dim), nil

	default:
		if cfg.APIBase == "" {
			cfg.APIBase = "https://api.openai.com/v1"
		}
		if cfg.Model == "" {
			cfg.Model = "text-embedding-3-small"
		}
		if cfg.Dimension <= 0 {
			cfg.Dimension = 1536
		}
		if cfg.MaxRetries <= 0 {
			cfg.MaxRetries = 3
		}
		return &openAIEmbedder{
			cfg: cfg,
			client: &http.Client{
				Timeout: 30 * time.Second,
			},
		}, nil
	}
}

// openAIEmbedder implements Embedder using the OpenAI-compatible embeddings API.
type openAIEmbedder struct {
	cfg    Config
	client *http.Client
	mu     sync.Mutex
	usage  tokenUsage
}

type tokenUsage struct {
	PromptTokens int64
	TotalTokens  int64
}

func (e *openAIEmbedder) Embed(text string, isQuery bool) (*EmbedResult, error) {
	results, err := e.EmbedBatch([]string{text}, isQuery)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	return results[0], nil
}

func (e *openAIEmbedder) EmbedBatch(texts []string, isQuery bool) ([]*EmbedResult, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	var lastErr error
	for attempt := 0; attempt <= e.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
			time.Sleep(delay)
		}

		results, err := e.doBatchRequest(texts, isQuery)
		if err == nil {
			return results, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("embedding failed after %d attempts: %w", e.cfg.MaxRetries+1, lastErr)
}

func (e *openAIEmbedder) Dimension() int {
	return e.cfg.Dimension
}

func (e *openAIEmbedder) Close() {}

// --- OpenAI API types ---

type embeddingRequest struct {
	Input      any                    `json:"input"`
	Model      string                 `json:"model"`
	Dimensions int                    `json:"dimensions,omitempty"`
	ExtraBody  map[string]any         `json:"extra_body,omitempty"`
}

type embeddingResponse struct {
	Data  []embeddingData `json:"data"`
	Usage struct {
		PromptTokens int64 `json:"prompt_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
	Error *apiError `json:"error,omitempty"`
}

type embeddingData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

func (e *openAIEmbedder) doBatchRequest(texts []string, isQuery bool) ([]*EmbedResult, error) {
	reqBody := embeddingRequest{
		Model: e.cfg.Model,
	}

	if len(texts) == 1 {
		reqBody.Input = texts[0]
	} else {
		reqBody.Input = texts
	}

	if e.cfg.Dimension > 0 {
		reqBody.Dimensions = e.cfg.Dimension
	}

	extraBody := e.buildExtraBody(isQuery)
	if extraBody != nil {
		reqBody.ExtraBody = extraBody
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := e.cfg.APIBase + "/embeddings"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var apiResp embeddingResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if apiResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", apiResp.Error.Message)
	}

	e.mu.Lock()
	e.usage.PromptTokens += apiResp.Usage.PromptTokens
	e.usage.TotalTokens += apiResp.Usage.TotalTokens
	e.mu.Unlock()

	results := make([]*EmbedResult, len(texts))
	for _, d := range apiResp.Data {
		if d.Index < len(results) {
			vec := d.Embedding
			if e.cfg.Dimension > 0 && len(vec) > e.cfg.Dimension {
				vec = TruncateAndNormalize(vec, e.cfg.Dimension)
			}
			results[d.Index] = &EmbedResult{DenseVector: vec}
		}
	}

	for i, r := range results {
		if r == nil {
			results[i] = &EmbedResult{}
		}
	}

	return results, nil
}

func (e *openAIEmbedder) buildExtraBody(isQuery bool) map[string]any {
	param := ""
	if isQuery && e.cfg.QueryParam != "" {
		param = e.cfg.QueryParam
	} else if !isQuery && e.cfg.DocumentParam != "" {
		param = e.cfg.DocumentParam
	}
	if param == "" {
		return nil
	}
	return map[string]any{"input_type": param}
}

// TruncateAndNormalize truncates a vector to the target dimension and L2-normalizes it.
func TruncateAndNormalize(vec []float32, dim int) []float32 {
	if dim <= 0 || len(vec) <= dim {
		return vec
	}
	v := vec[:dim]
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		result := make([]float32, dim)
		for i, x := range v {
			result[i] = float32(float64(x) / norm)
		}
		return result
	}
	return v
}

// TokenUsage returns cumulative token usage.
func (e *openAIEmbedder) TokenUsage() (prompt, total int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.usage.PromptTokens, e.usage.TotalTokens
}
