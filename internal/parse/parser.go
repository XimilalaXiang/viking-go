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
}

// Parser handles document parsing and ingestion into VikingFS.
type Parser struct {
	vfs     *vikingfs.VikingFS
	indexer *indexer.Indexer
}

// NewParser creates a new document parser.
func NewParser(vfs *vikingfs.VikingFS, idx *indexer.Indexer) *Parser {
	return &Parser{vfs: vfs, indexer: idx}
}

// SupportedExtensions lists file extensions this parser can handle.
var SupportedExtensions = map[string]bool{
	".md":       true,
	".txt":      true,
	".text":     true,
	".markdown": true,
	".rst":      true,
	".org":      true,
	".json":     true,
	".yaml":     true,
	".yml":      true,
	".toml":     true,
	".xml":      true,
	".csv":      true,
	".go":       true,
	".py":       true,
	".js":       true,
	".ts":       true,
	".java":     true,
	".c":        true,
	".cpp":      true,
	".h":        true,
	".rs":       true,
	".rb":       true,
	".php":      true,
	".sh":       true,
	".bash":     true,
	".sql":      true,
	".html":     true,
	".css":      true,
}

// ImportFile imports a local file into VikingFS at the given URI.
func (p *Parser) ImportFile(localPath, targetURI string, reqCtx *ctx.RequestContext) (*ParseResult, error) {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", localPath, err)
	}

	ext := strings.ToLower(filepath.Ext(localPath))
	if !SupportedExtensions[ext] {
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}

	content := string(data)
	title := filepath.Base(localPath)
	abstract := extractAbstract(content, title)

	if err := p.vfs.WriteContext(targetURI, abstract, "", content, title, reqCtx); err != nil {
		return nil, fmt.Errorf("write to VikingFS: %w", err)
	}

	if p.indexer != nil {
		if err := p.indexer.IndexFile(targetURI, abstract, reqCtx); err != nil {
			fmt.Printf("Warning: index %s: %v\n", targetURI, err)
		}
	}

	return &ParseResult{
		URI:      targetURI,
		Title:    title,
		Abstract: abstract,
		Content:  content,
	}, nil
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
			ext := strings.ToLower(filepath.Ext(name))
			if !SupportedExtensions[ext] {
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
