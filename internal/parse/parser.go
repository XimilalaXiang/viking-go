package parse

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/indexer"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

// ParseResult represents the output of parsing a file or directory.
type ParseResult struct {
	URI      string        `json:"uri"`
	Title    string        `json:"title"`
	Abstract string        `json:"abstract"`
	Content  string        `json:"content"`
	Children []ParseResult `json:"children,omitempty"`
	IsDir    bool          `json:"is_dir"`
	Format   string        `json:"format,omitempty"`
}

// Parser handles document parsing and ingestion into VikingFS.
type Parser struct {
	vfs      *vikingfs.VikingFS
	indexer  *indexer.Indexer
	registry *Registry
}

// NewParser creates a new document parser with the built-in registry.
func NewParser(vfs *vikingfs.VikingFS, idx *indexer.Indexer) *Parser {
	return &Parser{vfs: vfs, indexer: idx, registry: NewRegistry()}
}

// SupportedExtensions returns a dynamically-computed set from the registry.
// Kept for backward compatibility.
var SupportedExtensions = func() map[string]bool {
	r := NewRegistry()
	m := make(map[string]bool)
	for _, ext := range r.AllExtensions() {
		m[ext] = true
	}
	return m
}()

// ImportFile imports a local file into VikingFS at the given URI.
// It uses the parser registry to select the correct parser for the file type.
func (p *Parser) ImportFile(localPath, targetURI string, reqCtx *ctx.RequestContext) (*ParseResult, error) {
	ext := strings.ToLower(filepath.Ext(localPath))

	fp := p.registry.ParserFor(localPath)
	if fp != nil {
		result, err := fp.Parse(localPath, true)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", localPath, err)
		}

		if err := p.vfs.WriteContext(targetURI, result.Abstract, "", result.Content, result.Title, reqCtx); err != nil {
			return nil, fmt.Errorf("write to VikingFS: %w", err)
		}

		if p.indexer != nil {
			if err := p.indexer.IndexFile(targetURI, result.Abstract, reqCtx); err != nil {
				fmt.Printf("Warning: index %s: %v\n", targetURI, err)
			}
		}

		result.URI = targetURI
		return result, nil
	}

	return nil, fmt.Errorf("unsupported file type: %s", ext)
}

// ImportDirectory recursively imports a local directory into VikingFS.
func (p *Parser) ImportDirectory(localDir, targetURI string, reqCtx *ctx.RequestContext) (*ParseResult, error) {
	info, err := os.Stat(localDir)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", localDir, err)
	}
	if !info.IsDir() {
		return p.ImportFile(localDir, targetURI, reqCtx)
	}

	if err := p.vfs.Mkdir(targetURI, reqCtx); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", targetURI, err)
	}

	entries, err := os.ReadDir(localDir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", localDir, err)
	}

	result := &ParseResult{
		URI:   targetURI,
		Title: filepath.Base(localDir),
		IsDir: true,
	}

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		childPath := filepath.Join(localDir, name)
		childURI := targetURI + "/" + name

		if entry.IsDir() {
			child, err := p.ImportDirectory(childPath, childURI, reqCtx)
			if err != nil {
				fmt.Printf("Warning: import dir %s: %v\n", childPath, err)
				continue
			}
			result.Children = append(result.Children, *child)
		} else {
			if !p.registry.CanParse(name) {
				continue
			}
			child, err := p.ImportFile(childPath, childURI, reqCtx)
			if err != nil {
				fmt.Printf("Warning: import file %s: %v\n", childPath, err)
				continue
			}
			result.Children = append(result.Children, *child)
		}
	}

	if p.indexer != nil {
		if _, err := p.indexer.IndexDirectory(targetURI, reqCtx); err != nil {
			fmt.Printf("Warning: index dir %s: %v\n", targetURI, err)
		}
	}

	return result, nil
}

func extractAbstract(content, filename string) string {
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimPrefix(trimmed, "# ")
		}
		if len(trimmed) > 200 {
			return trimmed[:200] + "..."
		}
		return trimmed
	}

	return filename
}

// ExtractMarkdownSections splits markdown into sections by headings.
func ExtractMarkdownSections(content string) []Section {
	var sections []Section
	var current *Section
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			if current != nil {
				current.Content = strings.TrimSpace(current.Content)
				sections = append(sections, *current)
			}
			current = &Section{
				Heading: strings.TrimPrefix(trimmed, "# "),
				Level:   1,
			}
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			if current != nil {
				current.Content = strings.TrimSpace(current.Content)
				sections = append(sections, *current)
			}
			current = &Section{
				Heading: strings.TrimPrefix(trimmed, "## "),
				Level:   2,
			}
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			if current != nil {
				current.Content = strings.TrimSpace(current.Content)
				sections = append(sections, *current)
			}
			current = &Section{
				Heading: strings.TrimPrefix(trimmed, "### "),
				Level:   3,
			}
			continue
		}
		if current != nil {
			current.Content += line + "\n"
		} else {
			current = &Section{Heading: "", Level: 0}
			current.Content += line + "\n"
		}
	}

	if current != nil {
		current.Content = strings.TrimSpace(current.Content)
		sections = append(sections, *current)
	}

	return sections
}

// Section represents a heading-delimited section of a document.
type Section struct {
	Heading string `json:"heading"`
	Level   int    `json:"level"`
	Content string `json:"content"`
}
