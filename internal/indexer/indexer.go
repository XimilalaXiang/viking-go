package indexer

import (
	"fmt"
	"log"
	"strings"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/embedder"
	"github.com/ximilala/viking-go/internal/metrics"
	"github.com/ximilala/viking-go/internal/storage"
	"github.com/ximilala/viking-go/internal/vikingfs"
	vikinguri "github.com/ximilala/viking-go/pkg/uri"
)

const maxEmbedInputChars = 2000

// Indexer vectorizes contexts from VikingFS and stores them in the SQLite store.
type Indexer struct {
	store    *storage.Store
	vfs      *vikingfs.VikingFS
	embedder embedder.Embedder
}

// New creates a new Indexer.
func New(store *storage.Store, vfs *vikingfs.VikingFS, emb embedder.Embedder) *Indexer {
	return &Indexer{store: store, vfs: vfs, embedder: emb}
}

// IndexResult holds stats from an indexing operation.
type IndexResult struct {
	Indexed int `json:"indexed"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

// IndexDirectory indexes a directory and its contents.
// For each directory: vectorizes L0 (abstract) and L1 (overview).
// For each file: vectorizes its content or abstract.
func (idx *Indexer) IndexDirectory(uri string, reqCtx *ctx.RequestContext) (*IndexResult, error) {
	if idx.embedder == nil {
		return nil, fmt.Errorf("embedder not configured")
	}

	result := &IndexResult{}
	normalizedURI := strings.TrimRight(vikinguri.Normalize(uri), "/")
	contextType := vikingfs.InferContextType(normalizedURI)
	if contextType == "" {
		contextType = "resource"
	}

	parsed, _ := vikinguri.Parse(normalizedURI)
	parentURI := ""
	if parsed != nil && parsed.Parent != nil {
		parentURI = parsed.Parent.URI()
	}

	accountID := "default"
	ownerSpace := ""
	if reqCtx != nil {
		accountID = reqCtx.AccountID
		ownerSpace = deriveOwnerSpace(normalizedURI, reqCtx)
	}

	// Index L0: abstract
	abstract, err := idx.vfs.Abstract(normalizedURI, reqCtx)
	if err == nil && abstract != "" {
		if err := idx.vectorizeAndStore(normalizedURI, abstract, abstract, parentURI,
			contextType, 0, false, accountID, ownerSpace); err != nil {
			log.Printf("[Indexer] L0 vectorize error for %s: %v", normalizedURI, err)
			result.Errors++
		} else {
			result.Indexed++
		}
	}

	// Index L1: overview
	overview, err := idx.vfs.Overview(normalizedURI, reqCtx)
	if err == nil && overview != "" {
		if err := idx.vectorizeAndStore(normalizedURI, abstract, overview, parentURI,
			contextType, 1, false, accountID, ownerSpace); err != nil {
			log.Printf("[Indexer] L1 vectorize error for %s: %v", normalizedURI, err)
			result.Errors++
		} else {
			result.Indexed++
		}
	}

	// Index files (L2)
	entries, err := idx.vfs.Ls(normalizedURI, reqCtx)
	if err != nil {
		return result, nil
	}

	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		name := entry.Name
		if strings.HasPrefix(name, ".") {
			continue
		}

		fileURI := entry.URI
		content, err := idx.vfs.ReadFile(fileURI, reqCtx)
		if err != nil {
			result.Skipped++
			continue
		}

		embedText := truncateText(content, maxEmbedInputChars)
		if embedText == "" {
			result.Skipped++
			continue
		}

		fileSummary := content
		if len(fileSummary) > 500 {
			fileSummary = fileSummary[:500]
		}

		if err := idx.vectorizeAndStore(fileURI, fileSummary, embedText, normalizedURI,
			contextType, 2, true, accountID, ownerSpace); err != nil {
			log.Printf("[Indexer] L2 vectorize error for %s: %v", fileURI, err)
			result.Errors++
		} else {
			result.Indexed++
		}
	}

	return result, nil
}

// IndexDirectoryRecursive indexes a directory tree, recursing into subdirectories.
// maxRPM controls the rate limit (requests per minute). Use 0 for no limit.
func (idx *Indexer) IndexDirectoryRecursive(uri string, reqCtx *ctx.RequestContext, maxRPM int) (*IndexResult, error) {
	if idx.embedder == nil {
		return nil, fmt.Errorf("embedder not configured")
	}

	total := &IndexResult{}
	var ticker *time.Ticker
	if maxRPM > 0 {
		interval := time.Minute / time.Duration(maxRPM)
		ticker = time.NewTicker(interval)
		defer ticker.Stop()
	}

	idx.indexRecursive(uri, reqCtx, total, ticker)
	return total, nil
}

func (idx *Indexer) indexRecursive(uri string, reqCtx *ctx.RequestContext, total *IndexResult, ticker *time.Ticker) {
	normalizedURI := strings.TrimRight(vikinguri.Normalize(uri), "/")
	contextType := vikingfs.InferContextType(normalizedURI)
	if contextType == "" {
		contextType = "resource"
	}

	parsed, _ := vikinguri.Parse(normalizedURI)
	parentURI := ""
	if parsed != nil && parsed.Parent != nil {
		parentURI = parsed.Parent.URI()
	}

	accountID := "default"
	ownerSpace := ""
	if reqCtx != nil {
		accountID = reqCtx.AccountID
		ownerSpace = deriveOwnerSpace(normalizedURI, reqCtx)
	}

	// L0: abstract
	abstract, err := idx.vfs.Abstract(normalizedURI, reqCtx)
	if err == nil && abstract != "" {
		if ticker != nil {
			<-ticker.C
		}
		if err := idx.vectorizeAndStore(normalizedURI, abstract, abstract, parentURI,
			contextType, 0, false, accountID, ownerSpace); err != nil {
			log.Printf("[Indexer] L0 error for %s: %v", normalizedURI, err)
			total.Errors++
		} else {
			total.Indexed++
		}
	}

	// L1: overview
	overview, err := idx.vfs.Overview(normalizedURI, reqCtx)
	if err == nil && overview != "" {
		if ticker != nil {
			<-ticker.C
		}
		if err := idx.vectorizeAndStore(normalizedURI, abstract, overview, parentURI,
			contextType, 1, false, accountID, ownerSpace); err != nil {
			log.Printf("[Indexer] L1 error for %s: %v", normalizedURI, err)
			total.Errors++
		} else {
			total.Indexed++
		}
	}

	entries, err := idx.vfs.Ls(normalizedURI, reqCtx)
	if err != nil {
		return
	}

	var subdirs []string
	for _, entry := range entries {
		if entry.IsDir {
			subdirs = append(subdirs, entry.URI)
			continue
		}
		if strings.HasPrefix(entry.Name, ".") {
			continue
		}

		content, err := idx.vfs.ReadFile(entry.URI, reqCtx)
		if err != nil {
			total.Skipped++
			continue
		}

		embedText := truncateText(content, maxEmbedInputChars)
		if embedText == "" {
			total.Skipped++
			continue
		}

		fileSummary := content
		if len(fileSummary) > 500 {
			fileSummary = fileSummary[:500]
		}

		if ticker != nil {
			<-ticker.C
		}
		if err := idx.vectorizeAndStore(entry.URI, fileSummary, embedText, normalizedURI,
			contextType, 2, true, accountID, ownerSpace); err != nil {
			log.Printf("[Indexer] L2 error for %s: %v", entry.URI, err)
			total.Errors++
		} else {
			total.Indexed++
		}

		if total.Indexed%100 == 0 && total.Indexed > 0 {
			log.Printf("[Indexer] progress: indexed=%d skipped=%d errors=%d", total.Indexed, total.Skipped, total.Errors)
		}
	}

	for _, subURI := range subdirs {
		idx.indexRecursive(subURI, reqCtx, total, ticker)
	}
}

// IndexFile indexes a single file.
func (idx *Indexer) IndexFile(uri string, summary string, reqCtx *ctx.RequestContext) error {
	if idx.embedder == nil {
		return fmt.Errorf("embedder not configured")
	}

	normalizedURI := vikinguri.Normalize(uri)
	contextType := vikingfs.InferContextType(normalizedURI)
	if contextType == "" {
		contextType = "resource"
	}

	parsed, _ := vikinguri.Parse(normalizedURI)
	parentURI := ""
	if parsed != nil && parsed.Parent != nil {
		parentURI = parsed.Parent.URI()
	}

	accountID := "default"
	ownerSpace := ""
	if reqCtx != nil {
		accountID = reqCtx.AccountID
		ownerSpace = deriveOwnerSpace(normalizedURI, reqCtx)
	}

	embedText := summary
	if embedText == "" {
		content, err := idx.vfs.ReadFile(normalizedURI, reqCtx)
		if err != nil {
			return fmt.Errorf("read file for indexing: %w", err)
		}
		embedText = truncateText(content, maxEmbedInputChars)
	}

	if embedText == "" {
		return nil
	}

	return idx.vectorizeAndStore(normalizedURI, summary, embedText, parentURI,
		contextType, 2, true, accountID, ownerSpace)
}

// IndexContext directly indexes a pre-built Context object with embedding.
func (idx *Indexer) IndexContext(c *ctx.Context) error {
	if idx.embedder == nil {
		return fmt.Errorf("embedder not configured")
	}

	text := c.VectorizeText
	if text == "" {
		text = c.Abstract
	}
	if text == "" {
		return nil
	}

	result, err := idx.embedder.Embed(truncateText(text, maxEmbedInputChars), false)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	c.Vector = result.DenseVector
	return idx.store.Upsert(c)
}

// DeleteByURI removes all indexed records for a URI and its children.
func (idx *Indexer) DeleteByURI(uri string) (int, error) {
	return idx.store.DeleteByFilter(storage.PathScope{
		Field:    "uri",
		BasePath: vikinguri.Normalize(uri),
		Depth:    -1,
	})
}

// UpdateURI updates the URI of indexed records when a file/directory is moved.
func (idx *Indexer) UpdateURI(oldURI, newURI string) error {
	oldNorm := strings.TrimRight(vikinguri.Normalize(oldURI), "/")
	newNorm := strings.TrimRight(vikinguri.Normalize(newURI), "/")

	results, err := idx.store.Query(
		storage.PathScope{Field: "uri", BasePath: oldNorm, Depth: -1},
		10000, 0, "", false,
	)
	if err != nil {
		return err
	}

	for _, c := range results {
		c.URI = strings.Replace(c.URI, oldNorm, newNorm, 1)
		parsed, _ := vikinguri.Parse(c.URI)
		if parsed != nil && parsed.Parent != nil {
			c.ParentURI = parsed.Parent.URI()
		}
		if err := idx.store.Upsert(c); err != nil {
			return fmt.Errorf("update URI %s: %w", c.URI, err)
		}
	}
	return nil
}

func (idx *Indexer) vectorizeAndStore(
	uri, abstract, embedText, parentURI, contextType string,
	level int, isLeaf bool,
	accountID, ownerSpace string,
) error {
	metrics.Inc("viking_embedding_requests_total")
	embResult, err := idx.embedder.Embed(truncateText(embedText, maxEmbedInputChars), false)
	if err != nil {
		metrics.Inc("viking_embedding_errors_total")
		return fmt.Errorf("embed: %w", err)
	}
	metrics.Inc("viking_index_upserts_total")

	c := ctx.NewContext(uri,
		ctx.WithAbstract(abstract),
		ctx.WithContextType(contextType),
		ctx.WithLevel(level),
		ctx.WithIsLeaf(isLeaf),
		ctx.WithAccountID(accountID),
		ctx.WithOwnerSpace(ownerSpace),
		ctx.WithParentURI(parentURI),
	)
	c.Vector = embResult.DenseVector
	return idx.store.Upsert(c)
}

func deriveOwnerSpace(uri string, reqCtx *ctx.RequestContext) string {
	if reqCtx == nil || reqCtx.User == nil {
		return ""
	}
	if strings.HasPrefix(uri, "viking://agent/") {
		return reqCtx.User.AgentSpaceName()
	}
	if strings.HasPrefix(uri, "viking://user/") || strings.HasPrefix(uri, "viking://session/") {
		return reqCtx.User.UserSpaceName()
	}
	return ""
}

func truncateText(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars] + "\n...(truncated for embedding)"
}
