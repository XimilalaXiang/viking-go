// Package bot provides an OpenAI-compatible chat API that uses viking-go's
// retrieval pipeline as context for LLM responses. Supports both streaming
// and non-streaming modes.
package bot

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/llm"
	"github.com/ximilala/viking-go/internal/retriever"
)

// Config configures the bot's OpenAPI channel.
type Config struct {
	SystemPrompt string `json:"system_prompt"`
	MaxContext   int    `json:"max_context"`
	TopK        int    `json:"top_k"`
}

// Handler provides HTTP handlers for OpenAI-compatible chat endpoints.
type Handler struct {
	llmClient llm.Client
	retriever *retriever.HierarchicalRetriever
	cfg       Config
}

// NewHandler creates a bot handler.
func NewHandler(llmClient llm.Client, ret *retriever.HierarchicalRetriever, cfg Config) *Handler {
	if cfg.MaxContext <= 0 {
		cfg.MaxContext = 4000
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = "You are a helpful assistant. Use the provided context to answer questions accurately. If the context doesn't contain relevant information, say so."
	}
	return &Handler{
		llmClient: llmClient,
		retriever: ret,
		cfg:       cfg,
	}
}

// ChatCompletionsRequest mirrors the OpenAI chat completions request format.
type ChatCompletionsRequest struct {
	Model       string       `json:"model"`
	Messages    []ChatMsg    `json:"messages"`
	Stream      bool         `json:"stream"`
	Temperature *float64     `json:"temperature,omitempty"`
	MaxTokens   *int         `json:"max_tokens,omitempty"`
	AccountID   string       `json:"account_id,omitempty"`
	OwnerSpace  string       `json:"owner_space,omitempty"`
}

// ChatMsg is a single message in the conversation.
type ChatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionsResponse mirrors the OpenAI non-streaming response.
type ChatCompletionsResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice is a single response choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      ChatMsg `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage tracks token usage.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk is a single SSE chunk for streaming responses.
type StreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

// StreamChoice is a streaming choice delta.
type StreamChoice struct {
	Index        int      `json:"index"`
	Delta        ChatMsg  `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

// RegisterRoutes adds bot endpoints to an HTTP mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/chat/completions", h.handleChatCompletions)
	mux.HandleFunc("/bot/chat", h.handleChatCompletions)
}

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req ChatCompletionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		http.Error(w, "messages required", http.StatusBadRequest)
		return
	}

	lastMsg := req.Messages[len(req.Messages)-1]
	if lastMsg.Role != "user" {
		http.Error(w, "last message must be from user", http.StatusBadRequest)
		return
	}

	context := h.retrieveContext(lastMsg.Content, req.AccountID, req.OwnerSpace)

	messages := h.buildMessages(req.Messages, context)

	if req.Stream {
		h.handleStreaming(w, messages, req.Model)
	} else {
		h.handleNonStreaming(w, messages, req.Model)
	}
}

func (h *Handler) retrieveContext(query, accountID, ownerSpace string) string {
	if h.retriever == nil {
		return ""
	}

	tq := retriever.TypedQuery{Query: query}
	reqCtx := ctx.RootContext()
	if accountID != "" {
		reqCtx.AccountID = accountID
	}

	result, err := h.retriever.Retrieve(tq, reqCtx, h.cfg.TopK)
	if err != nil {
		log.Printf("[bot] retrieval error: %v", err)
		return ""
	}

	if result == nil || len(result.MatchedContexts) == 0 {
		return ""
	}

	var sb strings.Builder
	totalLen := 0
	for i, mc := range result.MatchedContexts {
		chunk := fmt.Sprintf("[%d] %s (score: %.3f)\n%s\n\n", i+1, mc.URI, mc.Score, mc.Abstract)
		if totalLen+len(chunk) > h.cfg.MaxContext {
			break
		}
		sb.WriteString(chunk)
		totalLen += len(chunk)
	}
	return sb.String()
}

func (h *Handler) buildMessages(userMsgs []ChatMsg, context string) []llm.Message {
	messages := make([]llm.Message, 0, len(userMsgs)+2)

	sysPrompt := h.cfg.SystemPrompt
	if context != "" {
		sysPrompt += "\n\n## Retrieved Context\n\n" + context
	}
	messages = append(messages, llm.Message{Role: "system", Content: sysPrompt})

	for _, m := range userMsgs {
		messages = append(messages, llm.Message{Role: m.Role, Content: m.Content})
	}

	return messages
}

func (h *Handler) handleNonStreaming(w http.ResponseWriter, messages []llm.Message, model string) {
	resp, err := h.llmClient.Complete(messages)
	if err != nil {
		http.Error(w, "LLM error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	chatResp := ChatCompletionsResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{
			{
				Index:        0,
				Message:      ChatMsg{Role: "assistant", Content: resp.Content},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     resp.PromptTokens,
			CompletionTokens: resp.CompTokens,
			TotalTokens:      resp.PromptTokens + resp.CompTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chatResp)
}

func (h *Handler) handleStreaming(w http.ResponseWriter, messages []llm.Message, model string) {
	resp, err := h.llmClient.Complete(messages)
	if err != nil {
		http.Error(w, "LLM error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()

	// Simulate streaming by sending content in chunks
	content := resp.Content
	chunkSize := 20
	for i := 0; i < len(content); i += chunkSize {
		end := i + chunkSize
		if end > len(content) {
			end = len(content)
		}

		chunk := StreamChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []StreamChoice{
				{
					Index: 0,
					Delta: ChatMsg{Role: "assistant", Content: content[i:end]},
				},
			},
		}

		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	finishReason := "stop"
	doneChunk := StreamChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []StreamChoice{
			{
				Index:        0,
				Delta:        ChatMsg{},
				FinishReason: &finishReason,
			},
		},
	}
	data, _ := json.Marshal(doneChunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
