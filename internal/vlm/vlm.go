// Package vlm provides Vision-Language Model support for understanding
// images, audio transcripts, and video frames via OpenAI-compatible
// multimodal APIs (GPT-4o, Qwen-VL, etc.).
package vlm

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// VLMResult holds the L0/L1/L2 understanding output.
type VLMResult struct {
	Abstract string         `json:"abstract"`
	Overview string         `json:"overview"`
	Detail   string         `json:"detail"`
	Meta     map[string]any `json:"meta,omitempty"`
}

// Config configures the VLM client.
type Config struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	APIKey      string  `json:"api_key"`
	APIBase     string  `json:"api_base"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
}

// Client provides vision-language model capabilities.
type Client struct {
	cfg    Config
	http   *http.Client
}

// NewClient creates a VLM client. Defaults to OpenAI-compatible endpoint.
func NewClient(cfg Config) *Client {
	if cfg.APIBase == "" {
		cfg.APIBase = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o"
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 1024
	}
	return &Client{
		cfg: cfg,
		http: &http.Client{Timeout: 120 * time.Second},
	}
}

// --- Content part types for multimodal messages ---

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature"`
}

type chatChoice struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// UnderstandImage sends an image to the VLM and returns a structured understanding.
func (c *Client) UnderstandImage(imagePath, context, instruction string) (*VLMResult, error) {
	dataURI, err := imageToDataURI(imagePath)
	if err != nil {
		return nil, fmt.Errorf("encode image: %w", err)
	}

	if instruction == "" {
		instruction = "Describe this image in detail. Provide: 1) A one-line abstract, 2) A paragraph overview, 3) Detailed text description of all visible content."
	}

	parts := []contentPart{
		{Type: "text", Text: buildVisionPrompt(instruction, context)},
		{Type: "image_url", ImageURL: &imageURL{URL: dataURI, Detail: "auto"}},
	}

	resp, err := c.call(parts)
	if err != nil {
		return nil, err
	}

	return parseVLMResponse(resp), nil
}

// UnderstandImageBytes is like UnderstandImage but accepts raw bytes.
func (c *Client) UnderstandImageBytes(data []byte, mimeType, context, instruction string) (*VLMResult, error) {
	if mimeType == "" {
		mimeType = "image/png"
	}
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))

	if instruction == "" {
		instruction = "Describe this image in detail."
	}

	parts := []contentPart{
		{Type: "text", Text: buildVisionPrompt(instruction, context)},
		{Type: "image_url", ImageURL: &imageURL{URL: dataURI, Detail: "auto"}},
	}

	resp, err := c.call(parts)
	if err != nil {
		return nil, err
	}

	return parseVLMResponse(resp), nil
}

// UnderstandMultipleImages sends multiple images in one call for batch processing.
func (c *Client) UnderstandMultipleImages(imagePaths []string, context, instruction string) ([]*VLMResult, error) {
	if len(imagePaths) == 0 {
		return nil, nil
	}

	if instruction == "" {
		instruction = "Describe each image separately. For each, provide a one-line abstract and detailed description."
	}

	parts := []contentPart{
		{Type: "text", Text: buildVisionPrompt(instruction, context)},
	}

	for _, p := range imagePaths {
		dataURI, err := imageToDataURI(p)
		if err != nil {
			continue
		}
		parts = append(parts, contentPart{
			Type:     "image_url",
			ImageURL: &imageURL{URL: dataURI, Detail: "auto"},
		})
	}

	resp, err := c.call(parts)
	if err != nil {
		return nil, err
	}

	result := parseVLMResponse(resp)
	results := make([]*VLMResult, len(imagePaths))
	for i := range results {
		results[i] = result
	}
	return results, nil
}

// DescribeForIndex produces a text description suitable for vector indexing.
func (c *Client) DescribeForIndex(imagePath, context string) (string, error) {
	result, err := c.UnderstandImage(imagePath, context,
		"Produce a plain text description of this image suitable for search indexing. "+
			"Include all visible text, objects, colors, layout, and any data shown in charts or tables.")
	if err != nil {
		return "", err
	}
	return result.Detail, nil
}

func (c *Client) call(parts []contentPart) (string, error) {
	req := chatRequest{
		Model:       c.cfg.Model,
		MaxTokens:   c.cfg.MaxTokens,
		Temperature: c.cfg.Temperature,
		Messages: []chatMessage{
			{Role: "user", Content: parts},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(c.cfg.APIBase, "/") + "/chat/completions"
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("VLM API call: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var resp chatResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if resp.Error != nil {
		return "", fmt.Errorf("VLM API error: %s", resp.Error.Message)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("VLM returned no choices")
	}

	return resp.Choices[0].Message.Content, nil
}

func buildVisionPrompt(instruction, context string) string {
	var sb strings.Builder
	sb.WriteString(instruction)
	if context != "" {
		sb.WriteString("\n\nContext: ")
		if len(context) > 500 {
			context = context[:500] + "..."
		}
		sb.WriteString(context)
	}
	sb.WriteString("\n\nRespond in this format:\n")
	sb.WriteString("ABSTRACT: <one line summary>\n")
	sb.WriteString("OVERVIEW: <detailed paragraph>\n")
	sb.WriteString("DETAIL: <full text description>\n")
	return sb.String()
}

func parseVLMResponse(content string) *VLMResult {
	result := &VLMResult{Meta: make(map[string]any)}

	if idx := strings.Index(content, "ABSTRACT:"); idx >= 0 {
		rest := content[idx+9:]
		if end := strings.Index(rest, "\nOVERVIEW:"); end >= 0 {
			result.Abstract = strings.TrimSpace(rest[:end])
		} else if end := strings.Index(rest, "\n"); end >= 0 {
			result.Abstract = strings.TrimSpace(rest[:end])
		} else {
			result.Abstract = strings.TrimSpace(rest)
		}
	}

	if idx := strings.Index(content, "OVERVIEW:"); idx >= 0 {
		rest := content[idx+9:]
		if end := strings.Index(rest, "\nDETAIL:"); end >= 0 {
			result.Overview = strings.TrimSpace(rest[:end])
		} else if end := strings.Index(rest, "\n\n"); end >= 0 {
			result.Overview = strings.TrimSpace(rest[:end])
		} else {
			result.Overview = strings.TrimSpace(rest)
		}
	}

	if idx := strings.Index(content, "DETAIL:"); idx >= 0 {
		result.Detail = strings.TrimSpace(content[idx+7:])
	}

	if result.Abstract == "" && result.Overview == "" && result.Detail == "" {
		lines := strings.SplitN(content, "\n", 2)
		result.Abstract = strings.TrimSpace(lines[0])
		if len(lines) > 1 {
			result.Detail = strings.TrimSpace(lines[1])
		}
		result.Overview = result.Abstract
	}

	return result
}

// --- Image helpers ---

var imageExtMIME = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".png": "image/png", ".gif": "image/gif",
	".webp": "image/webp", ".bmp": "image/bmp",
	".svg": "image/svg+xml",
}

func imageToDataURI(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	ext := strings.ToLower(filepath.Ext(path))
	mime, ok := imageExtMIME[ext]
	if !ok {
		mime = "image/png"
	}

	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data)), nil
}

// IsImage checks if a file path has an image extension.
func IsImage(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := imageExtMIME[ext]
	return ok
}

// SupportedImageExtensions returns all image extensions the VLM can handle.
func SupportedImageExtensions() []string {
	exts := make([]string, 0, len(imageExtMIME))
	for ext := range imageExtMIME {
		exts = append(exts, ext)
	}
	return exts
}
