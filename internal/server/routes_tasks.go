package server

import (
	"net/http"
)

// --- Tasks handlers ---

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("task_id")
	rc := s.reqCtx(r)

	userID := ""
	if rc.User != nil {
		userID = rc.User.UserID
	}
	task := s.taskTracker.Get(taskID, rc.AccountID, userID)
	if task == nil {
		writeError(w, http.StatusNotFound, "task not found or expired")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": task,
	})
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	rc := s.reqCtx(r)
	taskType := r.URL.Query().Get("task_type")
	status := r.URL.Query().Get("status")
	resourceID := r.URL.Query().Get("resource_id")
	limit := queryInt(r, "limit", 50)
	if limit > 200 {
		limit = 200
	}

	userID := ""
	if rc.User != nil {
		userID = rc.User.UserID
	}
	tasks := s.taskTracker.List(taskType, status, resourceID, rc.AccountID, userID, limit)
	if tasks == nil {
		tasks = []*Task{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": tasks,
	})
}
