package server

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// TaskStatus represents the lifecycle state of a background task.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
)

// Task represents a background task with status tracking.
type Task struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	Status     TaskStatus `json:"status"`
	ResourceID string     `json:"resource_id,omitempty"`
	AccountID  string     `json:"account_id,omitempty"`
	UserID     string     `json:"user_id,omitempty"`
	Result     any        `json:"result,omitempty"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

const (
	maxTasks = 1000
	taskTTL  = 24 * time.Hour
)

// TaskTracker manages background tasks with in-memory storage.
type TaskTracker struct {
	tasks map[string]*Task
	mu    sync.RWMutex
}

// NewTaskTracker creates a new task tracker.
func NewTaskTracker() *TaskTracker {
	return &TaskTracker{tasks: make(map[string]*Task)}
}

// Create registers a new pending task and returns its ID.
func (tt *TaskTracker) Create(taskType, resourceID, accountID, userID string) string {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	tt.evictOld()

	id := "task_" + uuid.New().String()[:12]
	now := time.Now().UTC()
	tt.tasks[id] = &Task{
		ID:         id,
		Type:       taskType,
		Status:     TaskPending,
		ResourceID: resourceID,
		AccountID:  accountID,
		UserID:     userID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	return id
}

// MarkRunning transitions a task to running.
func (tt *TaskTracker) MarkRunning(id string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if t, ok := tt.tasks[id]; ok {
		t.Status = TaskRunning
		t.UpdatedAt = time.Now().UTC()
	}
}

// MarkCompleted transitions a task to completed with a result.
func (tt *TaskTracker) MarkCompleted(id string, result any) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if t, ok := tt.tasks[id]; ok {
		t.Status = TaskCompleted
		t.Result = result
		t.UpdatedAt = time.Now().UTC()
	}
}

// MarkFailed transitions a task to failed with an error message.
func (tt *TaskTracker) MarkFailed(id string, errMsg string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if t, ok := tt.tasks[id]; ok {
		t.Status = TaskFailed
		t.Error = errMsg
		t.UpdatedAt = time.Now().UTC()
	}
}

// Get retrieves a task by ID, filtered by owner.
func (tt *TaskTracker) Get(id, accountID, userID string) *Task {
	tt.mu.RLock()
	defer tt.mu.RUnlock()
	t, ok := tt.tasks[id]
	if !ok {
		return nil
	}
	if accountID != "" && t.AccountID != accountID {
		return nil
	}
	return t
}

// List returns tasks matching optional filters.
func (tt *TaskTracker) List(taskType, status, resourceID, accountID, userID string, limit int) []*Task {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	var result []*Task
	for _, t := range tt.tasks {
		if accountID != "" && t.AccountID != accountID {
			continue
		}
		if taskType != "" && t.Type != taskType {
			continue
		}
		if status != "" && string(t.Status) != status {
			continue
		}
		if resourceID != "" && t.ResourceID != resourceID {
			continue
		}
		result = append(result, t)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func (tt *TaskTracker) evictOld() {
	if len(tt.tasks) < maxTasks {
		return
	}
	cutoff := time.Now().Add(-taskTTL)
	for id, t := range tt.tasks {
		if t.UpdatedAt.Before(cutoff) {
			delete(tt.tasks, id)
		}
	}
}
