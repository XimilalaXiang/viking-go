package message

import (
	"fmt"
	"strings"
)

// ContextBlock represents a retrieved context to inject into the session.
type ContextBlock struct {
	URI         string `json:"uri"`
	ContextType string `json:"context_type"`
	Abstract    string `json:"abstract"`
	Overview    string `json:"overview"`
	Content     string `json:"content"`
	Score       float64 `json:"score"`
}

// AssemblerConfig configures how session context is assembled.
type AssemblerConfig struct {
	MaxTokens           int  // total token budget
	IncludeContextBlocks bool // whether to prepend context blocks
	IncludeSessionSummary bool // whether to include the compression summary
	RecentMessagesCount  int  // max number of recent messages to include
}

// DefaultAssemblerConfig returns a sensible default configuration.
func DefaultAssemblerConfig() AssemblerConfig {
	return AssemblerConfig{
		MaxTokens:            128000,
		IncludeContextBlocks: true,
		IncludeSessionSummary: true,
		RecentMessagesCount:  50,
	}
}

// Assembler builds the final context string sent to the LLM.
type Assembler struct {
	config AssemblerConfig
}

// NewAssembler creates a new context assembler.
func NewAssembler(cfg AssemblerConfig) *Assembler {
	return &Assembler{config: cfg}
}

// AssembleResult is the output of context assembly.
type AssembleResult struct {
	ContextText    string `json:"context_text"`
	TotalTokens    int    `json:"total_tokens"`
	MessagesUsed   int    `json:"messages_used"`
	ContextsUsed   int    `json:"contexts_used"`
	Truncated      bool   `json:"truncated"`
}

// Assemble builds the full context string from session data.
func (a *Assembler) Assemble(
	summary string,
	messages []*Message,
	contextBlocks []ContextBlock,
) AssembleResult {
	var sb strings.Builder
	budget := a.config.MaxTokens
	contextsUsed := 0

	if a.config.IncludeContextBlocks && len(contextBlocks) > 0 {
		sb.WriteString("## Retrieved Context\n\n")
		for _, block := range contextBlocks {
			section := formatContextBlock(block)
			tokens := estimateTokens(section)
			if budget-tokens < 0 {
				break
			}
			sb.WriteString(section)
			sb.WriteString("\n")
			budget -= tokens
			contextsUsed++
		}
		sb.WriteString("---\n\n")
	}

	if a.config.IncludeSessionSummary && summary != "" {
		header := fmt.Sprintf("## Session Summary\n%s\n\n---\n\n", summary)
		tokens := estimateTokens(header)
		if budget-tokens >= 0 {
			sb.WriteString(header)
			budget -= tokens
		}
	}

	recentMsgs := messages
	if a.config.RecentMessagesCount > 0 && len(messages) > a.config.RecentMessagesCount {
		recentMsgs = messages[len(messages)-a.config.RecentMessagesCount:]
	}

	truncated := false
	messagesUsed := 0
	for _, msg := range recentMsgs {
		line := formatMessage(msg)
		tokens := estimateTokens(line)
		if budget-tokens < 0 {
			truncated = true
			break
		}
		sb.WriteString(line)
		budget -= tokens
		messagesUsed++
	}

	return AssembleResult{
		ContextText:  sb.String(),
		TotalTokens:  a.config.MaxTokens - budget,
		MessagesUsed: messagesUsed,
		ContextsUsed: contextsUsed,
		Truncated:    truncated,
	}
}

func formatContextBlock(block ContextBlock) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### [%s] %s\n", block.ContextType, block.Abstract))
	if block.Overview != "" {
		sb.WriteString(block.Overview)
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatMessage(msg *Message) string {
	var sb strings.Builder
	roleName := msg.Role
	if roleName == "user" {
		roleName = "User"
	} else {
		roleName = "Assistant"
	}

	for _, p := range msg.Parts {
		switch v := p.(type) {
		case TextPart:
			sb.WriteString(fmt.Sprintf("[%s]: %s\n", roleName, v.Text))
		case ContextPart:
			sb.WriteString(fmt.Sprintf("[Context:%s] %s\n", v.ContextType, v.Abstract))
		case ToolPart:
			sb.WriteString(fmt.Sprintf("[ToolCall] %s (status=%s)\n", v.ToolName, v.ToolStatus))
			if v.ToolOutput != "" {
				sb.WriteString(fmt.Sprintf("[ToolResult] %s\n", v.ToolOutput))
			}
		}
	}
	return sb.String()
}

func estimateTokens(text string) int {
	return (len(text) + 3) / 4
}
