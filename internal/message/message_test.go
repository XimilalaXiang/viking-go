package message

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewUserMessage(t *testing.T) {
	msg := NewUserMessage("Hello, world!")
	if msg.Role != "user" {
		t.Errorf("expected user role, got %s", msg.Role)
	}
	if msg.Content() != "Hello, world!" {
		t.Errorf("wrong content: %s", msg.Content())
	}
	if !strings.HasPrefix(msg.ID, "msg_") {
		t.Errorf("expected msg_ prefix, got %s", msg.ID)
	}
}

func TestNewAssistantMessage(t *testing.T) {
	msg := NewAssistantMessage(
		"Here's what I found:",
		[]ContextPart{{URI: "viking://mem/1", ContextType: "memory", Abstract: "User prefers Python"}},
		[]ToolPart{{ToolID: "t1", ToolName: "web_search", ToolStatus: "completed", ToolOutput: "results..."}},
	)
	if msg.Role != "assistant" {
		t.Errorf("expected assistant role")
	}
	if len(msg.Parts) != 3 {
		t.Errorf("expected 3 parts, got %d", len(msg.Parts))
	}
	if len(msg.ContextParts()) != 1 {
		t.Error("expected 1 context part")
	}
	if len(msg.ToolParts()) != 1 {
		t.Error("expected 1 tool part")
	}
}

func TestMessageEstimatedTokens(t *testing.T) {
	msg := NewUserMessage("Hello, world!") // 13 chars -> ceil(13/4) = 4
	tokens := msg.EstimatedTokens()
	if tokens < 3 || tokens > 5 {
		t.Errorf("expected ~4 tokens, got %d", tokens)
	}
}

func TestFindToolPart(t *testing.T) {
	msg := NewAssistantMessage("", nil, []ToolPart{
		{ToolID: "t1", ToolName: "search"},
		{ToolID: "t2", ToolName: "read"},
	})
	found := msg.FindToolPart("t2")
	if found == nil {
		t.Fatal("expected to find tool t2")
	}
	if found.ToolName != "read" {
		t.Errorf("wrong tool name: %s", found.ToolName)
	}
	if msg.FindToolPart("t3") != nil {
		t.Error("should not find t3")
	}
}

func TestMessageJSONRoundTrip(t *testing.T) {
	original := NewAssistantMessage(
		"Response text",
		[]ContextPart{{URI: "viking://res/doc", ContextType: "resource", Abstract: "Doc about Go"}},
		[]ToolPart{{ToolID: "t1", ToolName: "code_exec", ToolStatus: "completed", ToolOutput: "42"}},
	)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID mismatch: %s vs %s", decoded.ID, original.ID)
	}
	if decoded.Role != "assistant" {
		t.Error("wrong role")
	}
	if len(decoded.Parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(decoded.Parts))
	}

	tp, ok := decoded.Parts[0].(TextPart)
	if !ok {
		t.Fatal("first part should be TextPart")
	}
	if tp.Text != "Response text" {
		t.Errorf("wrong text: %s", tp.Text)
	}

	cp, ok := decoded.Parts[1].(ContextPart)
	if !ok {
		t.Fatal("second part should be ContextPart")
	}
	if cp.URI != "viking://res/doc" {
		t.Error("wrong URI")
	}

	tool, ok := decoded.Parts[2].(ToolPart)
	if !ok {
		t.Fatal("third part should be ToolPart")
	}
	if tool.ToolName != "code_exec" {
		t.Error("wrong tool name")
	}
}

func TestMessageToJSONL(t *testing.T) {
	msg := NewUserMessage("test line")
	line, err := msg.ToJSONL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, `"role":"user"`) {
		t.Error("missing role in JSONL")
	}

	parsed, err := MessageFromJSONL(line)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Content() != "test line" {
		t.Errorf("wrong content: %s", parsed.Content())
	}
}

