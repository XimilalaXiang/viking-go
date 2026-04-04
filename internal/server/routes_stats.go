package server

import (
	"net/http"
	"strings"

	"github.com/ximilala/viking-go/internal/storage"
)

var memoryCategories = []string{
	"profile", "preferences", "entities", "events",
	"cases", "patterns", "tools", "skills",
}

// --- Stats handlers ---

func (s *Server) handleStatsMemories(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	if category != "" {
		valid := false
		for _, c := range memoryCategories {
			if c == category {
				valid = true
				break
			}
		}
		if !valid {
			writeError(w, http.StatusBadRequest,
				"unknown category: "+category+". Valid: "+strings.Join(memoryCategories, ", "))
			return
		}
	}

	memFilter := storage.Eq{Field: "context_type", Value: "memory"}
	var filter storage.FilterExpr
	if category != "" {
		filter = storage.And{Filters: []storage.FilterExpr{
			memFilter,
			storage.Eq{Field: "category", Value: category},
		}}
	} else {
		filter = memFilter
	}

	totalCount, err := s.store.Count(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	byCategory := make(map[string]int)
	for _, cat := range memoryCategories {
		catFilter := storage.And{Filters: []storage.FilterExpr{
			memFilter,
			storage.Eq{Field: "category", Value: cat},
		}}
		cnt, err := s.store.Count(catFilter)
		if err != nil {
			continue
		}
		if cnt > 0 {
			byCategory[cat] = cnt
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"total":       totalCount,
			"by_category": byCategory,
		},
	})
}

func (s *Server) handleStatsSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	rc := s.reqCtx(r)

	info, err := s.sessionMgr.Get(sessionID, rc)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	sessionFilter := storage.And{Filters: []storage.FilterExpr{
		storage.Eq{Field: "session_id", Value: sessionID},
		storage.Eq{Field: "context_type", Value: "memory"},
	}}

	extractedCount, err := s.store.Count(sessionFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"session_id":      sessionID,
			"session_info":    info,
			"extracted_count": extractedCount,
		},
	})
}
