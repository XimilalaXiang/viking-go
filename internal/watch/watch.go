package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/indexer"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

const (
	tasksStorageURI = "viking://resources/.watch_tasks.json"
	defaultInterval = 60 * time.Minute
	scanTickerSec   = 60
)

// Task represents a directory watch/sync task.
type Task struct {
	ID            string    `json:"id"`
	SourcePath    string    `json:"source_path"`
	TargetURI     string    `json:"target_uri"`
	Interval      float64   `json:"interval_minutes"`
	BuildIndex    bool      `json:"build_index"`
	Reason        string    `json:"reason,omitempty"`
	Active        bool      `json:"active"`
	CreatedAt     time.Time `json:"created_at"`
	LastRun       time.Time `json:"last_run,omitempty"`
	NextRun       time.Time `json:"next_run"`
	AccountID     string    `json:"account_id"`
	FileHashes    map[string]string `json:"file_hashes,omitempty"`
}

func (t *Task) intervalDuration() time.Duration {
	if t.Interval <= 0 {
		return defaultInterval
	}
	return time.Duration(t.Interval * float64(time.Minute))
}

// Manager handles watch task lifecycle and persistence.
type Manager struct {
	vfs    *vikingfs.VikingFS
	tasks  map[string]*Task
	mu     sync.RWMutex
	reqCtx *ctx.RequestContext
}

func defaultReqCtx() *ctx.RequestContext {
	user := &ctx.UserIdentifier{AccountID: "default", UserID: "default", AgentID: "default"}
	return ctx.NewRequestContext(user, ctx.RoleRoot)
}

// NewManager creates a WatchManager backed by VikingFS for persistence.
func NewManager(vfs *vikingfs.VikingFS) *Manager {
	m := &Manager{
		vfs:    vfs,
		tasks:  make(map[string]*Task),
		reqCtx: defaultReqCtx(),
	}
	m.load()
	return m
}

func (m *Manager) load() {
	data, err := m.vfs.Read(tasksStorageURI, m.reqCtx)
	if err != nil {
		return
	}
	var tasks []*Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		log.Printf("[Watch] failed to parse stored tasks: %v", err)
		return
	}
	for _, t := range tasks {
		m.tasks[t.ID] = t
	}
	log.Printf("[Watch] loaded %d watch tasks", len(m.tasks))
}

func (m *Manager) save() {
	m.mu.RLock()
	tasks := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		tasks = append(tasks, t)
	}
	m.mu.RUnlock()

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		log.Printf("[Watch] save error: %v", err)
		return
	}
	if err := m.vfs.WriteString(tasksStorageURI, string(data), m.reqCtx); err != nil {
		log.Printf("[Watch] save error: %v", err)
	}
}

// Create registers a new watch task. Returns the task ID.
func (m *Manager) Create(sourcePath, targetURI, reason string, intervalMinutes float64, buildIndex bool) (string, error) {
	absPath, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("stat source: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source_path must be a directory")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.tasks {
		if t.SourcePath == absPath && t.TargetURI == targetURI && t.Active {
			return "", fmt.Errorf("active watch already exists for %s → %s (id: %s)", absPath, targetURI, t.ID)
		}
	}

	task := &Task{
		ID:         uuid.New().String(),
		SourcePath: absPath,
		TargetURI:  targetURI,
		Interval:   intervalMinutes,
		BuildIndex: buildIndex,
		Reason:     reason,
		Active:     true,
		CreatedAt:  time.Now(),
		NextRun:    time.Now(),
		AccountID:  "default",
		FileHashes: make(map[string]string),
	}
	m.tasks[task.ID] = task

	go m.save()
	return task.ID, nil
}

// List returns all tasks (or only active ones).
func (m *Manager) List(activeOnly bool) []*Task {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		if activeOnly && !t.Active {
			continue
		}
		result = append(result, t)
	}
	return result
}

// Cancel deactivates a watch task.
func (m *Manager) Cancel(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tasks[taskID]
	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}
	t.Active = false
	go m.save()
	return nil
}

// Delete removes a watch task permanently.
func (m *Manager) Delete(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.tasks[taskID]; !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}
	delete(m.tasks, taskID)
	go m.save()
	return nil
}

