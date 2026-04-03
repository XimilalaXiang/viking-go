package agent

import (
	"fmt"
	"log"
	"strings"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/memory"
	"github.com/ximilala/viking-go/internal/retriever"
	"github.com/ximilala/viking-go/internal/session"
	"github.com/ximilala/viking-go/internal/storage"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

// Bridge provides agent lifecycle hooks for transparent memory injection/extraction.
// External agents call these endpoints at key moments:
//   - BeforeAgentStart: retrieve relevant context for the upcoming conversation
//   - AfterAgentEnd:    extract and store memories from the completed conversation
//   - BeforeCompaction: compress session history and extract memories before truncation
type Bridge struct {
	store      *storage.Store
	vfs        *vikingfs.VikingFS
	retriever  *retriever.HierarchicalRetriever
	sessionMgr *session.Manager
	extractor  *memory.Extractor
}

// NewBridge creates an agent bridge.
func NewBridge(store *storage.Store, vfs *vikingfs.VikingFS, ret *retriever.HierarchicalRetriever, sessionMgr *session.Manager, extractor *memory.Extractor) *Bridge {
	return &Bridge{
		store:      store,
		vfs:        vfs,
		retriever:  ret,
		sessionMgr: sessionMgr,
		extractor:  extractor,
	}
}

// StartRequest is the input for BeforeAgentStart.
type StartRequest struct {
	SessionID string `json:"session_id"`
	Query     string `json:"query"`
	AgentID   string `json:"agent_id"`
	Limit     int    `json:"limit,omitempty"`
}

// StartResponse is the output of BeforeAgentStart.
type StartResponse struct {
	SessionID  string   `json:"session_id"`
	Context    string   `json:"context"`
	Memories   []string `json:"memories,omitempty"`
	Resources  []string `json:"resources,omitempty"`
	Skills     []string `json:"skills,omitempty"`
}

// BeforeAgentStart retrieves relevant context for the agent's upcoming conversation.
// It searches for memories, resources, and skills related to the query and
// returns them as injectable context.
func (b *Bridge) BeforeAgentStart(req StartRequest) (*StartResponse, error) {
	if req.Limit <= 0 {
		req.Limit = 10
	}

	reqCtx := makeReqCtx(req.AgentID)

	// Create or resume session
	if req.SessionID == "" {
		sid, err := b.sessionMgr.Create(reqCtx)
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
		req.SessionID = sid
	}

	// Retrieve relevant context
	tq := retriever.TypedQuery{
		Query: req.Query,
	}
	result, err := b.retriever.Retrieve(tq, reqCtx, req.Limit)
	if err != nil {
		log.Printf("[Agent] retrieval error: %v", err)
		return &StartResponse{SessionID: req.SessionID}, nil
	}

	resp := &StartResponse{SessionID: req.SessionID}
	var contextParts []string

	for _, mc := range result.MatchedContexts {
		summary := fmt.Sprintf("[%s] %s: %s", mc.ContextType, mc.URI, truncate(mc.Abstract, 300))
		switch mc.ContextType {
		case "memory":
			resp.Memories = append(resp.Memories, summary)
		case "skill":
			resp.Skills = append(resp.Skills, summary)
		default:
			resp.Resources = append(resp.Resources, summary)
		}
		contextParts = append(contextParts, summary)
	}

	if len(contextParts) > 0 {
		resp.Context = "## Relevant Context\n\n" + strings.Join(contextParts, "\n\n")
	}

	return resp, nil
}

// EndRequest is the input for AfterAgentEnd.
type EndRequest struct {
	SessionID string    `json:"session_id"`
	Messages  []Message `json:"messages"`
	Summary   string    `json:"summary,omitempty"`
	AgentID   string    `json:"agent_id"`
}

// Message represents a single conversation message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// EndResponse is the output of AfterAgentEnd.
type EndResponse struct {
	MemoriesExtracted int    `json:"memories_extracted"`
	SessionArchived   bool   `json:"session_archived"`
	ArchiveURI        string `json:"archive_uri,omitempty"`
}

// AfterAgentEnd extracts memories from the conversation and archives the session.
func (b *Bridge) AfterAgentEnd(req EndRequest) (*EndResponse, error) {
	reqCtx := makeReqCtx(req.AgentID)
	resp := &EndResponse{}

	// Add messages to session
	for _, msg := range req.Messages {
		if err := b.sessionMgr.AddMessage(req.SessionID, msg.Role, msg.Content, reqCtx); err != nil {
			log.Printf("[Agent] add message error: %v", err)
		}
	}

	// Extract memories if extractor is available
	if b.extractor != nil {
		sessionMsgs := toSessionMessages(req.Messages)
		memories, err := b.extractor.Extract(sessionMsgs, req.SessionID, "", "")
		if err != nil {
			log.Printf("[Agent] memory extraction error: %v", err)
		} else {
			resp.MemoriesExtracted = len(memories)
			for _, mem := range memories {
				uri := fmt.Sprintf("viking://user/default/memories/%s/%s.md", mem.Category, sanitizeFilename(mem.Abstract))
				if err := b.vfs.WriteString(uri, mem.Content, reqCtx); err != nil {
					log.Printf("[Agent] write memory error: %v", err)
				}
			}
		}
	}

	// Archive session
	summary := req.Summary
	if summary == "" {
		summary = "Agent session completed"
	}
	archive, err := b.sessionMgr.Commit(req.SessionID, summary, reqCtx)
	if err != nil {
		log.Printf("[Agent] commit session error: %v", err)
	} else {
		resp.SessionArchived = true
		if archive != nil {
			resp.ArchiveURI = archive.SessionID
		}
	}

	return resp, nil
}

// CompactRequest is the input for BeforeCompaction.
type CompactRequest struct {
	SessionID string    `json:"session_id"`
	Messages  []Message `json:"messages"`
	AgentID   string    `json:"agent_id"`
}

// CompactResponse is the output of BeforeCompaction.
type CompactResponse struct {
	MemoriesExtracted int `json:"memories_extracted"`
	MessagesProcessed int `json:"messages_processed"`
}

// BeforeCompaction extracts memories from messages about to be compacted/truncated.
func (b *Bridge) BeforeCompaction(req CompactRequest) (*CompactResponse, error) {
	reqCtx := makeReqCtx(req.AgentID)
	resp := &CompactResponse{
		MessagesProcessed: len(req.Messages),
	}

	// Add messages to session
	for _, msg := range req.Messages {
		_ = b.sessionMgr.AddMessage(req.SessionID, msg.Role, msg.Content, reqCtx)
	}

	// Extract memories before they're lost
	if b.extractor != nil {
		sessionMsgs := toSessionMessages(req.Messages)
		memories, err := b.extractor.Extract(sessionMsgs, req.SessionID, "", "")
		if err != nil {
			log.Printf("[Agent] compaction extraction error: %v", err)
		} else {
			resp.MemoriesExtracted = len(memories)
			for _, mem := range memories {
				uri := fmt.Sprintf("viking://user/default/memories/%s/%s.md", mem.Category, sanitizeFilename(mem.Abstract))
				_ = b.vfs.WriteString(uri, mem.Content, reqCtx)
			}
		}
	}

	return resp, nil
}

func makeReqCtx(agentID string) *ctx.RequestContext {
	if agentID == "" {
		agentID = "default"
	}
	user := &ctx.UserIdentifier{
		AccountID: "default",
		UserID:    "default",
		AgentID:   agentID,
	}
	return ctx.NewRequestContext(user, ctx.RoleRoot)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func toSessionMessages(msgs []Message) []memory.SessionMessage {
	result := make([]memory.SessionMessage, len(msgs))
	for i, m := range msgs {
		result[i] = memory.SessionMessage{Role: m.Role, Content: m.Content}
	}
	return result
}

func sanitizeFilename(name string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", " ", "-", ":", "_")
	name = r.Replace(name)
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}
