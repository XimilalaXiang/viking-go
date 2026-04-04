package parse

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// FileParser is the interface all document parsers must implement.
type FileParser interface {
	Parse(source string, isPath bool) (*ParseResult, error)
	Extensions() []string
	Name() string
}

// Registry maps file extensions to parsers and selects the correct one.
type Registry struct {
	mu         sync.RWMutex
	parsers    map[string]FileParser
	extMap     map[string]string // extension -> parser name
}

// NewRegistry creates a registry pre-loaded with all built-in parsers.
func NewRegistry() *Registry {
	r := &Registry{
		parsers: make(map[string]FileParser),
		extMap:  make(map[string]string),
	}
	r.Register(&MarkdownFileParser{})
	r.Register(&TextFileParser{})
	r.Register(&CodeFileParser{})
	r.Register(&HTMLFileParser{})
	r.Register(&PDFFileParser{})
	r.Register(&WordFileParser{})
	r.Register(&ExcelFileParser{})
	r.Register(&EPUBFileParser{})
	r.Register(&ZipFileParser{})
	r.Register(&PPTXFileParser{})
	return r
}

// Register adds a parser to the registry for all its supported extensions.
func (r *Registry) Register(p FileParser) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := p.Name()
	r.parsers[name] = p
	for _, ext := range p.Extensions() {
		r.extMap[strings.ToLower(ext)] = name
	}
}

// ParserFor returns the parser registered for the given file path, or nil.
func (r *Registry) ParserFor(path string) FileParser {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ext := strings.ToLower(filepath.Ext(path))
	name, ok := r.extMap[ext]
	if !ok {
		return nil
	}
	return r.parsers[name]
}

// CanParse reports whether any registered parser handles the given extension.
func (r *Registry) CanParse(path string) bool {
	return r.ParserFor(path) != nil
}

// ParseFile selects the correct parser and processes the file.
func (r *Registry) ParseFile(path string) (*ParseResult, error) {
	p := r.ParserFor(path)
	if p == nil {
		return nil, fmt.Errorf("no parser for %s", filepath.Ext(path))
	}
	return p.Parse(path, true)
}

// ParseContent parses raw content using the parser for the given filename.
func (r *Registry) ParseContent(content, filename string) (*ParseResult, error) {
	p := r.ParserFor(filename)
	if p == nil {
		return nil, fmt.Errorf("no parser for %s", filepath.Ext(filename))
	}
	return p.Parse(content, false)
}

// AllExtensions returns every extension the registry can handle.
func (r *Registry) AllExtensions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	exts := make([]string, 0, len(r.extMap))
	for ext := range r.extMap {
		exts = append(exts, ext)
	}
	return exts
}
