package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// HTTPClient communicates with a remote Viking-Go server over HTTP.
type HTTPClient struct {
	baseURL    string
	apiKey     string
	accountID  string
	userID     string
	agentID    string
	httpClient *http.Client
}

// HTTPClientOption configures the HTTPClient.
type HTTPClientOption func(*HTTPClient)

func WithAPIKey(key string) HTTPClientOption     { return func(c *HTTPClient) { c.apiKey = key } }
func WithAccountID(id string) HTTPClientOption   { return func(c *HTTPClient) { c.accountID = id } }
func WithUserID(id string) HTTPClientOption      { return func(c *HTTPClient) { c.userID = id } }
func WithAgentID(id string) HTTPClientOption     { return func(c *HTTPClient) { c.agentID = id } }
func WithTimeout(d time.Duration) HTTPClientOption {
	return func(c *HTTPClient) { c.httpClient.Timeout = d }
}

// NewHTTPClient creates a new HTTP client for the given server URL.
func NewHTTPClient(baseURL string, opts ...HTTPClientOption) *HTTPClient {
	c := &HTTPClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *HTTPClient) Initialize(ctx context.Context) error {
	return c.Health(ctx)
}

func (c *HTTPClient) Close(_ context.Context) error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// --- Resource / Content ---

func (c *HTTPClient) AddResource(ctx context.Context, req AddResourceRequest) (*AddResourceResponse, error) {
	var resp AddResourceResponse
	if err := c.post(ctx, "/api/v1/content/write", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *HTTPClient) ReadContent(ctx context.Context, uri string) (*ContentResponse, error) {
	var resp ContentResponse
	if err := c.get(ctx, "/api/v1/content/read", map[string]string{"uri": uri}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *HTTPClient) WriteContent(ctx context.Context, req WriteContentRequest) error {
	return c.post(ctx, "/api/v1/content/write", req, nil)
}

func (c *HTTPClient) Reindex(ctx context.Context, uri string) error {
	return c.post(ctx, "/api/v1/content/reindex", map[string]string{"uri": uri}, nil)
}

// --- Filesystem ---

func (c *HTTPClient) Ls(ctx context.Context, uri string, opts LsOptions) ([]FSEntry, error) {
	params := map[string]string{"uri": uri}
	if opts.Simple {
		params["simple"] = "true"
	}
	if opts.Recursive {
		params["recursive"] = "true"
	}
	var entries []FSEntry
	if err := c.get(ctx, "/api/v1/fs/ls", params, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (c *HTTPClient) Stat(ctx context.Context, uri string) (*FSEntry, error) {
	var entry FSEntry
	if err := c.get(ctx, "/api/v1/fs/stat", map[string]string{"uri": uri}, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func (c *HTTPClient) Mkdir(ctx context.Context, uri string) error {
	return c.post(ctx, "/api/v1/fs/mkdir", map[string]string{"uri": uri}, nil)
}

func (c *HTTPClient) Rm(ctx context.Context, uri string, recursive bool) error {
	return c.post(ctx, "/api/v1/fs/rm", map[string]any{"uri": uri, "recursive": recursive}, nil)
}

func (c *HTTPClient) Mv(ctx context.Context, src, dst string) error {
	return c.post(ctx, "/api/v1/fs/mv", map[string]string{"source": src, "destination": dst}, nil)
}

// --- Search ---

func (c *HTTPClient) Find(ctx context.Context, req FindRequest) (*FindResponse, error) {
	var resp FindResponse
	if err := c.post(ctx, "/api/v1/search/find", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *HTTPClient) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	var resp SearchResponse
	if err := c.post(ctx, "/api/v1/search/search", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// --- Sessions ---

func (c *HTTPClient) CreateSession(ctx context.Context) (*Session, error) {
	var sess Session
	if err := c.post(ctx, "/api/v1/sessions", nil, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (c *HTTPClient) GetSession(ctx context.Context, id string) (*Session, error) {
	var sess Session
	if err := c.get(ctx, "/api/v1/sessions/"+id, nil, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (c *HTTPClient) ListSessions(ctx context.Context) ([]Session, error) {
	var sessions []Session
	if err := c.get(ctx, "/api/v1/sessions", nil, &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (c *HTTPClient) DeleteSession(ctx context.Context, id string) error {
	return c.doRequest(ctx, "DELETE", "/api/v1/sessions/"+id, nil, nil)
}

func (c *HTTPClient) AddMessage(ctx context.Context, sessionID string, msg Message) error {
	return c.post(ctx, "/api/v1/sessions/"+sessionID+"/messages", msg, nil)
}

func (c *HTTPClient) CommitSession(ctx context.Context, sessionID string) error {
	return c.post(ctx, "/api/v1/sessions/"+sessionID+"/commit", nil, nil)
}

func (c *HTTPClient) GetSessionContext(ctx context.Context, sessionID string) (string, error) {
	var resp struct {
		Context string `json:"context"`
	}
	if err := c.get(ctx, "/api/v1/sessions/"+sessionID+"/context", nil, &resp); err != nil {
		return "", err
	}
	return resp.Context, nil
}

// --- Relations ---

func (c *HTTPClient) Link(ctx context.Context, src, dst, relType string) error {
	return c.post(ctx, "/api/v1/relations/link", map[string]string{
		"source": src, "target": dst, "type": relType,
	}, nil)
}

func (c *HTTPClient) Unlink(ctx context.Context, src, dst, relType string) error {
	return c.doRequest(ctx, "DELETE", "/api/v1/relations/link", map[string]string{
		"source": src, "target": dst, "type": relType,
	}, nil)
}

func (c *HTTPClient) Relations(ctx context.Context, uri string) ([]Relation, error) {
	var rels []Relation
	if err := c.get(ctx, "/api/v1/relations", map[string]string{"uri": uri}, &rels); err != nil {
		return nil, err
	}
	return rels, nil
}

// --- Agent ---

func (c *HTTPClient) AgentStart(ctx context.Context, req AgentStartRequest) error {
	return c.post(ctx, "/api/v1/agent/start", req, nil)
}

func (c *HTTPClient) AgentEnd(ctx context.Context, sessionID string) error {
	return c.post(ctx, "/api/v1/agent/end", map[string]string{"session_id": sessionID}, nil)
}

func (c *HTTPClient) AgentCompact(ctx context.Context, sessionID string) error {
	return c.post(ctx, "/api/v1/agent/compact", map[string]string{"session_id": sessionID}, nil)
}

// --- System ---

func (c *HTTPClient) Health(ctx context.Context) error {
	return c.get(ctx, "/health", nil, nil)
}

func (c *HTTPClient) Status(ctx context.Context) (*SystemStatus, error) {
	var status SystemStatus
	if err := c.get(ctx, "/api/v1/system/status", nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// --- HTTP helpers ---

func (c *HTTPClient) get(ctx context.Context, path string, params map[string]string, out any) error {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return err
	}
	if params != nil {
		q := u.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)
	return c.execute(req, out)
}

func (c *HTTPClient) post(ctx context.Context, path string, body any, out any) error {
	return c.doRequest(ctx, "POST", path, body, out)
}

func (c *HTTPClient) doRequest(ctx context.Context, method, path string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setHeaders(req)
	return c.execute(req, out)
}

func (c *HTTPClient) setHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	if c.accountID != "" {
		req.Header.Set("X-Account-ID", c.accountID)
	}
	if c.userID != "" {
		req.Header.Set("X-User-ID", c.userID)
	}
	if c.agentID != "" {
		req.Header.Set("X-Agent-ID", c.agentID)
	}
}

func (c *HTTPClient) execute(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		_ = json.Unmarshal(respBody, &errResp)
		if errResp.Error != "" {
			return fmt.Errorf("server error %d [%s]: %s", resp.StatusCode, errResp.Code, errResp.Error)
		}
		return fmt.Errorf("server error %d: %s", resp.StatusCode, string(respBody))
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// compile-time interface check
var _ Client = (*HTTPClient)(nil)
