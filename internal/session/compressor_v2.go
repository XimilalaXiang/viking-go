package session

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/indexer"
	"github.com/ximilala/viking-go/internal/llm"
	"github.com/ximilala/viking-go/internal/memory"
	"github.com/ximilala/viking-go/internal/prompts"
	"github.com/ximilala/viking-go/internal/storage"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

// CompressorV2 uses schema-driven extraction with a ReAct-style loop.
// It replaces the V1 compressor by generating structured memory operations
// through LLM tool use, then applying them via MemoryUpdater with merge ops.
type CompressorV2 struct {
	llmClient     llm.Client
	store         *storage.Store
	indexer       *indexer.Indexer
	vfs           *vikingfs.VikingFS
	registry      *memory.MemoryTypeRegistry
	promptMgr     *prompts.Manager
	maxIterations int
}

// NewCompressorV2 creates a V2 compressor with optional schema registry.
func NewCompressorV2(
	llmClient llm.Client,
	store *storage.Store,
	idx *indexer.Indexer,
	vfs *vikingfs.VikingFS,
	registry *memory.MemoryTypeRegistry,
	promptMgr *prompts.Manager,
) *CompressorV2 {
	if registry == nil {
		registry = memory.NewMemoryTypeRegistry()
	}
	return &CompressorV2{
		llmClient:     llmClient,
		store:         store,
		indexer:        idx,
		vfs:           vfs,
		registry:      registry,
		promptMgr:     promptMgr,
		maxIterations: 3,
	}
}

// CompressV2 extracts structured memories from session messages using a ReAct loop.
func (c *CompressorV2) CompressV2(
	messages []memory.SessionMessage,
	sessionID string,
	user string,
	historySummary string,
	reqCtx *ctx.RequestContext,
) (*ExtractionStats, error) {
	stats := &ExtractionStats{}

	if len(messages) == 0 {
		return stats, nil
	}

	systemPrompt := c.buildSystemPrompt(reqCtx)
	conversationText := formatConversation(messages)

	reactMessages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: conversationText},
	}

	// Prefetch: list existing memory directories
	prefetchContext := c.prefetch(reqCtx)
	if prefetchContext != "" {
		reactMessages = append(reactMessages, llm.Message{
			Role: "user", Content: prefetchContext,
		})
	}

	tools := c.buildToolDefinitions()
	var finalOps *memory.MemoryOperations

	for iter := 0; iter < c.maxIterations; iter++ {
		isLast := iter == c.maxIterations-1

		resp, err := c.llmClient.ChatWithTools(reactMessages, tools)
		if err != nil {
			return stats, fmt.Errorf("LLM call (iter %d): %w", iter, err)
		}

		if len(resp.ToolCalls) > 0 && !isLast {
			toolResults := c.executeToolCalls(resp.ToolCalls, reqCtx)
			reactMessages = append(reactMessages, llm.Message{
				Role: "assistant", Content: resp.Content,
			})
			for _, tr := range toolResults {
				reactMessages = append(reactMessages, llm.Message{
					Role: "user", Content: fmt.Sprintf("[tool_result:%s] %s", tr.Name, tr.Result),
				})
			}
			continue
		}

		ops, err := parseMemoryOperations(resp.Content)
		if err != nil {
			log.Printf("[CompressorV2] parse operations failed (iter %d): %v", iter, err)
			if isLast {
				return stats, nil
			}
			continue
		}
		finalOps = ops
		break
	}

	if finalOps == nil || finalOps.IsEmpty() {
		log.Printf("[CompressorV2] no operations generated for session %s", sessionID)
		return stats, nil
	}

	updater := memory.NewMemoryUpdater(c.registry, c.vfs)
	result := updater.ApplyOperations(finalOps, reqCtx)

	stats.Created = len(result.WrittenURIs)
	stats.Merged = len(result.EditedURIs)
	stats.Deleted = len(result.DeletedURIs)
	stats.Skipped = len(result.Errors)

	// Index newly created memories
	if c.indexer != nil {
		for _, uri := range result.WrittenURIs {
			if _, err := c.indexer.IndexDirectory(uri, reqCtx); err != nil {
				log.Printf("[CompressorV2] index %s failed: %v", uri, err)
			}
		}
	}

	log.Printf("[CompressorV2] session %s: created=%d merged=%d deleted=%d skipped=%d",
		sessionID, stats.Created, stats.Merged, stats.Deleted, stats.Skipped)

	return stats, nil
}

