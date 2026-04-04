package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
)

func init() {
	RegisterBackend("http", func(cfg BackendConfig) (Backend, error) {
		if cfg.Endpoint == "" {
			return nil, fmt.Errorf("http backend requires endpoint")
		}
		return NewHTTPBackend(cfg.Endpoint), nil
	})
}

// HTTPBackend proxies storage operations to a remote HTTP vector database service.
type HTTPBackend struct {
	endpoint string
	client   *http.Client
}

// NewHTTPBackend creates a backend that delegates to a remote service.
func NewHTTPBackend(endpoint string) *HTTPBackend {
	return &HTTPBackend{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (b *HTTPBackend) Name() string { return "http" }

func (b *HTTPBackend) Upsert(c *ctx.Context) error {
	_, err := b.post("/upsert", c)
	return err
}

func (b *HTTPBackend) Get(ids []string) ([]*ctx.Context, error) {
	body, err := b.post("/get", map[string]any{"ids": ids})
	if err != nil {
		return nil, err
	}
	var results []*ctx.Context
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return results, nil
}

func (b *HTTPBackend) Delete(ids []string) (int, error) {
	body, err := b.post("/delete", map[string]any{"ids": ids})
	if err != nil {
		return 0, err
	}
	var result struct {
		Count int `json:"count"`
	}
	json.Unmarshal(body, &result)
	return result.Count, nil
}

func (b *HTTPBackend) DeleteByFilter(filter FilterExpr) (int, error) {
	body, err := b.post("/delete_by_filter", map[string]any{"filter": filterToMap(filter)})
	if err != nil {
		return 0, err
	}
	var result struct {
		Count int `json:"count"`
	}
	json.Unmarshal(body, &result)
	return result.Count, nil
}

func (b *HTTPBackend) Query(filter FilterExpr, limit, offset int, orderBy string, desc bool) ([]*ctx.Context, error) {
	body, err := b.post("/query", map[string]any{
		"filter":   filterToMap(filter),
		"limit":    limit,
		"offset":   offset,
		"order_by": orderBy,
		"desc":     desc,
	})
	if err != nil {
		return nil, err
	}
	var results []*ctx.Context
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("decode query response: %w", err)
	}
	return results, nil
}

func (b *HTTPBackend) Count(filter FilterExpr) (int, error) {
	body, err := b.post("/count", map[string]any{"filter": filterToMap(filter)})
	if err != nil {
		return 0, err
	}
	var result struct {
		Count int `json:"count"`
	}
	json.Unmarshal(body, &result)
	return result.Count, nil
}

func (b *HTTPBackend) VectorSearch(queryVec []float32, filter FilterExpr, limit int, outputFields []string) ([]SearchResult, error) {
	body, err := b.post("/search", map[string]any{
		"vector":        queryVec,
		"filter":        filterToMap(filter),
		"limit":         limit,
		"output_fields": outputFields,
	})
	if err != nil {
		return nil, err
	}
	var results []SearchResult
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	return results, nil
}

func (b *HTTPBackend) CollectionExists() bool {
	resp, err := b.client.Get(b.endpoint + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func (b *HTTPBackend) Stats() (map[string]any, error) {
	resp, err := b.client.Get(b.endpoint + "/stats")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

func (b *HTTPBackend) Close() error { return nil }

func (b *HTTPBackend) post(path string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	resp, err := b.client.Post(b.endpoint+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// filterToMap converts a FilterExpr to a serializable map.
func filterToMap(filter FilterExpr) map[string]any {
	if filter == nil {
		return nil
	}
	switch f := filter.(type) {
	case Eq:
		return map[string]any{"op": "eq", "field": f.Field, "value": f.Value}
	case And:
		conds := make([]map[string]any, 0, len(f.Filters))
		for _, c := range f.Filters {
			if m := filterToMap(c); m != nil {
				conds = append(conds, m)
			}
		}
		return map[string]any{"op": "and", "conds": conds}
	case In:
		return map[string]any{"op": "in", "field": f.Field, "values": f.Values}
	case PathScope:
		return map[string]any{"op": "path_scope", "field": f.Field, "path": f.BasePath, "depth": f.Depth}
	default:
		return map[string]any{"op": "unknown"}
	}
}
