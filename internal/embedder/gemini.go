package embedder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GeminiEmbedder implements the Embedder interface using Google's Gemini embedding API.
// https://ai.google.dev/api/embeddings
type GeminiEmbedder struct {
	apiKey    string
	model     string
	dimension int
	apiBase   string
	client    *http.Client
}

// NewGeminiEmbedder creates a Gemini embedder.
func NewGeminiEmbedder(apiKey, model string, dimension int, apiBase string) *GeminiEmbedder {
	if model == "" {
		model = "text-embedding-004"
	}
	if dimension <= 0 {
		dimension = 768
	}
	if apiBase == "" {
		apiBase = "https://generativelanguage.googleapis.com"
	}
	return &GeminiEmbedder{
		apiKey:    apiKey,
		model:     model,
		dimension: dimension,
		apiBase:   apiBase,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *GeminiEmbedder) Dimension() int { return e.dimension }
func (e *GeminiEmbedder) Close()         {}

func (e *GeminiEmbedder) Embed(text string, isQuery bool) (*EmbedResult, error) {
	results, err := e.EmbedBatch([]string{text}, isQuery)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return results[0], nil
}

func (e *GeminiEmbedder) EmbedBatch(texts []string, isQuery bool) ([]*EmbedResult, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	taskType := "RETRIEVAL_DOCUMENT"
	if isQuery {
		taskType = "RETRIEVAL_QUERY"
	}

	// Use batchEmbedContents for multiple texts
	if len(texts) > 1 {
		return e.batchEmbed(texts, taskType)
	}

	// Single text uses embedContent
	return e.singleEmbed(texts[0], taskType)
}

func (e *GeminiEmbedder) singleEmbed(text, taskType string) ([]*EmbedResult, error) {
	reqBody := geminiEmbedRequest{
		Model: "models/" + e.model,
		Content: geminiContent{
			Parts: []geminiPart{{Text: text}},
		},
		TaskType:        taskType,
		OutputDimensionality: e.dimension,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:embedContent?key=%s", e.apiBase, e.model, e.apiKey)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Gemini API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var geminiResp geminiEmbedResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	vec := make([]float32, len(geminiResp.Embedding.Values))
	for i, v := range geminiResp.Embedding.Values {
		vec[i] = float32(v)
	}
	return []*EmbedResult{{DenseVector: vec}}, nil
}

func (e *GeminiEmbedder) batchEmbed(texts []string, taskType string) ([]*EmbedResult, error) {
	var requests []geminiEmbedRequest
	for _, text := range texts {
		requests = append(requests, geminiEmbedRequest{
			Model: "models/" + e.model,
			Content: geminiContent{
				Parts: []geminiPart{{Text: text}},
			},
			TaskType:        taskType,
			OutputDimensionality: e.dimension,
		})
	}

	reqBody := geminiBatchEmbedRequest{
		Requests: requests,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:batchEmbedContents?key=%s", e.apiBase, e.model, e.apiKey)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Gemini API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var batchResp geminiBatchEmbedResponse
	if err := json.Unmarshal(respBody, &batchResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	results := make([]*EmbedResult, len(batchResp.Embeddings))
	for i, emb := range batchResp.Embeddings {
		vec := make([]float32, len(emb.Values))
		for j, v := range emb.Values {
			vec[j] = float32(v)
		}
		results[i] = &EmbedResult{DenseVector: vec}
	}

	return results, nil
}

// --- Gemini API types ---

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiEmbedRequest struct {
	Model                string        `json:"model"`
	Content              geminiContent `json:"content"`
	TaskType             string        `json:"taskType,omitempty"`
	OutputDimensionality int           `json:"outputDimensionality,omitempty"`
}

type geminiEmbedResponse struct {
	Embedding struct {
		Values []float64 `json:"values"`
	} `json:"embedding"`
}

type geminiBatchEmbedRequest struct {
	Requests []geminiEmbedRequest `json:"requests"`
}

type geminiBatchEmbedResponse struct {
	Embeddings []struct {
		Values []float64 `json:"values"`
	} `json:"embeddings"`
}
