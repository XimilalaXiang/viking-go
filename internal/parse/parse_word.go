package parse

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nguyenthenguyen/docx"
)

// WordFileParser handles .docx Word documents.
type WordFileParser struct{}

func (p *WordFileParser) Name() string { return "word" }

func (p *WordFileParser) Extensions() []string { return []string{".docx"} }

func (p *WordFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	if !isPath {
		return nil, fmt.Errorf("Word parsing requires a file path")
	}

	content, err := extractDocxText(source)
	if err != nil {
		return nil, fmt.Errorf("extract Word text: %w", err)
	}

	title := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	abstract := truncateStr(extractTextAbstract(content, title), 200)

	return &ParseResult{
		Title:    title,
		Abstract: abstract,
		Content:  content,
		Format:   "docx",
	}, nil
}

func extractDocxText(path string) (string, error) {
	r, err := docx.ReadDocxFile(path)
	if err != nil {
		return "", fmt.Errorf("open docx: %w", err)
	}
	defer r.Close()

	doc := r.Editable()
	content := doc.GetContent()

	content = strings.ReplaceAll(content, "</w:p>", "\n")
	content = strings.ReplaceAll(content, "</w:tr>", "\n")

	content = stripXMLTags(content)

	lines := strings.Split(content, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}

	return strings.Join(cleaned, "\n"), nil
}

func stripXMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	return b.String()
}
