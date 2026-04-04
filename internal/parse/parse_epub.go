package parse

import (
	"archive/zip"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// EPUBFileParser handles .epub ebook files.
// EPUBs are ZIP archives containing XHTML content files.
type EPUBFileParser struct{}

func (p *EPUBFileParser) Name() string { return "epub" }

func (p *EPUBFileParser) Extensions() []string { return []string{".epub"} }

func (p *EPUBFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	if !isPath {
		return nil, fmt.Errorf("EPUB parsing requires a file path")
	}

	chapters, err := extractEPUBChapters(source)
	if err != nil {
		return nil, fmt.Errorf("extract EPUB: %w", err)
	}

	title := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))

	var fullContent strings.Builder
	children := make([]ParseResult, 0, len(chapters))

	for _, ch := range chapters {
		children = append(children, ParseResult{
			Title:    ch.title,
			Abstract: truncateStr(ch.text, 200),
			Content:  ch.text,
			Format:   "epub_chapter",
		})
		if fullContent.Len() > 0 {
			fullContent.WriteString("\n\n---\n\n")
		}
		fullContent.WriteString(fmt.Sprintf("## %s\n\n%s", ch.title, ch.text))
	}

	abstract := fmt.Sprintf("%s (%d chapters)", title, len(chapters))

	return &ParseResult{
		Title:    title,
		Abstract: abstract,
		Content:  fullContent.String(),
		Children: children,
		Format:   "epub",
	}, nil
}

type epubChapter struct {
	title string
	text  string
}

func extractEPUBChapters(path string) ([]epubChapter, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open epub zip: %w", err)
	}
	defer r.Close()

	var chapters []epubChapter

	for _, f := range r.File {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext != ".xhtml" && ext != ".html" && ext != ".htm" {
			continue
		}

		// Skip non-content files (table of contents, navigation, etc.)
		lower := strings.ToLower(f.Name)
		if strings.Contains(lower, "nav") || strings.Contains(lower, "toc") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, 5*1024*1024))
		rc.Close()
		if err != nil {
			continue
		}

		htmlContent := string(data)
		markdown, docTitle := htmlToMarkdown(htmlContent)
		if strings.TrimSpace(markdown) == "" {
			continue
		}

		chTitle := docTitle
		if chTitle == "" {
			chTitle = strings.TrimSuffix(filepath.Base(f.Name), ext)
		}

		chapters = append(chapters, epubChapter{
			title: chTitle,
			text:  markdown,
		})
	}

	if len(chapters) == 0 {
		return nil, fmt.Errorf("no content chapters found in EPUB")
	}

	return chapters, nil
}
