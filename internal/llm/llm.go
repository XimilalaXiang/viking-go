package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Message represents a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Response is the result of an LLM completion call.
type Response struct {
	Content      string `json:"content"`
	FinishReason string `json:"finish_reason"`
	PromptTokens int    `json:"prompt_tokens"`
	CompTokens   int    `json:"completion_tokens"`
}

// Client is the interface for LLM providers.
type Client interface {
	Complete(messages []Message) (*Response, error)
	CompleteWithPrompt(prompt string) (*Response, error)
}

// Config configures an LLM client.
type Config struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	APIKey      string  `json:"api_key"`
	APIBase     string  `json:"api_base"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	MaxRetries  int     `json:"max_retries"`
}

// NewClient creates a new OpenAI-compatible LLM client.
func NewClient(cfg Config) Client {
	if cfg.APIBase == "" {
		cfg.APIBase = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 4096
	}
	return &openAIClient{
		cfg: cfg,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type openAIClient struct {
	cfg  Config
	http *http.Client
}

func (c *openAIClient) Complete(messages []Message) (*Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
			time.Sleep(delay)
		}
		resp, err := c.doRequest(messages)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("LLM completion failed after %d attempts: %w", c.cfg.MaxRetries+1, lastErr)
}

func (c *openAIClient) CompleteWithPrompt(prompt string) (*Response, error) {
	return c.Complete([]Message{{Role: "user", Content: prompt}})
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *openAIClient) doRequest(messages []Message) (*Response, error) {
	reqBody := chatRequest{
		Model:       c.cfg.Model,
		Messages:    messages,
		Temperature: c.cfg.Temperature,
	}
	if c.cfg.MaxTokens > 0 {
		reqBody.MaxTokens = c.cfg.MaxTokens
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	url := c.cfg.APIBase + "/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var apiResp chatResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", apiResp.Error.Message)
	}
	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return &Response{
		Content:      apiResp.Choices[0].Message.Content,
		FinishReason: apiResp.Choices[0].FinishReason,
		PromptTokens: apiResp.Usage.PromptTokens,
		CompTokens:   apiResp.Usage.CompletionTokens,
	}, nil
}
