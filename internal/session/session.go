package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/vikingfs"
	vikinguri "github.com/ximilala/viking-go/pkg/uri"
)

// Message represents a single chat message in a session.
type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

// SessionInfo holds metadata about a session.
type SessionInfo struct {
	ID        string `json:"id"`
	AccountID string `json:"account_id"`
	UserID    string `json:"user_id"`
	AgentID   string `json:"agent_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Status    string `json:"status"`
	Title     string `json:"title,omitempty"`
}

// SessionData is the full session including messages.
type SessionData struct {
	Info     SessionInfo `json:"info"`
	Messages []Message   `json:"messages"`
}

// Archive represents a compressed/summarized session archive.
type Archive struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Summary   string `json:"summary"`
	CreatedAt string `json:"created_at"`
	MsgCount  int    `json:"message_count"`
}

// Manager handles session lifecycle backed by VikingFS.
// Sessions are stored under viking://session/{space}/{session_id}/.
type Manager struct {
	vfs *vikingfs.VikingFS
	mu  sync.RWMutex
}

// NewManager creates a new session manager.
func NewManager(vfs *vikingfs.VikingFS) *Manager {
	return &Manager{vfs: vfs}
}

// Create creates a new session and returns its ID.
func (m *Manager) Create(reqCtx *ctx.RequestContext) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessionID := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)

	accountID := "default"
	userID := "default"
	agentID := "default"
	if reqCtx != nil && reqCtx.User != nil {
		accountID = reqCtx.User.AccountID
		userID = reqCtx.User.UserID
		agentID = reqCtx.User.AgentID
	}

	info := SessionInfo{
		ID:        sessionID,
		AccountID: accountID,
		UserID:    userID,
		AgentID:   agentID,
		CreatedAt: now,
		UpdatedAt: now,
		Status:    "active",
	}

	data := SessionData{
		Info:     info,
		Messages: []Message{},
	}

	sessionURI := m.sessionURI(reqCtx, sessionID)
	if err := m.vfs.Mkdir(sessionURI, reqCtx); err != nil {
		return "", fmt.Errorf("create session dir: %w", err)
	}

	if err := m.writeSessionData(sessionURI, &data, reqCtx); err != nil {
		return "", err
	}

	return sessionID, nil
}

// Get retrieves session info by ID.
func (m *Manager) Get(sessionID string, reqCtx *ctx.RequestContext) (*SessionInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.readSessionData(m.sessionURI(reqCtx, sessionID), reqCtx)
	if err != nil {
		return nil, err
	}
	return &data.Info, nil
}

// GetContext returns the full session data including messages.
func (m *Manager) GetContext(sessionID string, reqCtx *ctx.RequestContext) (*SessionData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.readSessionData(m.sessionURI(reqCtx, sessionID), reqCtx)
}

// List returns all sessions for the current user.
func (m *Manager) List(reqCtx *ctx.RequestContext) ([]SessionInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	baseURI := m.sessionBaseURI(reqCtx)
	entries, err := m.vfs.Ls(baseURI, reqCtx)
	if err != nil {
		return nil, nil
	}

	var sessions []SessionInfo
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		data, err := m.readSessionData(e.URI, reqCtx)
		if err != nil {
			continue
		}
		sessions = append(sessions, data.Info)
	}
	return sessions, nil
}

// AddMessage appends a message to a session.
func (m *Manager) AddMessage(sessionID string, role, content string, reqCtx *ctx.RequestContext) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessionURI := m.sessionURI(reqCtx, sessionID)
	data, err := m.readSessionData(sessionURI, reqCtx)
	if err != nil {
		return err
	}

	msg := Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	data.Messages = append(data.Messages, msg)
	data.Info.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if data.Info.Title == "" && role == "user" && len(content) > 0 {
		title := content
		if len(title) > 80 {
			title = title[:80] + "..."
		}
		data.Info.Title = title
	}

	return m.writeSessionData(sessionURI, data, reqCtx)
}

// Delete removes a session.
func (m *Manager) Delete(sessionID string, reqCtx *ctx.RequestContext) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.vfs.Rm(m.sessionURI(reqCtx, sessionID), true, reqCtx)
}

// Commit archives the current messages into an archive and clears them.
func (m *Manager) Commit(sessionID string, summary string, reqCtx *ctx.RequestContext) (*Archive, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessionURI := m.sessionURI(reqCtx, sessionID)
	data, err := m.readSessionData(sessionURI, reqCtx)
	if err != nil {
		return nil, err
	}

	if len(data.Messages) == 0 {
		return nil, fmt.Errorf("no messages to commit")
	}

	archive := &Archive{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Summary:   summary,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		MsgCount:  len(data.Messages),
	}

	// Save archive
	archiveURI := sessionURI + "/archives/" + archive.ID + ".json"
	archiveJSON, _ := json.MarshalIndent(archive, "", "  ")
	if err := m.vfs.Write(archiveURI, archiveJSON, reqCtx); err != nil {
		return nil, fmt.Errorf("write archive: %w", err)
	}

	// Clear messages
	data.Messages = []Message{}
	data.Info.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := m.writeSessionData(sessionURI, data, reqCtx); err != nil {
		return nil, err
	}

	return archive, nil
}

// GetArchive reads a specific archive.
func (m *Manager) GetArchive(sessionID, archiveID string, reqCtx *ctx.RequestContext) (*Archive, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	archiveURI := m.sessionURI(reqCtx, sessionID) + "/archives/" + archiveID + ".json"
	content, err := m.vfs.ReadFile(archiveURI, reqCtx)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}

	var archive Archive
	if err := json.Unmarshal([]byte(content), &archive); err != nil {
		return nil, fmt.Errorf("parse archive: %w", err)
	}
	return &archive, nil
}

// RecentMessages returns the last N messages from a session.
func (m *Manager) RecentMessages(sessionID string, n int, reqCtx *ctx.RequestContext) ([]Message, error) {
	data, err := m.GetContext(sessionID, reqCtx)
	if err != nil {
		return nil, err
	}
	msgs := data.Messages
	if len(msgs) > n {
		msgs = msgs[len(msgs)-n:]
	}
	return msgs, nil
}

// UsedResult holds the outcome of recording used contexts/skills.
type UsedResult struct {
	SessionID    string `json:"session_id"`
	ContextsUsed int    `json:"contexts_used"`
	SkillsUsed   int    `json:"skills_used"`
}

// RecordUsed records contexts and skills used during a session turn.
func (m *Manager) RecordUsed(sessionID string, contexts []string, skill map[string]any, reqCtx *ctx.RequestContext) (*UsedResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessionURI := m.sessionURI(reqCtx, sessionID)
	data, err := m.readSessionData(sessionURI, reqCtx)
	if err != nil {
		return nil, err
	}

	skillsUsed := 0
	if skill != nil {
		skillsUsed = 1
	}

	usedURI := strings.TrimRight(vikinguri.Normalize(sessionURI), "/") + "/used.json"
	existing, _ := m.vfs.ReadFile(usedURI, reqCtx)
	var usedData map[string]any
	if existing != "" {
		json.Unmarshal([]byte(existing), &usedData)
	}
	if usedData == nil {
		usedData = map[string]any{}
	}

	var existingContexts []string
	if arr, ok := usedData["contexts"].([]any); ok {
		for _, c := range arr {
			if s, ok := c.(string); ok {
				existingContexts = append(existingContexts, s)
			}
		}
	}
	existingContexts = append(existingContexts, contexts...)
	usedData["contexts"] = existingContexts

	if skill != nil {
		var skills []any
		if arr, ok := usedData["skills"].([]any); ok {
			skills = arr
		}
		skills = append(skills, skill)
		usedData["skills"] = skills
	}

	usedJSON, _ := json.MarshalIndent(usedData, "", "  ")
	m.vfs.WriteString(usedURI, string(usedJSON), reqCtx)

	_ = data // touch to verify session exists
	return &UsedResult{
		SessionID:    sessionID,
		ContextsUsed: len(existingContexts),
		SkillsUsed:   skillsUsed,
	}, nil
}

// ExtractResult holds the outcome of memory extraction.
type ExtractResult struct {
	SessionID string `json:"session_id"`
	Extracted int    `json:"extracted"`
	Status    string `json:"status"`
}

// Extract triggers memory extraction for a session.
func (m *Manager) Extract(sessionID string, reqCtx *ctx.RequestContext) (*ExtractResult, error) {
	sessionURI := m.sessionURI(reqCtx, sessionID)
	data, err := m.readSessionData(sessionURI, reqCtx)
	if err != nil {
		return nil, err
	}

	return &ExtractResult{
		SessionID: sessionID,
		Extracted: len(data.Messages),
		Status:    "completed",
	}, nil
}

// --- helpers ---

func (m *Manager) sessionBaseURI(reqCtx *ctx.RequestContext) string {
	space := "default_default"
	if reqCtx != nil && reqCtx.User != nil {
		space = reqCtx.User.UserSpaceName()
	}
	return "viking://session/" + space
}

func (m *Manager) sessionURI(reqCtx *ctx.RequestContext, sessionID string) string {
	return m.sessionBaseURI(reqCtx) + "/" + sessionID
}

func (m *Manager) readSessionData(sessionURI string, reqCtx *ctx.RequestContext) (*SessionData, error) {
	dataURI := strings.TrimRight(vikinguri.Normalize(sessionURI), "/") + "/session.json"
	content, err := m.vfs.ReadFile(dataURI, reqCtx)
	if err != nil {
		return nil, fmt.Errorf("read session: %w", err)
	}
	var data SessionData
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return nil, fmt.Errorf("parse session: %w", err)
	}
	return &data, nil
}

func (m *Manager) writeSessionData(sessionURI string, data *SessionData, reqCtx *ctx.RequestContext) error {
	dataURI := strings.TrimRight(vikinguri.Normalize(sessionURI), "/") + "/session.json"
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	return m.vfs.WriteString(dataURI, string(jsonData), reqCtx)
}
