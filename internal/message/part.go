// Package message defines the Message + Part system for rich conversation tracking.
// A Message is composed of Parts: TextPart (plain text), ContextPart (memory/resource
// references), and ToolPart (tool call records with status, timing, and tokens).
package message

import "encoding/json"

// Part is a discriminated union of message components.
type Part interface {
	PartType() string
}

// TextPart holds plain text content.
type TextPart struct {
	Text string `json:"text"`
}

func (p TextPart) PartType() string { return "text" }

// ContextPart references a retrieved context (memory, resource, or skill).
type ContextPart struct {
	URI         string `json:"uri"`
	ContextType string `json:"context_type"` // "memory", "resource", "skill"
	Abstract    string `json:"abstract"`
}

func (p ContextPart) PartType() string { return "context" }

// ToolPart records a tool call with its input, output, status, and metrics.
type ToolPart struct {
	ToolID           string         `json:"tool_id"`
	ToolName         string         `json:"tool_name"`
	ToolURI          string         `json:"tool_uri,omitempty"`
	SkillURI         string         `json:"skill_uri,omitempty"`
	ToolInput        map[string]any `json:"tool_input,omitempty"`
	ToolOutput       string         `json:"tool_output,omitempty"`
	ToolStatus       string         `json:"tool_status"` // "pending", "running", "completed", "error"
	DurationMs       *float64       `json:"duration_ms,omitempty"`
	PromptTokens     *int           `json:"prompt_tokens,omitempty"`
	CompletionTokens *int           `json:"completion_tokens,omitempty"`
}

func (p ToolPart) PartType() string { return "tool" }

// partEnvelope wraps a Part for JSON (de)serialization with a "type" discriminator.
type partEnvelope struct {
	Type string `json:"type"`
}

// MarshalPart serializes a Part into JSON with a "type" field.
func MarshalPart(p Part) (json.RawMessage, error) {
	switch v := p.(type) {
	case TextPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			TextPart
		}{Type: "text", TextPart: v})
	case ContextPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			ContextPart
		}{Type: "context", ContextPart: v})
	case ToolPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolPart
		}{Type: "tool", ToolPart: v})
	default:
		return nil, nil
	}
}

// UnmarshalPart deserializes JSON into the appropriate Part type.
func UnmarshalPart(data json.RawMessage) (Part, error) {
	var env partEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	switch env.Type {
	case "text":
		var p TextPart
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "context":
		var p ContextPart
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "tool":
		var p ToolPart
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		return p, nil
	default:
		return TextPart{Text: string(data)}, nil
	}
}
