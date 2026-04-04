package parse

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ximilala/viking-go/internal/vlm"
)

// ImageFileParser uses a VLM to understand image content.
// If no VLM client is configured, it falls back to metadata-only extraction.
type ImageFileParser struct {
	vlmClient *vlm.Client
}

// NewImageFileParser creates a parser optionally backed by a VLM client.
func NewImageFileParser(client *vlm.Client) *ImageFileParser {
	return &ImageFileParser{vlmClient: client}
}

func (p *ImageFileParser) Name() string { return "image" }

func (p *ImageFileParser) Extensions() []string {
	return []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg"}
}

func (p *ImageFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	if !isPath {
		return nil, fmt.Errorf("image parsing requires a file path")
	}

	title := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))

	if p.vlmClient != nil {
		result, err := p.vlmClient.UnderstandImage(source, "", "")
		if err != nil {
			return &ParseResult{
				Title:    title,
				Abstract: fmt.Sprintf("Image: %s (VLM error: %v)", title, err),
				Content:  fmt.Sprintf("[Image file: %s]\nVLM processing failed: %v", filepath.Base(source), err),
				Format:   "image",
			}, nil
		}

		return &ParseResult{
			Title:    title,
			Abstract: result.Abstract,
			Content:  formatImageContent(title, source, result),
			Format:   "image",
		}, nil
	}

	return &ParseResult{
		Title:    title,
		Abstract: fmt.Sprintf("Image: %s", title),
		Content:  fmt.Sprintf("[Image file: %s]\nNo VLM configured for image understanding.", filepath.Base(source)),
		Format:   "image",
	}, nil
}

func formatImageContent(title, path string, result *vlm.VLMResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	if result.Abstract != "" {
		sb.WriteString(fmt.Sprintf("**Summary:** %s\n\n", result.Abstract))
	}
	if result.Overview != "" {
		sb.WriteString(fmt.Sprintf("## Overview\n\n%s\n\n", result.Overview))
	}
	if result.Detail != "" {
		sb.WriteString(fmt.Sprintf("## Detailed Description\n\n%s\n", result.Detail))
	}
	return sb.String()
}
