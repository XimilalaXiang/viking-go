package fns

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/indexer"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

const (
	stateURI       = "viking://resources/.fns_sync_state.json"
	defaultPageSz  = 100
	syncBatchDelay = 50 * time.Millisecond
)

// SyncState tracks the last sync state for incremental syncing.
type SyncState struct {
	LastSyncAt    time.Time         `json:"last_sync_at"`
	NoteHashes    map[string]string `json:"note_hashes"`
	TotalNotes    int               `json:"total_notes"`
	TotalSynced   int               `json:"total_synced"`
}

// SyncResult reports what happened during a sync.
type SyncResult struct {
	Added    int           `json:"added"`
	Updated  int           `json:"updated"`
	Deleted  int           `json:"deleted"`
	Skipped  int           `json:"skipped"`
	Errors   int           `json:"errors"`
	Duration time.Duration `json:"duration"`
}

// Syncer connects Fast Note Sync to VikingFS.
type Syncer struct {
	client  *Client
	vfs     *vikingfs.VikingFS
	indexer *indexer.Indexer
	reqCtx  *ctx.RequestContext

	vault     string
	targetURI string
	maxRPM    int

	state SyncState
	mu    sync.Mutex
}

// NewSyncer creates an FNS syncer.
func NewSyncer(client *Client, vfs *vikingfs.VikingFS, idx *indexer.Indexer, vault, targetURI string, maxRPM int) *Syncer {
	user := &ctx.UserIdentifier{AccountID: "default", UserID: "default", AgentID: "default"}
	s := &Syncer{
		client:    client,
		vfs:       vfs,
		indexer:   idx,
		reqCtx:    ctx.NewRequestContext(user, ctx.RoleRoot),
		vault:     vault,
		targetURI: strings.TrimRight(targetURI, "/"),
		maxRPM:    maxRPM,
		state: SyncState{
			NoteHashes: make(map[string]string),
		},
	}
	s.loadState()
	return s
}

func (s *Syncer) loadState() {
	data, err := s.vfs.Read(stateURI, s.reqCtx)
	if err != nil {
		return
	}
	var state SyncState
	if err := json.Unmarshal(data, &state); err != nil {
		log.Printf("[FNS] failed to load sync state: %v", err)
		return
	}
	s.state = state
	if s.state.NoteHashes == nil {
		s.state.NoteHashes = make(map[string]string)
	}
	log.Printf("[FNS] loaded sync state: %d notes tracked, last sync: %s",
		len(s.state.NoteHashes), s.state.LastSyncAt.Format(time.RFC3339))
}

func (s *Syncer) saveState() {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		log.Printf("[FNS] save state error: %v", err)
		return
	}
	if err := s.vfs.WriteString(stateURI, string(data), s.reqCtx); err != nil {
		log.Printf("[FNS] save state error: %v", err)
	}
}

// Sync performs an incremental sync from FNS to VikingFS.
// It fetches all notes, detects changes via contentHash, and writes changed notes.
func (s *Syncer) Sync(buildIndex bool) (*SyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	start := time.Now()
	result := &SyncResult{}

	log.Printf("[FNS] starting sync: vault=%s → %s", s.vault, s.targetURI)

	seenPaths := make(map[string]bool)
	page := 1

	for {
		resp, err := s.client.ListNotes(s.vault, page, defaultPageSz)
		if err != nil {
			return nil, fmt.Errorf("list notes page %d: %w", page, err)
		}

		for _, meta := range resp.List {
			if seenPaths[meta.Path] {
				continue
			}
			seenPaths[meta.Path] = true

			prevHash := s.state.NoteHashes[meta.Path]

			// FNS returns multiple versions of the same path; we only want the latest.
			// The contentHash check handles dedup between versions.
			note, err := s.client.GetNote(s.vault, meta.Path)
			if err != nil {
				log.Printf("[FNS] get note %s: %v", meta.Path, err)
				result.Errors++
				continue
			}

			if note.ContentHash == prevHash && prevHash != "" {
				result.Skipped++
				continue
			}

			uri := s.targetURI + "/" + note.Path
			if err := s.vfs.WriteString(uri, note.Content, s.reqCtx); err != nil {
				log.Printf("[FNS] write %s: %v", uri, err)
				result.Errors++
				continue
			}

			if prevHash == "" {
				result.Added++
			} else {
				result.Updated++
			}

			s.state.NoteHashes[meta.Path] = note.ContentHash
			time.Sleep(syncBatchDelay)
		}

		if page*defaultPageSz >= resp.Pager.TotalRows {
			break
		}
		page++
	}

	// Detect deletions: notes we track but FNS no longer has
	for path := range s.state.NoteHashes {
		if !seenPaths[path] {
			uri := s.targetURI + "/" + path
			if err := s.vfs.Rm(uri, false, s.reqCtx); err != nil {
				log.Printf("[FNS] delete %s: %v", uri, err)
			}
			delete(s.state.NoteHashes, path)
			result.Deleted++
		}
	}

	s.state.LastSyncAt = time.Now()
	s.state.TotalNotes = len(seenPaths)
	s.state.TotalSynced = result.Added + result.Updated
	s.saveState()

	result.Duration = time.Since(start)

	if buildIndex && s.indexer != nil && (result.Added > 0 || result.Updated > 0) {
		log.Printf("[FNS] starting recursive index with maxRPM=%d", s.maxRPM)
		idxResult, err := s.indexer.IndexDirectoryRecursive(s.targetURI, s.reqCtx, s.maxRPM)
		if err != nil {
			log.Printf("[FNS] index error: %v", err)
		} else {
			log.Printf("[FNS] indexed: %d new, %d skipped, %d errors", idxResult.Indexed, idxResult.Skipped, idxResult.Errors)
		}
	}

	log.Printf("[FNS] sync complete: added=%d updated=%d deleted=%d skipped=%d errors=%d duration=%s",
		result.Added, result.Updated, result.Deleted, result.Skipped, result.Errors,
		result.Duration.Round(time.Millisecond))

	return result, nil
}

// Status returns the current sync state.
func (s *Syncer) Status() SyncState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}
