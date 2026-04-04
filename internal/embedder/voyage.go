package embedder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// VoyageEmbedder implements the Embedder interface using Voyage AI's native API.
// https://docs.voyageai.com/reference/embeddings-api
type VoyageEmbedder struct {
	apiKey    string
	model     string
	dimension int
	client    *http.Client
}

// NewVoyageEmbedder creates a Voyage AI embedder.
func NewVoyageEmbedder(apiKey, model string, dimension int) *VoyageEmbedder {
	if model == "" {
		model = "voyage-3"
	}
	if dimension <= 0 {
		dimension = 1024
	}
	return &VoyageEmbedder{
		apiKey:    apiKey,
		model:     model,
		dimension: dimension,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *VoyageEmbedder) Dimension() int { return e.dimension }
func (e *VoyageEmbedder) Close()         {}

func (e *VoyageEmbedder) Embed(text string, isQuery bool) (*EmbedResult, error) {
	results, err := e.EmbedBatch([]string{text}, isQuery)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return results[0], nil
}

func (e *VoyageEmbedder) EmbedBatch(texts []string, isQuery bool) ([]*EmbedResult, error) {
	inputType := "document"
	if isQuery {
		inputType = "query"
	}

	reqBody := voyageRequest{
		Input:     texts,
		Model:     e.model,
		InputType: inputType,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.voyageai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Voyage API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Voyage API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var voyageResp voyageResponse
	if err := json.Unmarshal(respBody, &voyageResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	results := make([]*EmbedResult, len(voyageResp.Data))
	for i, d := range voyageResp.Data {
		vec := make([]float32, len(d.Embedding))
		for j, v := range d.Embedding {
			vec[j] = float32(v)
		}
		results[i] = &EmbedResult{DenseVector: vec}
	}

	return results, nil
}

type voyageRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type,omitempty"`
}

type voyageResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}