// Scheduler runs watch tasks on their schedule.
type Scheduler struct {
	manager  *Manager
	indexer  *indexer.Indexer
	vfs      *vikingfs.VikingFS
	stopCh   chan struct{}
	wg       sync.WaitGroup
	reqCtx   *ctx.RequestContext
}

// NewScheduler creates a scheduler that drives the watch manager.
func NewScheduler(mgr *Manager, idx *indexer.Indexer, vfs *vikingfs.VikingFS) *Scheduler {
	return &Scheduler{
		manager: mgr,
		indexer: idx,
		vfs:     vfs,
		stopCh:  make(chan struct{}),
		reqCtx:  defaultReqCtx(),
	}
}

// Start begins the background scheduling loop.
func (s *Scheduler) Start() {
	s.wg.Add(1)
	go s.run()
	log.Printf("[Watch] scheduler started (check interval: %ds)", scanTickerSec)
}

// Stop gracefully shuts down the scheduler.
func (s *Scheduler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
	log.Println("[Watch] scheduler stopped")
}

func (s *Scheduler) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Duration(scanTickerSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.checkAndExecute()
		}
	}
}

func (s *Scheduler) checkAndExecute() {
	now := time.Now()
	tasks := s.manager.List(true)

	for _, task := range tasks {
		if !task.Active || now.Before(task.NextRun) {
			continue
		}
		s.executeTask(task)
	}
}

func (s *Scheduler) executeTask(task *Task) {
	log.Printf("[Watch] executing task %s: %s → %s", task.ID, task.SourcePath, task.TargetURI)
	start := time.Now()

	changed, added, removed, err := s.syncDirectory(task)
	if err != nil {
		log.Printf("[Watch] task %s sync error: %v", task.ID, err)
	}

	if task.BuildIndex && s.indexer != nil && (changed > 0 || added > 0) {
		result, err := s.indexer.IndexDirectory(task.TargetURI, s.reqCtx)
		if err != nil {
			log.Printf("[Watch] task %s index error: %v", task.ID, err)
		} else {
			log.Printf("[Watch] task %s indexed: %d new, %d skipped", task.ID, result.Indexed, result.Skipped)
		}
	}

	s.manager.mu.Lock()
	task.LastRun = time.Now()
	task.NextRun = task.LastRun.Add(task.intervalDuration())
	s.manager.mu.Unlock()

	s.manager.save()
	log.Printf("[Watch] task %s done in %s (added=%d, changed=%d, removed=%d)",
		task.ID, time.Since(start).Round(time.Millisecond), added, changed, removed)
}

// syncDirectory walks the source directory, detects changes via SHA256,
// and copies new/changed files into VikingFS. Returns counts of changed, added, removed files.
func (s *Scheduler) syncDirectory(task *Task) (changed, added, removed int, err error) {
	currentHashes := make(map[string]string)
	supportedExts := map[string]bool{
		".md": true, ".txt": true, ".json": true, ".yaml": true, ".yml": true,
		".html": true, ".xml": true, ".csv": true, ".rst": true, ".org": true,
		".py": true, ".go": true, ".js": true, ".ts": true, ".sh": true,
	}

	err = filepath.WalkDir(task.SourcePath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == ".obsidian" || base == "node_modules" || base == ".trash" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !supportedExts[ext] {
			return nil
		}

		rel, _ := filepath.Rel(task.SourcePath, path)
		rel = filepath.ToSlash(rel)

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		hash := sha256Hash(data)
		currentHashes[rel] = hash

		prevHash := task.FileHashes[rel]
		if prevHash == hash {
			return nil
		}

		targetURI := strings.TrimRight(task.TargetURI, "/") + "/" + rel
		if writeErr := s.vfs.WriteString(targetURI, string(data), s.reqCtx); writeErr != nil {
			log.Printf("[Watch] write %s: %v", targetURI, writeErr)
			return nil
		}

		if prevHash == "" {
			added++
		} else {
			changed++
		}

		return nil
	})

	// Detect removed files
	s.manager.mu.Lock()
	for rel := range task.FileHashes {
		if _, exists := currentHashes[rel]; !exists {
			targetURI := strings.TrimRight(task.TargetURI, "/") + "/" + rel
			_ = s.vfs.Rm(targetURI, false, s.reqCtx)
			removed++
		}
	}
	task.FileHashes = currentHashes
	s.manager.mu.Unlock()

	return
}

func sha256Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
