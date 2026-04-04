package parse

import (
	"os"
	"path/filepath"
	"strings"
)

// TextFileParser handles plain-text and configuration files.
type TextFileParser struct{}

func (p *TextFileParser) Name() string { return "text" }

func (p *TextFileParser) Extensions() []string {
	return []string{
		".txt", ".text", ".rst", ".org", ".log",
		".json", ".yaml", ".yml", ".toml", ".xml", ".csv", ".tsv",
		".ini", ".cfg", ".conf", ".env", ".properties",
	}
}

func (p *TextFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
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
		title = "untitled.txt"
	}

	abstract := extractTextAbstract(content, title)

	return &ParseResult{
		Title:    title,
		Abstract: abstract,
		Content:  content,
		Format:   "text",
	}, nil
}

func extractTextAbstract(content, filename string) string {
	for _, line := range strings.SplitN(content, "\n", 10) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		return truncateStr(trimmed, 200)
	}
	return filename
}
