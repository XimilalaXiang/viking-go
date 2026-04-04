package fns

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client communicates with the Fast Note Sync REST API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates an FNS API client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type apiResponse struct {
	Code    int             `json:"code"`
	Status  bool            `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type Vault struct {
	ID        int    `json:"id"`
	Name      string `json:"vault"`
	NoteCount int    `json:"noteCount"`
	FileCount int    `json:"fileCount"`
	NoteSize  int64  `json:"noteSize"`
	FileSize  int64  `json:"fileSize"`
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updatedAt"`
}

type NoteMeta struct {
	Path        string `json:"path"`
	PathHash    string `json:"pathHash"`
	Version     int    `json:"version"`
	Ctime       int64  `json:"ctime"`
	Mtime       int64  `json:"mtime"`
	Size        int    `json:"size"`
	LastTime    int64  `json:"lastTime"`
	UpdatedAt   string `json:"updatedAt"`
	CreatedAt   string `json:"createdAt"`
}

type Note struct {
	Path        string            `json:"path"`
	PathHash    string            `json:"pathHash"`
	Content     string            `json:"content"`
	ContentHash string            `json:"contentHash"`
	FileLinks   map[string]string `json:"fileLinks"`
	Version     int               `json:"version"`
	Ctime       int64             `json:"ctime"`
	Mtime       int64             `json:"mtime"`
	LastTime    int64             `json:"lastTime"`
	UpdatedAt   string            `json:"updatedAt"`
	CreatedAt   string            `json:"createdAt"`
}

type Pager struct {
	Page      int `json:"page"`
	PageSize  int `json:"pageSize"`
	TotalRows int `json:"totalRows"`
}

type NoteListResponse struct {
	List  []NoteMeta `json:"list"`
	Pager Pager      `json:"pager"`
}

type Folder struct {
	Path      string   `json:"path"`
	Name      string   `json:"name"`
	NoteCount int      `json:"noteCount"`
	FileCount int      `json:"fileCount"`
	Children  []Folder `json:"children,omitempty"`
}

type FolderTreeResponse struct {
	Folders []Folder `json:"folders"`
}

func (c *Client) do(method, path string, params url.Values) (json.RawMessage, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("token", c.token)
	req.Header.Set("User-Agent", "Viking-Go/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("FNS request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w (body: %s)", err, string(body[:min(200, len(body))]))
	}

	if !apiResp.Status {
		return nil, fmt.Errorf("FNS error code=%d: %s", apiResp.Code, apiResp.Message)
	}

	return apiResp.Data, nil
}

// ListVaults returns all vaults.
func (c *Client) ListVaults() ([]Vault, error) {
	data, err := c.do("GET", "/api/vault", url.Values{"limit": {"100"}})
	if err != nil {
		return nil, err
	}
	var vaults []Vault
	if err := json.Unmarshal(data, &vaults); err != nil {
		return nil, fmt.Errorf("parse vaults: %w", err)
	}
	return vaults, nil
}

// ListNotes returns a paginated list of notes for a vault, sorted by mtime desc.
func (c *Client) ListNotes(vault string, page, pageSize int) (*NoteListResponse, error) {
	params := url.Values{
		"vault":     {vault},
		"page":      {strconv.Itoa(page)},
		"pageSize":  {strconv.Itoa(pageSize)},
		"sortBy":    {"mtime"},
		"sortOrder": {"desc"},
	}
	data, err := c.do("GET", "/api/notes", params)
	if err != nil {
		return nil, err
	}
	var result NoteListResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse notes: %w", err)
	}
	return &result, nil
}

// GetNote returns a single note with content.
func (c *Client) GetNote(vault, path string) (*Note, error) {
	params := url.Values{
		"vault": {vault},
		"path":  {path},
	}
	data, err := c.do("GET", "/api/note", params)
	if err != nil {
		return nil, err
	}
	var note Note
	if err := json.Unmarshal(data, &note); err != nil {
		return nil, fmt.Errorf("parse note: %w", err)
	}
	return &note, nil
}

// GetFolderTree returns the folder tree for a vault.
func (c *Client) GetFolderTree(vault string, depth int) (*FolderTreeResponse, error) {
	params := url.Values{
		"vault": {vault},
	}
	if depth > 0 {
		params.Set("depth", strconv.Itoa(depth))
	}
	data, err := c.do("GET", "/api/folder/tree", params)
	if err != nil {
		return nil, err
	}
	var tree FolderTreeResponse
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("parse folder tree: %w", err)
	}
	return &tree, nil
}

// Health checks if the FNS service is healthy.
func (c *Client) Health() error {
	data, err := c.do("GET", "/api/health", nil)
	if err != nil {
		return err
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("parse health: %w", err)
	}
	if result.Status != "healthy" {
		return fmt.Errorf("FNS unhealthy: %s", result.Status)
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
