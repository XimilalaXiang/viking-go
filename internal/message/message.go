package message

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Message is a conversation turn composed of multiple Parts.
type Message struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"` // "user" or "assistant"
	Parts     []Part    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// Content returns the text content of the first TextPart, or "".
func (m *Message) Content() string {
	for _, p := range m.Parts {
		if tp, ok := p.(TextPart); ok {
			return tp.Text
		}
	}
	return ""
}

// EstimatedTokens estimates the token count using a ceil(chars/4) heuristic.
func (m *Message) EstimatedTokens() int {
	totalChars := 0
	for _, p := range m.Parts {
		switch v := p.(type) {
		case TextPart:
			totalChars += len(v.Text)
		case ContextPart:
			totalChars += len(v.Abstract)
		case ToolPart:
			totalChars += len(v.ToolID) + len(v.ToolName)
			if v.ToolInput != nil {
				data, _ := json.Marshal(v.ToolInput)
				totalChars += len(data)
			}
			totalChars += len(v.ToolOutput)
		}
	}
	return (totalChars + 3) / 4
}

// ContextParts returns all ContextPart instances.
func (m *Message) ContextParts() []ContextPart {
	var parts []ContextPart
	for _, p := range m.Parts {
		if cp, ok := p.(ContextPart); ok {
			parts = append(parts, cp)
		}
	}
	return parts
}

// ToolParts returns all ToolPart instances.
func (m *Message) ToolParts() []ToolPart {
	var parts []ToolPart
	for _, p := range m.Parts {
		if tp, ok := p.(ToolPart); ok {
			parts = append(parts, tp)
		}
	}
	return parts
}

// FindToolPart finds a ToolPart by tool_id.
func (m *Message) FindToolPart(toolID string) *ToolPart {
	for _, p := range m.Parts {
		if tp, ok := p.(ToolPart); ok && tp.ToolID == toolID {
			return &[]ToolPart{tp}[0]
		}
	}
	return nil
}

// NewUserMessage creates a user message with plain text.
func NewUserMessage(content string) *Message {
	return &Message{
		ID:        "msg_" + uuid.New().String()[:8],
		Role:      "user",
		Parts:     []Part{TextPart{Text: content}},
		CreatedAt: time.Now().UTC(),
	}
}

// NewAssistantMessage creates an assistant message with optional parts.
func NewAssistantMessage(content string, contextRefs []ContextPart, toolCalls []ToolPart) *Message {
	var parts []Part
	if content != "" {
		parts = append(parts, TextPart{Text: content})
	}
	for _, ref := range contextRefs {
		parts = append(parts, ref)
	}
	for _, tc := range toolCalls {
		parts = append(parts, tc)
	}
	return &Message{
		ID:        "msg_" + uuid.New().String()[:8],
		Role:      "assistant",
		Parts:     parts,
		CreatedAt: time.Now().UTC(),
	}
}

// MarshalJSON serializes a Message to JSON including the parts array.
func (m *Message) MarshalJSON() ([]byte, error) {
	rawParts := make([]json.RawMessage, 0, len(m.Parts))
	for _, p := range m.Parts {
		data, err := MarshalPart(p)
		if err != nil {
			return nil, err
		}
		rawParts = append(rawParts, data)
	}
	return json.Marshal(struct {
		ID        string            `json:"id"`
		Role      string            `json:"role"`
		Parts     []json.RawMessage `json:"parts"`
		CreatedAt string            `json:"created_at"`
	}{
		ID:        m.ID,
		Role:      m.Role,
		Parts:     rawParts,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
	})
}

// UnmarshalJSON deserializes a Message from JSON.
func (m *Message) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID        string            `json:"id"`
		Role      string            `json:"role"`
		Parts     []json.RawMessage `json:"parts"`
		CreatedAt string            `json:"created_at"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.ID = raw.ID
	m.Role = raw.Role
	t, err := time.Parse(time.RFC3339, raw.CreatedAt)
	if err != nil {
		return fmt.Errorf("parse created_at: %w", err)
	}
	m.CreatedAt = t
	m.Parts = make([]Part, 0, len(raw.Parts))
	for _, rp := range raw.Parts {
		p, err := UnmarshalPart(rp)
		if err != nil {
			return err
		}
		m.Parts = append(m.Parts, p)
	}
	return nil
}

// ToJSONL serializes the message to a single JSONL line.
func (m *Message) ToJSONL() (string, error) {
	data, err := m.MarshalJSON()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// MessageFromJSONL parses a JSONL line into a Message.
func MessageFromJSONL(line string) (*Message, error) {
	var m Message
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return nil, err
	}
	return &m, nil
}
