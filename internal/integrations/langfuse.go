// Package integrations provides third-party service integrations.
// This file implements Langfuse tracing for LLM calls.
// Langfuse API: https://langfuse.com/docs/api
package integrations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/ximilala/viking-go/internal/llm"
)

// LangfuseConfig configures the Langfuse integration.
type LangfuseConfig struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	SecretKey string `json:"secret_key"`
	Host      string `json:"host"`
	// FlushInterval controls how often buffered events are flushed.
	FlushInterval time.Duration `json:"flush_interval"`
	// BufferSize is the max number of events before an automatic flush.
	BufferSize int `json:"buffer_size"`
}

// LangfuseTracer wraps an llm.Client to send traces to Langfuse.
type LangfuseTracer struct {
	inner  llm.Client
	cfg    LangfuseConfig
	client *http.Client

	mu     sync.Mutex
	buffer []langfuseEvent
	done   chan struct{}
}

// NewLangfuseTracer creates a tracing wrapper. If config is not enabled,
// it returns the inner client unwrapped.
func NewLangfuseTracer(inner llm.Client, cfg LangfuseConfig) llm.Client {
	if !cfg.Enabled || cfg.PublicKey == "" || cfg.SecretKey == "" {
		return inner
	}
	if cfg.Host == "" {
		cfg.Host = "https://cloud.langfuse.com"
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 50
	}

	t := &LangfuseTracer{
		inner:  inner,
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		buffer: make([]langfuseEvent, 0, cfg.BufferSize),
		done:   make(chan struct{}),
	}

	go t.flushLoop()
	return t
}

// Complete wraps the inner Complete with Langfuse tracing.
func (t *LangfuseTracer) Complete(messages []llm.Message) (*llm.Response, error) {
	start := time.Now()
	resp, err := t.inner.Complete(messages)
	duration := time.Since(start)

	t.recordGeneration("complete", messages, nil, resp, err, duration)
	return resp, err
}

// CompleteWithPrompt wraps CompleteWithPrompt.
func (t *LangfuseTracer) CompleteWithPrompt(prompt string) (*llm.Response, error) {
	msgs := []llm.Message{{Role: "user", Content: prompt}}
	start := time.Now()
	resp, err := t.inner.CompleteWithPrompt(prompt)
	duration := time.Since(start)

	t.recordGeneration("complete_prompt", msgs, nil, resp, err, duration)
	return resp, err
}

// ChatWithTools wraps ChatWithTools.
func (t *LangfuseTracer) ChatWithTools(messages []llm.Message, tools []llm.ToolDef) (*llm.Response, error) {
	start := time.Now()
	resp, err := t.inner.ChatWithTools(messages, tools)
	duration := time.Since(start)

	t.recordGeneration("chat_with_tools", messages, tools, resp, err, duration)
	return resp, err
}

// Close flushes remaining events and stops the flush loop.
func (t *LangfuseTracer) Close() {
	close(t.done)
	t.flush()
}

func (t *LangfuseTracer) recordGeneration(
	name string,
	input []llm.Message,
	tools []llm.ToolDef,
	resp *llm.Response,
	callErr error,
	duration time.Duration,
) {
	now := time.Now().UTC()
	traceID := fmt.Sprintf("trace-%d", now.UnixNano())

	evt := langfuseEvent{
		Type: "generation-create",
		Body: langfuseGeneration{
			TraceID:     traceID,
			Name:        name,
			StartTime:   now.Add(-duration).Format(time.RFC3339Nano),
			EndTime:     now.Format(time.RFC3339Nano),
			Model:       "default",
			Input:       input,
			Metadata:    map[string]any{},
			CompletionStartTime: now.Add(-duration / 2).Format(time.RFC3339Nano),
		},
	}

	if len(tools) > 0 {
		evt.Body.Metadata["tools_count"] = len(tools)
	}

	if resp != nil {
		evt.Body.Output = resp.Content
		evt.Body.Usage = &langfuseUsage{
			Input:  resp.PromptTokens,
			Output: resp.CompTokens,
			Total:  resp.PromptTokens + resp.CompTokens,
		}
		if len(resp.ToolCalls) > 0 {
			evt.Body.Metadata["tool_calls"] = resp.ToolCalls
		}
	}

	if callErr != nil {
		evt.Body.StatusMessage = callErr.Error()
		evt.Body.Level = "ERROR"
	} else {
		evt.Body.Level = "DEFAULT"
	}

	t.enqueue(evt)
}

func (t *LangfuseTracer) enqueue(evt langfuseEvent) {
	t.mu.Lock()
	t.buffer = append(t.buffer, evt)
	shouldFlush := len(t.buffer) >= t.cfg.BufferSize
	t.mu.Unlock()

	if shouldFlush {
		go t.flush()
	}
}

func (t *LangfuseTracer) flushLoop() {
	ticker := time.NewTicker(t.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.flush()
		case <-t.done:
			return
		}
	}
}

func (t *LangfuseTracer) flush() {
	t.mu.Lock()
	if len(t.buffer) == 0 {
		t.mu.Unlock()
		return
	}
	events := t.buffer
	t.buffer = make([]langfuseEvent, 0, t.cfg.BufferSize)
	t.mu.Unlock()

	batch := langfuseBatch{Batch: events}
	body, err := json.Marshal(batch)
	if err != nil {
		log.Printf("[langfuse] marshal error: %v", err)
		return
	}

	url := t.cfg.Host + "/api/public/ingestion"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[langfuse] request error: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(t.cfg.PublicKey, t.cfg.SecretKey)

	resp, err := t.client.Do(req)
	if err != nil {
		log.Printf("[langfuse] send error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[langfuse] API error (status %d): %s", resp.StatusCode, string(respBody))
	}
}

// --- Langfuse API types ---

type langfuseEvent struct {
	Type string              `json:"type"`
	Body langfuseGeneration  `json:"body"`
}

type langfuseGeneration struct {
	TraceID             string         `json:"traceId"`
	Name                string         `json:"name"`
	StartTime           string         `json:"startTime"`
	EndTime             string         `json:"endTime,omitempty"`
	CompletionStartTime string         `json:"completionStartTime,omitempty"`
	Model               string         `json:"model,omitempty"`
	Input               any            `json:"input,omitempty"`
	Output              any            `json:"output,omitempty"`
	Usage               *langfuseUsage `json:"usage,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
	Level               string         `json:"level,omitempty"`
	StatusMessage       string         `json:"statusMessage,omitempty"`
}

type langfuseUsage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
	Total  int `json:"total"`
}

type langfuseBatch struct {
	Batch []langfuseEvent `json:"batch"`
}
