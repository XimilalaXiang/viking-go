package parse

import (
	"os"
	"path/filepath"
	"strings"
)

// MarkdownFileParser handles .md and .markdown files.
type MarkdownFileParser struct{}

func (p *MarkdownFileParser) Name() string { return "markdown" }

func (p *MarkdownFileParser) Extensions() []string {
	return []string{".md", ".markdown", ".mdown", ".mkd"}
}

func (p *MarkdownFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	var content string
	var title string
	if isPath {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		content = string(data)
		title = filepath.Base(source)
	} else {
		content = source
		title = "untitled.md"
	}

	sections := ExtractMarkdownSections(content)
	abstract := extractMarkdownAbstract(content, title)

	children := make([]ParseResult, 0, len(sections))
	for _, sec := range sections {
		if sec.Content == "" && sec.Heading == "" {
			continue
		}
		children = append(children, ParseResult{
			Title:    sec.Heading,
			Abstract: truncateStr(sec.Content, 200),
			Content:  sec.Content,
		})
	}

	return &ParseResult{
		Title:    title,
		Abstract: abstract,
		Content:  content,
		Children: children,
		Format:   "markdown",
	}, nil
}

func extractMarkdownAbstract(content, filename string) string {
	for _, line := range strings.SplitN(content, "\n", 20) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimPrefix(trimmed, "# ")
		}
		return truncateStr(trimmed, 200)
	}
	return filename
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
