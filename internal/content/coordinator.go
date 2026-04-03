package content

import (
	"fmt"
	"log"
	"path"
	"strings"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/indexer"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

var derivedFilenames = map[string]bool{
	".abstract.md":    true,
	".overview.md":    true,
	".relations.json": true,
}

// WriteCoordinator coordinates content writes with downstream vector indexing.
type WriteCoordinator struct {
	vfs     *vikingfs.VikingFS
	indexer *indexer.Indexer
}

// NewWriteCoordinator creates a new content write coordinator.
func NewWriteCoordinator(vfs *vikingfs.VikingFS, idx *indexer.Indexer) *WriteCoordinator {
	return &WriteCoordinator{vfs: vfs, indexer: idx}
}

// WriteResult captures the outcome of a write operation.
type WriteResult struct {
	URI          string `json:"uri"`
	BytesWritten int    `json:"bytes_written"`
	Indexed      bool   `json:"indexed"`
}

// Write writes content to a URI and triggers vector indexing.
func (wc *WriteCoordinator) Write(
	uri, content string,
	mode string,
	reqCtx *ctx.RequestContext,
) (*WriteResult, error) {
	base := path.Base(uri)
	if derivedFilenames[base] {
		return nil, fmt.Errorf("cannot write to derived file %s directly", base)
	}

	switch mode {
	case "append":
		if err := wc.vfs.AppendFile(uri, content, reqCtx); err != nil {
			return nil, fmt.Errorf("append: %w", err)
		}
	default:
		if err := wc.vfs.WriteString(uri, content, reqCtx); err != nil {
			return nil, fmt.Errorf("write: %w", err)
		}
	}

	result := &WriteResult{
		URI:          uri,
		BytesWritten: len(content),
	}

	if wc.indexer != nil {
		if err := wc.indexer.IndexFile(uri, "", reqCtx); err != nil {
			log.Printf("Warning: index after write %s: %v", uri, err)
		} else {
			result.Indexed = true
		}
	}

	return result, nil
}

// WriteContext writes L0/L1/L2 content and indexes all levels.
func (wc *WriteCoordinator) WriteContext(
	uri, abstract, overview, content, contentFilename string,
	reqCtx *ctx.RequestContext,
) (*WriteResult, error) {
	if err := wc.vfs.WriteContext(uri, abstract, overview, content, contentFilename, reqCtx); err != nil {
		return nil, fmt.Errorf("write context: %w", err)
	}

	result := &WriteResult{
		URI:          uri,
		BytesWritten: len(abstract) + len(overview) + len(content),
	}

	if wc.indexer != nil {
		if _, err := wc.indexer.IndexDirectory(uri, reqCtx); err != nil {
			log.Printf("Warning: index after write context %s: %v", uri, err)
		} else {
			result.Indexed = true
		}
	}

	return result, nil
}

// Delete removes content and its vector index entries.
func (wc *WriteCoordinator) Delete(
	uri string,
	recursive bool,
	reqCtx *ctx.RequestContext,
) error {
	if err := wc.vfs.Rm(uri, recursive, reqCtx); err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	if wc.indexer != nil {
		_, _ = wc.indexer.DeleteByURI(uri)
	}

	return nil
}

// Move moves content and updates vector index entries.
func (wc *WriteCoordinator) Move(
	oldURI, newURI string,
	reqCtx *ctx.RequestContext,
) error {
	if err := wc.vfs.Mv(oldURI, newURI, reqCtx); err != nil {
		return fmt.Errorf("move: %w", err)
	}

	if wc.indexer != nil {
		wc.indexer.UpdateURI(oldURI, newURI)
	}

	return nil
}

// Reindex rebuilds vector index for a URI and its children.
func (wc *WriteCoordinator) Reindex(
	uri string,
	reqCtx *ctx.RequestContext,
) (*indexer.IndexResult, error) {
	if wc.indexer == nil {
		return nil, fmt.Errorf("indexer not available")
	}

	info, err := wc.vfs.Stat(uri, reqCtx)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", uri, err)
	}

	if info.IsDir() {
		return wc.indexer.IndexDirectory(uri, reqCtx)
	}
	return nil, wc.indexer.IndexFile(uri, "", reqCtx)
}

// InferContextType returns memory/skill/resource based on URI path.
func InferContextType(uri string) string {
	if strings.Contains(uri, "/memories") {
		return "memory"
	}
	if strings.Contains(uri, "/skills") {
		return "skill"
	}
	return "resource"
}
