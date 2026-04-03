package session

import (
	"fmt"
	"log"
	"strings"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/embedder"
	"github.com/ximilala/viking-go/internal/indexer"
	"github.com/ximilala/viking-go/internal/llm"
	"github.com/ximilala/viking-go/internal/memory"
	"github.com/ximilala/viking-go/internal/storage"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

// ExtractionStats tracks memory extraction outcomes.
type ExtractionStats struct {
	Created int `json:"created"`
	Merged  int `json:"merged"`
	Deleted int `json:"deleted"`
	Skipped int `json:"skipped"`
}

var alwaysMergeCategories = map[memory.MemoryCategory]bool{
	memory.CatProfile: true,
}

var mergeSupportedCategories = map[memory.MemoryCategory]bool{
	memory.CatPreferences: true,
	memory.CatEntities:    true,
	memory.CatPatterns:    true,
}

var toolSkillCategories = map[memory.MemoryCategory]bool{
	memory.CatTools:  true,
	memory.CatSkills: true,
}

// Compressor orchestrates memory extraction and deduplication from sessions.
type Compressor struct {
	extractor    *memory.Extractor
	deduplicator *memory.Deduplicator
	indexer      *indexer.Indexer
	vfs          *vikingfs.VikingFS
}

// NewCompressor creates a new session compressor.
func NewCompressor(
	llmClient llm.Client,
	emb embedder.Embedder,
	store *storage.Store,
	idx *indexer.Indexer,
	vfs *vikingfs.VikingFS,
) *Compressor {
	ext := memory.NewExtractor(llmClient, vfs)
	dedup := memory.NewDeduplicator(store, emb, llmClient)
	return &Compressor{
		extractor:    ext,
		deduplicator: dedup,
		indexer:      idx,
		vfs:          vfs,
	}
}

// Compress extracts memories from session messages, deduplicates, and persists them.
func (c *Compressor) Compress(
	messages []memory.SessionMessage,
	sessionID string,
	user string,
	historySummary string,
	reqCtx *ctx.RequestContext,
) (*ExtractionStats, error) {
	stats := &ExtractionStats{}

	candidates, err := c.extractor.Extract(messages, sessionID, user, historySummary)
	if err != nil {
		return stats, fmt.Errorf("extract memories: %w", err)
	}

	if len(candidates) == 0 {
		log.Printf("No candidate memories extracted from session %s", sessionID)
		return stats, nil
	}

	log.Printf("Extracted %d candidate memories from session %s", len(candidates), sessionID)

	for _, candidate := range candidates {
		if err := c.processCandidate(candidate, sessionID, reqCtx, stats); err != nil {
			log.Printf("Warning: failed to process candidate %q: %v", candidate.Abstract, err)
		}
	}

	log.Printf("Session %s compression: created=%d merged=%d deleted=%d skipped=%d",
		sessionID, stats.Created, stats.Merged, stats.Deleted, stats.Skipped)

	return stats, nil
}

func (c *Compressor) processCandidate(
	candidate memory.CandidateMemory,
	sessionID string,
	reqCtx *ctx.RequestContext,
	stats *ExtractionStats,
) error {
	if alwaysMergeCategories[candidate.Category] {
		return c.createAndIndex(candidate, sessionID, reqCtx, stats)
	}

	if toolSkillCategories[candidate.Category] {
		return c.createAndIndex(candidate, sessionID, reqCtx, stats)
	}

	result, err := c.deduplicator.Deduplicate(candidate, reqCtx)
	if err != nil {
		log.Printf("Dedup failed for %q, creating anyway: %v", candidate.Abstract, err)
		return c.createAndIndex(candidate, sessionID, reqCtx, stats)
	}

	switch result.Decision {
	case memory.DecisionSkip:
		log.Printf("Skipping duplicate memory: %s (reason: %s)", candidate.Abstract, result.Reason)
		stats.Skipped++
		return nil

	case memory.DecisionCreate:
		for _, action := range result.Actions {
			if action.Decision == memory.ActionDelete {
				c.deleteExisting(action.Memory, reqCtx, stats)
			}
		}
		return c.createAndIndex(candidate, sessionID, reqCtx, stats)

	case memory.DecisionNone:
		for _, action := range result.Actions {
			switch action.Decision {
			case memory.ActionMerge:
				if err := c.mergeIntoExisting(candidate, action.Memory, reqCtx); err != nil {
					log.Printf("Merge failed for %s: %v", action.Memory.URI, err)
				} else {
					stats.Merged++
				}
			case memory.ActionDelete:
				c.deleteExisting(action.Memory, reqCtx, stats)
			}
		}
		return nil

	default:
		return c.createAndIndex(candidate, sessionID, reqCtx, stats)
	}
}

func (c *Compressor) createAndIndex(
	candidate memory.CandidateMemory,
	sessionID string,
	reqCtx *ctx.RequestContext,
	stats *ExtractionStats,
) error {
	mem, err := c.extractor.CreateMemory(candidate, sessionID, reqCtx)
	if err != nil {
		return fmt.Errorf("create memory: %w", err)
	}

	if c.indexer != nil && mem != nil {
		if err := c.indexer.IndexContext(mem); err != nil {
			log.Printf("Warning: index memory %s failed: %v", mem.URI, err)
		}
	}

	stats.Created++
	return nil
}

func (c *Compressor) mergeIntoExisting(
	candidate memory.CandidateMemory,
	target *ctx.Context,
	reqCtx *ctx.RequestContext,
) error {
	existing, err := c.vfs.ReadFile(target.URI, reqCtx)
	if err != nil {
		return fmt.Errorf("read existing: %w", err)
	}

	merged := existing + "\n\n---\n\n" + candidate.Content
	if err := c.vfs.WriteString(target.URI, merged, reqCtx); err != nil {
		return fmt.Errorf("write merged: %w", err)
	}

	target.Abstract = candidate.Abstract
	target.VectorizeText = merged

	if c.indexer != nil {
		if err := c.indexer.IndexContext(target); err != nil {
			log.Printf("Warning: reindex merged %s failed: %v", target.URI, err)
		}
	}

	log.Printf("Merged memory into %s", target.URI)
	return nil
}

func (c *Compressor) deleteExisting(
	mem *ctx.Context,
	reqCtx *ctx.RequestContext,
	stats *ExtractionStats,
) {
	if err := c.vfs.Rm(mem.URI, false, reqCtx); err != nil {
		log.Printf("Warning: delete memory %s failed: %v", mem.URI, err)
		return
	}
	if c.indexer != nil {
		_, _ = c.indexer.DeleteByURI(mem.URI)
	}
	stats.Deleted++
	log.Printf("Deleted memory: %s", mem.URI)
}

// ChunkText splits text into overlapping chunks, preferring paragraph boundaries.
func ChunkText(text string, chunkSize, overlap int) []string {
	if len(text) <= chunkSize {
		return []string{text}
	}

	var chunks []string
	start := 0
	for start < len(text) {
		end := start + chunkSize
		if end > len(text) {
			end = len(text)
		}

		if end < len(text) {
			boundary := strings.LastIndex(text[start:end], "\n\n")
			if boundary > chunkSize/2 {
				end = start + boundary + 2
			}
		}

		chunk := strings.TrimSpace(text[start:end])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		start = end - overlap
		if start >= len(text) {
			break
		}
	}

	return chunks
}
