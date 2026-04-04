package storage

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// LockType distinguishes between point locks and subtree locks.
type LockType int

const (
	LockPoint   LockType = iota
	LockSubtree
)

// LockEntry records a single active lock.
type LockEntry struct {
	Path      string    `json:"path"`
	Type      LockType  `json:"type"`
	HandleID  string    `json:"handle_id"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// LockHandle groups all locks held by a single transaction/operation.
type LockHandle struct {
	ID            string       `json:"id"`
	CreatedAt     time.Time    `json:"created_at"`
	LastActiveAt  time.Time    `json:"last_active_at"`
	locks         []*LockEntry
}

// RedoEntry records a pending operation for crash recovery.
type RedoEntry struct {
	ID         string         `json:"id"`
	Operation  string         `json:"operation"`
	Params     map[string]any `json:"params"`
	CreatedAt  time.Time      `json:"created_at"`
	Status     string         `json:"status"` // "pending", "done"
}

const defaultLockExpire = 5 * time.Minute

// LockManager manages path-level locks with deadlock prevention via ordered acquisition.
type LockManager struct {
	mu         sync.Mutex
	handles    map[string]*LockHandle
	pathLocks  map[string]*LockEntry
	lockExpire time.Duration
	stopCh     chan struct{}

	// Redo log for crash recovery
	redoMu  sync.Mutex
	redoLog map[string]*RedoEntry
}

// NewLockManager creates a new lock manager.
func NewLockManager(lockExpire time.Duration) *LockManager {
	if lockExpire <= 0 {
		lockExpire = defaultLockExpire
	}
	lm := &LockManager{
		handles:    make(map[string]*LockHandle),
		pathLocks:  make(map[string]*LockEntry),
		lockExpire: lockExpire,
		stopCh:     make(chan struct{}),
		redoLog:    make(map[string]*RedoEntry),
	}
	go lm.cleanupLoop()
	return lm
}

// CreateHandle creates a new lock handle for a transaction.
func (lm *LockManager) CreateHandle() *LockHandle {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	h := &LockHandle{
		ID:           "lh_" + uuid.New().String()[:12],
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
	}
	lm.handles[h.ID] = h
	return h
}

// AcquirePoint locks a single path (non-recursive).
func (lm *LockManager) AcquirePoint(handle *LockHandle, path string) bool {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return lm.acquireLock(handle, path, LockPoint)
}

// AcquireSubtree locks a path and all its descendants.
func (lm *LockManager) AcquireSubtree(handle *LockHandle, path string) bool {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return lm.acquireLock(handle, path, LockSubtree)
}

// AcquireSubtreeBatch acquires subtree locks on multiple paths using ordered acquisition to prevent deadlock.
func (lm *LockManager) AcquireSubtreeBatch(handle *LockHandle, paths []string) bool {
	if len(paths) == 0 {
		return true
	}

	sorted := make([]string, len(paths))
	copy(sorted, paths)
	sort.Slice(sorted, func(i, j int) bool {
		if len(sorted[i]) != len(sorted[j]) {
			return len(sorted[i]) < len(sorted[j])
		}
		return sorted[i] < sorted[j]
	})

	lm.mu.Lock()
	defer lm.mu.Unlock()

	var acquired []string
	for _, p := range sorted {
		if !lm.acquireLock(handle, p, LockSubtree) {
			for _, a := range acquired {
				lm.releaseLock(handle, a)
			}
			return false
		}
		acquired = append(acquired, p)
	}

	return true
}

// Release releases all locks held by a handle.
func (lm *LockManager) Release(handle *LockHandle) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for _, entry := range handle.locks {
		delete(lm.pathLocks, entry.Path)
	}
	handle.locks = nil
	delete(lm.handles, handle.ID)
}

// ReleaseSelected releases specific paths from a handle.
func (lm *LockManager) ReleaseSelected(handle *LockHandle, paths []string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	pathSet := make(map[string]bool, len(paths))
	for _, p := range paths {
		pathSet[p] = true
	}

	for _, p := range paths {
		lm.releaseLock(handle, p)
	}

	var remaining []*LockEntry
	for _, e := range handle.locks {
		if !pathSet[e.Path] {
			remaining = append(remaining, e)
		}
	}
	handle.locks = remaining
}

// ActiveHandles returns all active lock handles.
func (lm *LockManager) ActiveHandles() map[string]*LockHandle {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	result := make(map[string]*LockHandle)
	for id, h := range lm.handles {
		if len(h.locks) > 0 {
			result[id] = h
		}
	}
	return result
}

// Stop shuts down the cleanup goroutine and releases all locks.
func (lm *LockManager) Stop() {
	close(lm.stopCh)
	lm.mu.Lock()
	defer lm.mu.Unlock()
	for _, h := range lm.handles {
		for _, e := range h.locks {
			delete(lm.pathLocks, e.Path)
		}
		h.locks = nil
	}
	lm.handles = make(map[string]*LockHandle)
}

// --- Redo log ---

// WriteRedo creates a redo log entry for crash recovery.
func (lm *LockManager) WriteRedo(operation string, params map[string]any) string {
	lm.redoMu.Lock()
	defer lm.redoMu.Unlock()

	id := "redo_" + uuid.New().String()[:12]
	lm.redoLog[id] = &RedoEntry{
		ID:        id,
		Operation: operation,
		Params:    params,
		CreatedAt: time.Now(),
		Status:    "pending",
	}
	return id
}

// MarkRedoDone marks a redo entry as completed.
func (lm *LockManager) MarkRedoDone(id string) {
	lm.redoMu.Lock()
	defer lm.redoMu.Unlock()

	if entry, ok := lm.redoLog[id]; ok {
		entry.Status = "done"
	}
}

// PendingRedos returns all unfinished redo entries.
func (lm *LockManager) PendingRedos() []*RedoEntry {
	lm.redoMu.Lock()
	defer lm.redoMu.Unlock()

	var result []*RedoEntry
	for _, entry := range lm.redoLog {
		if entry.Status == "pending" {
			result = append(result, entry)
		}
	}
	return result
}

// --- internal ---

func (lm *LockManager) acquireLock(handle *LockHandle, path string, lockType LockType) bool {
	existing, ok := lm.pathLocks[path]
	if ok {
		if existing.HandleID == handle.ID {
			return true
		}
		if existing.ExpiresAt.Before(time.Now()) {
			delete(lm.pathLocks, path)
		} else {
			return false
		}
	}

	// Check conflicts with all existing locks (subtree-aware)
	for p, e := range lm.pathLocks {
		if e.HandleID == handle.ID {
			continue
		}
		if isPathConflict(path, p, lockType, e.Type) {
			if e.ExpiresAt.Before(time.Now()) {
				delete(lm.pathLocks, p)
				continue
			}
			return false
		}
	}

	now := time.Now()
	entry := &LockEntry{
		Path:       path,
		Type:       lockType,
		HandleID:   handle.ID,
		AcquiredAt: now,
		ExpiresAt:  now.Add(lm.lockExpire),
	}
	lm.pathLocks[path] = entry
	handle.locks = append(handle.locks, entry)
	handle.LastActiveAt = now
	return true
}

func (lm *LockManager) releaseLock(handle *LockHandle, path string) {
	if existing, ok := lm.pathLocks[path]; ok {
		if existing.HandleID == handle.ID {
			delete(lm.pathLocks, path)
		}
	}
}

func isPathConflict(newPath, existingPath string, newType, existingType LockType) bool {
	if newType == LockSubtree && existingType == LockSubtree {
		return isAncestor(newPath, existingPath) || isAncestor(existingPath, newPath) || newPath == existingPath
	}
	if newType == LockSubtree {
		return isAncestor(newPath, existingPath) || newPath == existingPath
	}
	if existingType == LockSubtree {
		return isAncestor(existingPath, newPath) || newPath == existingPath
	}
	return newPath == existingPath
}

func isAncestor(parent, child string) bool {
	if parent == child {
		return false
	}
	if len(parent) >= len(child) {
		return false
	}
	p := parent
	if p[len(p)-1] != '/' {
		p += "/"
	}
	return len(child) > len(p) && child[:len(p)] == p
}

func (lm *LockManager) cleanupLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-lm.stopCh:
			return
		case <-ticker.C:
			lm.cleanupStale()
		}
	}
}

func (lm *LockManager) cleanupStale() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now()
	var staleHandles []string
	for id, h := range lm.handles {
		if now.Sub(h.LastActiveAt) > lm.lockExpire && len(h.locks) > 0 {
			staleHandles = append(staleHandles, id)
		}
	}

	for _, id := range staleHandles {
		h := lm.handles[id]
		log.Printf("[LockManager] releasing stale handle %s (%d locks)", id, len(h.locks))
		for _, e := range h.locks {
			delete(lm.pathLocks, e.Path)
		}
		h.locks = nil
		delete(lm.handles, id)
	}

	// Cleanup expired path locks
	for path, e := range lm.pathLocks {
		if e.ExpiresAt.Before(now) {
			delete(lm.pathLocks, path)
		}
	}

	// Cleanup old redo entries
	lm.redoMu.Lock()
	for id, entry := range lm.redoLog {
		if entry.Status == "done" && now.Sub(entry.CreatedAt) > 24*time.Hour {
			delete(lm.redoLog, id)
		}
	}
	lm.redoMu.Unlock()
}

// Stats returns current lock manager statistics.
func (lm *LockManager) Stats() map[string]any {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	return map[string]any{
		"active_handles": len(lm.handles),
		"active_locks":   len(lm.pathLocks),
		"lock_expire":    lm.lockExpire.String(),
	}
}

// LockStatus returns the lock status for a specific path (for debugging).
func (lm *LockManager) LockStatus(path string) *LockEntry {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return lm.pathLocks[path]
}

// IsLocked checks if a path is currently locked by any handle.
func (lm *LockManager) IsLocked(path string) bool {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	e, ok := lm.pathLocks[path]
	if !ok {
		return false
	}
	return e.ExpiresAt.After(time.Now())
}

// IsLockedBy checks if a path is locked by a specific handle.
func (lm *LockManager) IsLockedBy(path string, handleID string) bool {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	e, ok := lm.pathLocks[path]
	if !ok {
		return false
	}
	return e.HandleID == handleID && e.ExpiresAt.After(time.Now())
}

// FormatStats returns a human-readable summary of lock manager state.
func (lm *LockManager) FormatStats() string {
	stats := lm.Stats()
	return fmt.Sprintf("LockManager: %d handles, %d locks (expire=%v)",
		stats["active_handles"], stats["active_locks"], stats["lock_expire"])
}

// LockScope provides a convenient acquire/release pattern.
// Usage:
//
//	scope, err := lm.Scope(paths, "point")
//	if err != nil { ... }
//	defer scope.Release()
type LockScope struct {
	handle  *LockHandle
	manager *LockManager
}

// Release frees all locks held by this scope.
func (s *LockScope) Release() {
	if s.handle != nil && s.manager != nil {
		s.manager.Release(s.handle)
		s.handle = nil
	}
}

// Handle returns the underlying lock handle.
func (s *LockScope) Handle() *LockHandle {
	return s.handle
}

// Scope creates a LockScope that acquires locks on the given paths.
// mode can be "point" or "subtree".
func (lm *LockManager) Scope(paths []string, mode string) (*LockScope, error) {
	h := lm.CreateHandle()
	success := true

	switch mode {
	case "subtree":
		success = lm.AcquireSubtreeBatch(h, paths)
	default:
		for _, p := range paths {
			if !lm.AcquirePoint(h, p) {
				success = false
				break
			}
		}
	}

	if !success {
		lm.Release(h)
		return nil, fmt.Errorf("failed to acquire %s lock for %v", mode, paths)
	}

	return &LockScope{handle: h, manager: lm}, nil
}
