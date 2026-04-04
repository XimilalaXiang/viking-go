package parse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AudioFileParser transcribes audio files using an OpenAI-compatible
// Whisper API and returns the transcript as text content.
type AudioFileParser struct {
	apiBase string
	apiKey  string
	model   string
	http    *http.Client
}

// AudioConfig configures the Whisper API endpoint.
type AudioConfig struct {
	APIBase string
	APIKey  string
	Model   string
}

// NewAudioFileParser creates a parser backed by a Whisper-compatible API.
// If cfg is nil, parsing degrades to metadata-only output.
func NewAudioFileParser(cfg *AudioConfig) *AudioFileParser {
	p := &AudioFileParser{http: &http.Client{Timeout: 300 * time.Second}}
	if cfg != nil {
		p.apiBase = cfg.APIBase
		p.apiKey = cfg.APIKey
		p.model = cfg.Model
	}
	if p.apiBase == "" {
		p.apiBase = "https://api.openai.com/v1"
	}
	if p.model == "" {
		p.model = "whisper-1"
	}
	return p
}

func (p *AudioFileParser) Name() string { return "audio" }

func (p *AudioFileParser) Extensions() []string {
	return []string{".mp3", ".wav", ".m4a", ".flac", ".ogg", ".opus", ".webm"}
}

func (p *AudioFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	if !isPath {
		return nil, fmt.Errorf("audio parsing requires a file path")
	}

	title := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))

	if p.apiKey == "" {
		return &ParseResult{
			Title:    title,
			Abstract: fmt.Sprintf("Audio: %s (no Whisper API configured)", title),
			Content:  fmt.Sprintf("[Audio file: %s]\nWhisper API not configured for transcription.", filepath.Base(source)),
			Format:   "audio",
		}, nil
	}

	transcript, err := p.transcribe(source)
	if err != nil {
		return &ParseResult{
			Title:    title,
			Abstract: fmt.Sprintf("Audio: %s (transcription failed)", title),
			Content:  fmt.Sprintf("[Audio file: %s]\nTranscription error: %v", filepath.Base(source), err),
			Format:   "audio",
		}, nil
	}

	abstract := truncateStr(extractAbstractFromText(transcript, title), 200)

	return &ParseResult{
		Title:    title,
		Abstract: abstract,
		Content:  fmt.Sprintf("# %s\n\n## Transcript\n\n%s", title, transcript),
		Format:   "audio",
	}, nil
}

func (p *AudioFileParser) transcribe(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open audio: %w", err)
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("copy audio data: %w", err)
	}

	_ = writer.WriteField("model", p.model)
	_ = writer.WriteField("response_format", "json")

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart: %w", err)
	}

	url := strings.TrimRight(p.apiBase, "/") + "/audio/transcriptions"
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("whisper API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("whisper API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return string(respBody), nil
	}

	return result.Text, nil
}
