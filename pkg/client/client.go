// Package client provides SDK clients for interacting with Viking-Go.
// It offers both an embedded LocalClient (in-process) and an HTTPClient (remote HTTP).
package client

import "context"

// Client is the main interface for interacting with Viking-Go.
type Client interface {
	// Lifecycle
	Initialize(ctx context.Context) error
	Close(ctx context.Context) error

	// Resource / Content
	AddResource(ctx context.Context, req AddResourceRequest) (*AddResourceResponse, error)
	ReadContent(ctx context.Context, uri string) (*ContentResponse, error)
	WriteContent(ctx context.Context, req WriteContentRequest) error
	Reindex(ctx context.Context, uri string) error

	// FileSystem
	Ls(ctx context.Context, uri string, opts LsOptions) ([]FSEntry, error)
	Stat(ctx context.Context, uri string) (*FSEntry, error)
	Mkdir(ctx context.Context, uri string) error
	Rm(ctx context.Context, uri string, recursive bool) error
	Mv(ctx context.Context, src, dst string) error

	// Search
	Find(ctx context.Context, req FindRequest) (*FindResponse, error)
	Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)

	// Sessions
	CreateSession(ctx context.Context) (*Session, error)
	GetSession(ctx context.Context, id string) (*Session, error)
	ListSessions(ctx context.Context) ([]Session, error)
	DeleteSession(ctx context.Context, id string) error
	AddMessage(ctx context.Context, sessionID string, msg Message) error
	CommitSession(ctx context.Context, sessionID string) error
	GetSessionContext(ctx context.Context, sessionID string) (string, error)

	// Relations
	Link(ctx context.Context, src, dst, relType string) error
	Unlink(ctx context.Context, src, dst, relType string) error
	Relations(ctx context.Context, uri string) ([]Relation, error)

	// Agent
	AgentStart(ctx context.Context, req AgentStartRequest) error
	AgentEnd(ctx context.Context, sessionID string) error
	AgentCompact(ctx context.Context, sessionID string) error

	// System
	Health(ctx context.Context) error
	Status(ctx context.Context) (*SystemStatus, error)
}

// AddResourceRequest is the request to add a resource.
type AddResourceRequest struct {
	Path        string `json:"path"`
	To          string `json:"to,omitempty"`
	Parent      string `json:"parent,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Instruction string `json:"instruction,omitempty"`
	Wait        bool   `json:"wait,omitempty"`
}

// AddResourceResponse is the response from adding a resource.
type AddResourceResponse struct {
	URI    string `json:"uri"`
	Status string `json:"status"`
}

// WriteContentRequest is the request to write content.
type WriteContentRequest struct {
	URI      string `json:"uri"`
	Abstract string `json:"abstract,omitempty"`
	Overview string `json:"overview,omitempty"`
	Content  string `json:"content,omitempty"`
}

// ContentResponse holds content read from a URI.
type ContentResponse struct {
	URI      string `json:"uri"`
	Abstract string `json:"abstract"`
	Overview string `json:"overview"`
	Content  string `json:"content"`
}

// LsOptions are options for listing directory contents.
type LsOptions struct {
	Simple    bool `json:"simple,omitempty"`
	Recursive bool `json:"recursive,omitempty"`
}

// FSEntry represents a filesystem entry.
type FSEntry struct {
	URI      string `json:"uri"`
	Name     string `json:"name"`
	IsDir    bool   `json:"is_dir"`
	Abstract string `json:"abstract,omitempty"`
}

// FindRequest is the request for semantic search.
type FindRequest struct {
	Query       string `json:"query"`
	ContextType string `json:"context_type,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// FindResponse contains search results.
type FindResponse struct {
	Results []FindResult `json:"results"`
}

// FindResult is a single search result.
type FindResult struct {
	URI      string  `json:"uri"`
	Abstract string  `json:"abstract"`
	Overview string  `json:"overview"`
	Content  string  `json:"content"`
	Score    float64 `json:"score"`
}

// SearchRequest is a direct vector search request.
type SearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// SearchResponse contains direct search results.
type SearchResponse struct {
	Results []FindResult `json:"results"`
}

// Session represents a conversation session.
type Session struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// Message is a conversation message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Relation represents a relationship between two URIs.
type Relation struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

// AgentStartRequest is the request to start an agent session.
type AgentStartRequest struct {
	SessionID string `json:"session_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
}

// SystemStatus contains system health information.
type SystemStatus struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}
