package embedder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CohereEmbedder implements the Embedder interface using Cohere's native API.
// https://docs.cohere.com/reference/embed
type CohereEmbedder struct {
	apiKey    string
	model     string
	dimension int
	client    *http.Client
}

// NewCohereEmbedder creates a Cohere embedder.
func NewCohereEmbedder(apiKey, model string, dimension int) *CohereEmbedder {
	if model == "" {
		model = "embed-english-v3.0"
	}
	if dimension <= 0 {
		dimension = 1024
	}
	return &CohereEmbedder{
		apiKey:    apiKey,
		model:     model,
		dimension: dimension,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *CohereEmbedder) Dimension() int { return e.dimension }
func (e *CohereEmbedder) Close()         {}

func (e *CohereEmbedder) Embed(text string, isQuery bool) (*EmbedResult, error) {
	results, err := e.EmbedBatch([]string{text}, isQuery)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return results[0], nil
}

func (e *CohereEmbedder) EmbedBatch(texts []string, isQuery bool) ([]*EmbedResult, error) {
	inputType := "search_document"
	if isQuery {
		inputType = "search_query"
	}

	reqBody := cohereEmbedRequest{
		Texts:         texts,
		Model:         e.model,
		InputType:     inputType,
		EmbeddingTypes: []string{"float"},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.cohere.com/v1/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Cohere API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Cohere API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var cohereResp cohereEmbedResponse
	if err := json.Unmarshal(respBody, &cohereResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	embeddings := cohereResp.Embeddings.Float
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("Cohere returned no embeddings")
	}

	results := make([]*EmbedResult, len(embeddings))
	for i, emb := range embeddings {
		vec := make([]float32, len(emb))
		for j, v := range emb {
			vec[j] = float32(v)
		}
		results[i] = &EmbedResult{DenseVector: vec}
	}

	return results, nil
}

type cohereEmbedRequest struct {
	Texts          []string `json:"texts"`
	Model          string   `json:"model"`
	InputType      string   `json:"input_type"`
	EmbeddingTypes []string `json:"embedding_types,omitempty"`
}

type cohereEmbedResponse struct {
	Embeddings struct {
		Float [][]float64 `json:"float"`
	} `json:"embeddings"`
	Texts []string `json:"texts"`
	Meta  struct {
		BilledUnits struct {
			InputTokens int `json:"input_tokens"`
		} `json:"billed_units"`
	} `json:"meta"`
}