func TestPartMarshalUnmarshal(t *testing.T) {
	parts := []Part{
		TextPart{Text: "hello"},
		ContextPart{URI: "viking://test", ContextType: "memory", Abstract: "abs"},
		ToolPart{ToolID: "t1", ToolName: "search", ToolStatus: "pending"},
	}

	for _, p := range parts {
		data, err := MarshalPart(p)
		if err != nil {
			t.Fatalf("MarshalPart: %v", err)
		}
		decoded, err := UnmarshalPart(data)
		if err != nil {
			t.Fatalf("UnmarshalPart: %v", err)
		}
		if decoded.PartType() != p.PartType() {
			t.Errorf("type mismatch: %s vs %s", decoded.PartType(), p.PartType())
		}
	}
}

func TestAssemblerBasic(t *testing.T) {
	asm := NewAssembler(DefaultAssemblerConfig())

	messages := []*Message{
		NewUserMessage("What is Go?"),
		NewAssistantMessage("Go is a programming language.", nil, nil),
	}
	contexts := []ContextBlock{
		{URI: "viking://res/go-intro", ContextType: "resource", Abstract: "Go language introduction", Overview: "Go is a statically typed..."},
	}

	result := asm.Assemble("User asked about Go", messages, contexts)

	if result.MessagesUsed != 2 {
		t.Errorf("expected 2 messages used, got %d", result.MessagesUsed)
	}
	if result.ContextsUsed != 1 {
		t.Errorf("expected 1 context used, got %d", result.ContextsUsed)
	}
	if !strings.Contains(result.ContextText, "Retrieved Context") {
		t.Error("missing context header")
	}
	if !strings.Contains(result.ContextText, "Go language introduction") {
		t.Error("missing context abstract")
	}
	if !strings.Contains(result.ContextText, "Session Summary") {
		t.Error("missing summary")
	}
	if !strings.Contains(result.ContextText, "What is Go?") {
		t.Error("missing user message")
	}
}

func TestAssemblerTruncation(t *testing.T) {
	cfg := DefaultAssemblerConfig()
	cfg.MaxTokens = 20
	asm := NewAssembler(cfg)

	messages := make([]*Message, 0)
	for i := 0; i < 100; i++ {
		messages = append(messages, NewUserMessage(strings.Repeat("x", 100)))
	}

	result := asm.Assemble("", messages, nil)
	if !result.Truncated {
		t.Error("expected truncation")
	}
	if result.MessagesUsed >= 100 {
		t.Errorf("expected fewer messages, got %d", result.MessagesUsed)
	}
}

func TestAssemblerNoContextBlocks(t *testing.T) {
	cfg := DefaultAssemblerConfig()
	cfg.IncludeContextBlocks = false
	asm := NewAssembler(cfg)

	result := asm.Assemble("summary", []*Message{NewUserMessage("hi")}, []ContextBlock{
		{URI: "viking://a", ContextType: "memory", Abstract: "test"},
	})

	if strings.Contains(result.ContextText, "Retrieved Context") {
		t.Error("should not include context blocks when disabled")
	}
	if result.ContextsUsed != 0 {
		t.Errorf("expected 0 contexts, got %d", result.ContextsUsed)
	}
}

func TestAssemblerRecentMessagesLimit(t *testing.T) {
	cfg := DefaultAssemblerConfig()
	cfg.RecentMessagesCount = 3
	asm := NewAssembler(cfg)

	messages := make([]*Message, 10)
	for i := range messages {
		messages[i] = NewUserMessage("msg")
	}

	result := asm.Assemble("", messages, nil)
	if result.MessagesUsed > 3 {
		t.Errorf("expected at most 3 messages, got %d", result.MessagesUsed)
	}
}

func TestAssemblerToolParts(t *testing.T) {
	asm := NewAssembler(DefaultAssemblerConfig())
	msg := NewAssistantMessage("", nil, []ToolPart{
		{ToolID: "t1", ToolName: "web_search", ToolStatus: "completed", ToolOutput: "found 5 results"},
	})
	result := asm.Assemble("", []*Message{msg}, nil)
	if !strings.Contains(result.ContextText, "[ToolCall] web_search") {
		t.Error("missing tool call in output")
	}
	if !strings.Contains(result.ContextText, "[ToolResult] found 5 results") {
		t.Error("missing tool result in output")
	}
}
