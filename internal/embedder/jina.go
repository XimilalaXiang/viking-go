package embedder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// JinaEmbedder implements the Embedder interface using Jina AI's native API.
// https://jina.ai/embeddings/
type JinaEmbedder struct {
	apiKey    string
	model     string
	dimension int
	client    *http.Client
}

// NewJinaEmbedder creates a Jina AI embedder.
func NewJinaEmbedder(apiKey, model string, dimension int) *JinaEmbedder {
	if model == "" {
		model = "jina-embeddings-v3"
	}
	if dimension <= 0 {
		dimension = 1024
	}
	return &JinaEmbedder{
		apiKey:    apiKey,
		model:     model,
		dimension: dimension,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *JinaEmbedder) Dimension() int { return e.dimension }
func (e *JinaEmbedder) Close()         {}

func (e *JinaEmbedder) Embed(text string, isQuery bool) (*EmbedResult, error) {
	results, err := e.EmbedBatch([]string{text}, isQuery)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return results[0], nil
}

func (e *JinaEmbedder) EmbedBatch(texts []string, isQuery bool) ([]*EmbedResult, error) {
	task := "retrieval.passage"
	if isQuery {
		task = "retrieval.query"
	}

	input := make([]jinaInput, len(texts))
	for i, t := range texts {
		input[i] = jinaInput{Text: t}
	}

	reqBody := jinaRequest{
		Model:          e.model,
		Input:          input,
		Task:           task,
		EmbeddingType:  "float",
		Dimensions:     e.dimension,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.jina.ai/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Jina API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Jina API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var jinaResp jinaResponse
	if err := json.Unmarshal(respBody, &jinaResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	results := make([]*EmbedResult, len(jinaResp.Data))
	for i, d := range jinaResp.Data {
		vec := make([]float32, len(d.Embedding))
		for j, v := range d.Embedding {
			vec[j] = float32(v)
		}
		results[i] = &EmbedResult{DenseVector: vec}
	}

	return results, nil
}

type jinaInput struct {
	Text string `json:"text"`
}

type jinaRequest struct {
	Model         string      `json:"model"`
	Input         []jinaInput `json:"input"`
	Task          string      `json:"task,omitempty"`
	EmbeddingType string     `json:"embedding_type,omitempty"`
	Dimensions    int         `json:"dimensions,omitempty"`
}

type jinaResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}
