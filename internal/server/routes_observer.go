package server

import (
	"net/http"
	"runtime"
	"time"
)

// ComponentStatus mirrors the Python ComponentStatus for observer responses.
type ComponentStatus struct {
	Name      string `json:"name"`
	IsHealthy bool   `json:"is_healthy"`
	HasErrors bool   `json:"has_errors"`
	Status    string `json:"status"`
}

// --- Observer handlers ---

func (s *Server) handleObserverQueue(w http.ResponseWriter, r *http.Request) {
	cs := ComponentStatus{
		Name:      "queue",
		IsHealthy: true,
		Status:    "no queue configured",
	}
	if s.embQueue != nil {
		stats := s.embQueue.Stats()
		cs.Status = "running"
		cs.IsHealthy = true
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"result": map[string]any{
				"name":       cs.Name,
				"is_healthy": cs.IsHealthy,
				"has_errors": false,
				"status":     cs.Status,
				"details": map[string]any{
					"pending":   stats.Pending,
					"running":   stats.Running,
					"completed": stats.Completed,
					"failed":    stats.Failed,
				},
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": cs,
	})
}

func (s *Server) handleObserverStorage(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.Stats()
	isHealthy := err == nil
	status := "ok"
	if !isHealthy {
		status = err.Error()
	}

	du, _ := s.vfs.DiskUsage(s.reqCtx(r))

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"name":       "storage",
			"is_healthy": isHealthy,
			"has_errors": !isHealthy,
			"status":     status,
			"details": map[string]any{
				"store_stats":    stats,
				"disk_usage_bytes": du,
			},
		},
	})
}

func (s *Server) handleObserverModels(w http.ResponseWriter, r *http.Request) {
	hasIndexer := s.indexer != nil
	hasRetriever := s.retriever != nil

	status := "no models configured"
	if hasIndexer || hasRetriever {
		status = "ok"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"name":       "models",
			"is_healthy": hasIndexer || hasRetriever,
			"has_errors": false,
			"status":     status,
			"details": map[string]any{
				"embedding_available": hasIndexer,
				"retriever_available": hasRetriever,
			},
		},
	})
}

func (s *Server) handleObserverLock(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"name":       "lock",
			"is_healthy": true,
			"has_errors": false,
			"status":     "ok",
			"details": map[string]any{
				"active_locks":   0,
				"pending_locks":  0,
				"lock_type":      "goroutine",
			},
		},
	})
}

func (s *Server) handleObserverRetrieval(w http.ResponseWriter, r *http.Request) {
	hasRetriever := s.retriever != nil
	status := "ok"
	if !hasRetriever {
		status = "no retriever configured"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"name":       "retrieval",
			"is_healthy": hasRetriever,
			"has_errors": false,
			"status":     status,
			"details": map[string]any{
				"retriever_available": hasRetriever,
			},
		},
	})
}

func (s *Server) handleObserverVikingDB(w http.ResponseWriter, r *http.Request) {
	storeStats, err := s.store.Stats()
	isHealthy := err == nil
	status := "ok"
	if !isHealthy {
		status = err.Error()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"name":       "vikingdb",
			"is_healthy": isHealthy,
			"has_errors": !isHealthy,
			"status":     status,
			"details": map[string]any{
				"backend":     "sqlite",
				"store_stats": storeStats,
			},
		},
	})
}

func (s *Server) handleObserverSystem(w http.ResponseWriter, r *http.Request) {
	rc := s.reqCtx(r)

	queueHealthy := true
	var queueDetails map[string]any
	if s.embQueue != nil {
		stats := s.embQueue.Stats()
		queueDetails = map[string]any{
			"pending": stats.Pending, "running": stats.Running,
			"completed": stats.Completed, "failed": stats.Failed,
		}
	}

	storeStats, storeErr := s.store.Stats()
	storageHealthy := storeErr == nil
	du, _ := s.vfs.DiskUsage(rc)

	modelsHealthy := s.indexer != nil || s.retriever != nil

	allHealthy := queueHealthy && storageHealthy && modelsHealthy

	var errors []string
	if !storageHealthy {
		errors = append(errors, "storage: "+storeErr.Error())
	}
	if !modelsHealthy {
		errors = append(errors, "models: no embedding or retriever configured")
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"is_healthy": allHealthy,
			"errors":     errors,
			"uptime_seconds": int(time.Since(s.startTime).Seconds()),
			"components": map[string]any{
				"queue": map[string]any{
					"name": "queue", "is_healthy": queueHealthy,
					"has_errors": false, "status": "running",
					"details": queueDetails,
				},
				"storage": map[string]any{
					"name": "storage", "is_healthy": storageHealthy,
					"has_errors": !storageHealthy,
					"details": map[string]any{
						"store_stats":      storeStats,
						"disk_usage_bytes": du,
					},
				},
				"models": map[string]any{
					"name": "models", "is_healthy": modelsHealthy,
					"has_errors": false,
					"details": map[string]any{
						"embedding_available": s.indexer != nil,
						"retriever_available": s.retriever != nil,
					},
				},
			},
			"runtime": map[string]any{
				"goroutines":  runtime.NumGoroutine(),
				"heap_alloc":  m.HeapAlloc,
				"sys_memory":  m.Sys,
				"gc_cycles":   m.NumGC,
			},
		},
	})
}