func (c *CompressorV2) buildSystemPrompt(reqCtx *ctx.RequestContext) string {
	var sb strings.Builder

	if c.promptMgr != nil {
		if rendered, err := c.promptMgr.Render("memory_extract_v2", map[string]any{
			"schemas": c.registry.All(),
		}); err == nil {
			return rendered
		}
	}

	sb.WriteString("You are a memory extraction system. Analyze the conversation and extract structured memories.\n\n")

	schemas := c.registry.All()
	if len(schemas) > 0 {
		sb.WriteString("## Available Memory Types\n\n")
		for _, s := range schemas {
			sb.WriteString(fmt.Sprintf("### %s\n%s\n", s.MemoryType, s.Description))
			sb.WriteString(fmt.Sprintf("Directory: %s\n", s.Directory))
			sb.WriteString("Fields:\n")
			for _, f := range s.Fields {
				sb.WriteString(fmt.Sprintf("  - %s (%s, merge=%s): %s\n", f.Name, f.FieldType, f.MergeOp, f.Description))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString(`## Output Format
Return a JSON object with:
- "reasoning": Brief explanation of what you extracted
- "write_ops": Array of new memories to create
- "edit_ops": Array of edits to existing memories
- "edit_overview_ops": Array of overview edits
- "delete_uris": Array of URIs to delete

Each operation has: "type", "uri", "memory_type", "fields" (key-value map), "content"
`)

	sb.WriteString("\n## Tools Available\n")
	sb.WriteString("You can use 'read', 'ls', 'search' tools to inspect existing memories before deciding.\n")
	sb.WriteString("After investigation, output the final JSON operations.\n")

	return sb.String()
}

func (c *CompressorV2) prefetch(reqCtx *ctx.RequestContext) string {
	schemas := c.registry.All()
	if len(schemas) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Existing Memory Structure\n\n")

	for _, s := range schemas {
		dir := s.Directory
		if dir == "" {
			continue
		}
		entries, err := c.vfs.Ls(dir, reqCtx)
		if err != nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("### %s (%s) — %d entries\n", s.MemoryType, dir, len(entries)))
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("  - %s\n", e.Name))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (c *CompressorV2) buildToolDefinitions() []llm.ToolDef {
	return []llm.ToolDef{
		{
			Name:        "read",
			Description: "Read the content of a memory file at the given URI",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"uri": map[string]any{"type": "string", "description": "Viking URI to read"},
				},
				"required": []string{"uri"},
			},
		},
		{
			Name:        "ls",
			Description: "List files in a directory",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"uri": map[string]any{"type": "string", "description": "Viking URI to list"},
				},
				"required": []string{"uri"},
			},
		},
		{
			Name:        "search",
			Description: "Search for memories by text query",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query"},
					"limit": map[string]any{"type": "integer", "description": "Max results"},
				},
				"required": []string{"query"},
			},
		},
	}
}

type toolResult struct {
	Name   string
	Result string
}

func (c *CompressorV2) executeToolCalls(calls []llm.ToolCall, reqCtx *ctx.RequestContext) []toolResult {
	var results []toolResult
	for _, call := range calls {
		var result string
		switch call.Name {
		case "read":
			uri, _ := call.Arguments["uri"].(string)
			content, err := c.vfs.ReadFile(uri, reqCtx)
			if err != nil {
				result = fmt.Sprintf("error: %v", err)
			} else {
				result = content
			}
		case "ls":
			uri, _ := call.Arguments["uri"].(string)
			entries, err := c.vfs.Ls(uri, reqCtx)
			if err != nil {
				result = fmt.Sprintf("error: %v", err)
			} else {
				data, _ := json.Marshal(entries)
				result = string(data)
			}
		case "search":
			result = "search not available in this context"
		default:
			result = fmt.Sprintf("unknown tool: %s", call.Name)
		}
		results = append(results, toolResult{Name: call.Name, Result: result})
	}
	return results
}

func formatConversation(messages []memory.SessionMessage) string {
	var sb strings.Builder
	sb.WriteString("## Conversation to analyze:\n\n")
	for _, m := range messages {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n\n", m.Role, m.Content))
	}
	return sb.String()
}

func parseMemoryOperations(content string) (*memory.MemoryOperations, error) {
	jsonStart := strings.Index(content, "{")
	jsonEnd := strings.LastIndex(content, "}")
	if jsonStart < 0 || jsonEnd < 0 || jsonEnd <= jsonStart {
		return nil, fmt.Errorf("no JSON object found in response")
	}

	jsonStr := content[jsonStart : jsonEnd+1]
	var ops memory.MemoryOperations
	if err := json.Unmarshal([]byte(jsonStr), &ops); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return &ops, nil
}
