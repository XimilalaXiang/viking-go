package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// LocalClient is an embedded client that calls the Viking-Go HTTP server in-process.
// It creates an in-memory HTTP server and routes requests directly, avoiding network overhead.
type LocalClient struct {
	handler http.Handler
	server  *httptest.Server
	inner   *HTTPClient
}

// NewLocalClient creates an embedded client backed by the given http.Handler.
// The handler is typically the server.Server from internal/server.
func NewLocalClient(handler http.Handler) *LocalClient {
	ts := httptest.NewServer(handler)
	return &LocalClient{
		handler: handler,
		server:  ts,
		inner:   NewHTTPClient(ts.URL),
	}
}

func (c *LocalClient) Initialize(ctx context.Context) error  { return c.inner.Initialize(ctx) }
func (c *LocalClient) Close(_ context.Context) error {
	c.server.Close()
	return nil
}

func (c *LocalClient) AddResource(ctx context.Context, req AddResourceRequest) (*AddResourceResponse, error) {
	return c.inner.AddResource(ctx, req)
}
func (c *LocalClient) ReadContent(ctx context.Context, uri string) (*ContentResponse, error) {
	return c.inner.ReadContent(ctx, uri)
}
func (c *LocalClient) WriteContent(ctx context.Context, req WriteContentRequest) error {
	return c.inner.WriteContent(ctx, req)
}
func (c *LocalClient) Reindex(ctx context.Context, uri string) error {
	return c.inner.Reindex(ctx, uri)
}
func (c *LocalClient) Ls(ctx context.Context, uri string, opts LsOptions) ([]FSEntry, error) {
	return c.inner.Ls(ctx, uri, opts)
}
func (c *LocalClient) Stat(ctx context.Context, uri string) (*FSEntry, error) {
	return c.inner.Stat(ctx, uri)
}
func (c *LocalClient) Mkdir(ctx context.Context, uri string) error {
	return c.inner.Mkdir(ctx, uri)
}
func (c *LocalClient) Rm(ctx context.Context, uri string, recursive bool) error {
	return c.inner.Rm(ctx, uri, recursive)
}
func (c *LocalClient) Mv(ctx context.Context, src, dst string) error {
	return c.inner.Mv(ctx, src, dst)
}
func (c *LocalClient) Find(ctx context.Context, req FindRequest) (*FindResponse, error) {
	return c.inner.Find(ctx, req)
}
func (c *LocalClient) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	return c.inner.Search(ctx, req)
}
func (c *LocalClient) CreateSession(ctx context.Context) (*Session, error) {
	return c.inner.CreateSession(ctx)
}
func (c *LocalClient) GetSession(ctx context.Context, id string) (*Session, error) {
	return c.inner.GetSession(ctx, id)
}
func (c *LocalClient) ListSessions(ctx context.Context) ([]Session, error) {
	return c.inner.ListSessions(ctx)
}
func (c *LocalClient) DeleteSession(ctx context.Context, id string) error {
	return c.inner.DeleteSession(ctx, id)
}
func (c *LocalClient) AddMessage(ctx context.Context, sessionID string, msg Message) error {
	return c.inner.AddMessage(ctx, sessionID, msg)
}
func (c *LocalClient) CommitSession(ctx context.Context, sessionID string) error {
	return c.inner.CommitSession(ctx, sessionID)
}
func (c *LocalClient) GetSessionContext(ctx context.Context, sessionID string) (string, error) {
	return c.inner.GetSessionContext(ctx, sessionID)
}
func (c *LocalClient) Link(ctx context.Context, src, dst, relType string) error {
	return c.inner.Link(ctx, src, dst, relType)
}
func (c *LocalClient) Unlink(ctx context.Context, src, dst, relType string) error {
	return c.inner.Unlink(ctx, src, dst, relType)
}
func (c *LocalClient) Relations(ctx context.Context, uri string) ([]Relation, error) {
	return c.inner.Relations(ctx, uri)
}
func (c *LocalClient) AgentStart(ctx context.Context, req AgentStartRequest) error {
	return c.inner.AgentStart(ctx, req)
}
func (c *LocalClient) AgentEnd(ctx context.Context, sessionID string) error {
	return c.inner.AgentEnd(ctx, sessionID)
}
func (c *LocalClient) AgentCompact(ctx context.Context, sessionID string) error {
	return c.inner.AgentCompact(ctx, sessionID)
}
func (c *LocalClient) Health(ctx context.Context) error {
	return c.inner.Health(ctx)
}
func (c *LocalClient) Status(ctx context.Context) (*SystemStatus, error) {
	return c.inner.Status(ctx)
}

// RawRequest sends a raw HTTP request to the embedded server for advanced use.
func (c *LocalClient) RawRequest(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.server.URL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.server.Client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server error %d: %s", resp.StatusCode, string(respBody))
	}
	return json.RawMessage(respBody), nil
}

var _ Client = (*LocalClient)(nil)
