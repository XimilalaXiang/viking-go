package parse

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

// TreeBuilder moves a parsed document tree from a temporary VikingFS
// location to its final URI, optionally enqueuing the root for semantic
// processing. This mirrors the v5.0 Python TreeBuilder architecture:
//
//  1. Parser creates directory structure in temp VikingFS (no LLM calls)
//  2. TreeBuilder moves to final URI + enqueues to SemanticQueue
//  3. SemanticProcessor generates .abstract.md and .overview.md asynchronously
type TreeBuilder struct {
	vfs *vikingfs.VikingFS
}

// NewTreeBuilder creates a TreeBuilder backed by the given VikingFS.
func NewTreeBuilder(vfs *vikingfs.VikingFS) *TreeBuilder {
	return &TreeBuilder{vfs: vfs}
}

// BuildResult holds the outcome of FinalizeFromTemp.
type BuildResult struct {
	RootURI      string `json:"root_uri"`
	CandidateURI string `json:"candidate_uri,omitempty"`
	SourcePath   string `json:"source_path,omitempty"`
	SourceFormat string `json:"source_format,omitempty"`
}

// FinalizeConfig configures a FinalizeFromTemp call.
type FinalizeConfig struct {
	TempDirPath  string
	Scope        string // "resources", "user", "agent"
	ToURI        string // exact target URI (must not exist)
	ParentURI    string // target parent URI (must exist)
	SourcePath   string // original source file path or URL
	SourceFormat string // e.g. "repository", "file", etc.
	ReqCtx       *ctx.RequestContext
}

// FinalizeFromTemp moves a parsed document tree from a temp VikingFS
// directory to its permanent location. It returns the resolved root URI.
func (tb *TreeBuilder) FinalizeFromTemp(cfg FinalizeConfig) (*BuildResult, error) {
	if cfg.ReqCtx == nil {
		cfg.ReqCtx = &ctx.RequestContext{}
	}
	if cfg.Scope == "" {
		cfg.Scope = "resources"
	}

	entries, err := tb.vfs.Ls(cfg.TempDirPath, cfg.ReqCtx)
	if err != nil {
		return nil, fmt.Errorf("list temp dir: %w", err)
	}

	var docDirs []vikingfs.DirEntry
	for _, e := range entries {
		if e.IsDir && e.Name != "." && e.Name != ".." {
			docDirs = append(docDirs, e)
		}
	}
	if len(docDirs) != 1 {
		return nil, fmt.Errorf("expected 1 document directory in %s, found %d", cfg.TempDirPath, len(docDirs))
	}

	originalName := docDirs[0].Name
	docName := sanitizeSegment(originalName)
	tempDocURI := cfg.TempDirPath + "/" + originalName

	finalDocName := docName
	if cfg.SourcePath != "" && cfg.SourceFormat == "repository" {
		if parsed := parseCodeHostingURL(cfg.SourcePath); parsed != "" {
			finalDocName = parsed
		}
	}

	baseURI := tb.getBaseURI(cfg.Scope)
	if cfg.ParentURI != "" {
		baseURI = cfg.ParentURI
	}

	var candidateURI string
	if cfg.ToURI != "" {
		candidateURI = cfg.ToURI
	} else {
		if cfg.ParentURI != "" {
			if !tb.vfs.Exists(cfg.ParentURI, cfg.ReqCtx) {
				return nil, fmt.Errorf("parent URI does not exist: %s", cfg.ParentURI)
			}
		}
		candidateURI = baseURI + "/" + finalDocName
	}

	var finalURI string
	if cfg.ToURI != "" {
		finalURI = candidateURI
	} else {
		finalURI = tb.resolveUniqueURI(candidateURI, cfg.ReqCtx)
	}

	if err := tb.vfs.Mv(tempDocURI, finalURI, cfg.ReqCtx); err != nil {
		return nil, fmt.Errorf("move %s -> %s: %w", tempDocURI, finalURI, err)
	}

	log.Printf("[TreeBuilder] finalized %s -> %s", tempDocURI, finalURI)

	result := &BuildResult{
		RootURI:      finalURI,
		SourcePath:   cfg.SourcePath,
		SourceFormat: cfg.SourceFormat,
	}
	if cfg.ToURI == "" {
		result.CandidateURI = candidateURI
	}
	return result, nil
}

func (tb *TreeBuilder) getBaseURI(scope string) string {
	switch scope {
	case "resources":
		return "viking://resources"
	case "user":
		return "viking://user"
	case "agent":
		return "viking://agent"
	default:
		return "viking://resources"
	}
}

func (tb *TreeBuilder) resolveUniqueURI(uri string, reqCtx *ctx.RequestContext) string {
	if !tb.vfs.Exists(uri, reqCtx) {
		return uri
	}
	for i := 1; i <= 100; i++ {
		candidate := fmt.Sprintf("%s_%d", uri, i)
		if !tb.vfs.Exists(candidate, reqCtx) {
			return candidate
		}
	}
	return fmt.Sprintf("%s_%d", uri, 101)
}

// sanitizeSegment makes a name safe for use as a URI path segment.
func sanitizeSegment(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '\x00' {
			return '_'
		}
		return r
	}, name)
	if name == "" {
		name = "unnamed"
	}
	return name
}

// parseCodeHostingURL extracts "org/repo" from common Git hosting URLs.
func parseCodeHostingURL(urlStr string) string {
	for _, prefix := range []string{"https://github.com/", "https://gitlab.com/", "https://bitbucket.org/"} {
		if strings.HasPrefix(urlStr, prefix) {
			remainder := strings.TrimPrefix(urlStr, prefix)
			remainder = strings.TrimSuffix(remainder, ".git")
			parts := strings.SplitN(remainder, "/", 3)
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1]
			}
		}
	}
	return ""
}

// BuildParseTree takes a source file/directory, runs the appropriate parser,
// writes the parsed output into a temporary VikingFS directory, and returns
// the temp URI ready for FinalizeFromTemp.
func (tb *TreeBuilder) BuildParseTree(sourcePath string, reqCtx *ctx.RequestContext) (string, error) {
	if reqCtx == nil {
		reqCtx = &ctx.RequestContext{}
	}

	tempURI, err := tb.vfs.CreateTempURI(reqCtx)
	if err != nil {
		return "", fmt.Errorf("create temp URI: %w", err)
	}

	baseName := filepath.Base(sourcePath)
	docName := sanitizeSegment(strings.TrimSuffix(baseName, filepath.Ext(baseName)))
	docURI := tempURI + "/" + docName

	if err := tb.vfs.Mkdir(docURI, reqCtx); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", docURI, err)
	}

	reg := NewRegistry()
	ext := filepath.Ext(sourcePath)
	parser := reg.ParserFor(ext)
	if parser == nil {
		return "", fmt.Errorf("no parser for extension %q", ext)
	}

	result, err := parser.Parse(sourcePath, true)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", sourcePath, err)
	}

	fileName := baseName
	contentURI := docURI + "/" + sanitizeSegment(fileName)

	if err := tb.vfs.WriteString(contentURI, result.Content, reqCtx); err != nil {
		return "", fmt.Errorf("write content: %w", err)
	}

	if result.Abstract != "" {
		absURI := docURI + "/.abstract.md"
		if err := tb.vfs.WriteString(absURI, result.Abstract, reqCtx); err != nil {
			log.Printf("[TreeBuilder] warning: write abstract: %v", err)
		}
	}

	return tempURI, nil
}
