package server

import (
	"net/http"
	"strconv"

	"github.com/ximilala/viking-go/internal/storage"
)

// --- Debug handlers ---

func (s *Server) handleDebugHealth(w http.ResponseWriter, r *http.Request) {
	isHealthy := s.store.CollectionExists()
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{"healthy": isHealthy},
	})
}

func (s *Server) handleDebugVectorScroll(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 100)
	if limit > 1000 {
		limit = 1000
	}
	offset := queryInt(r, "offset", 0)
	uri := r.URL.Query().Get("uri")

	var filter storage.FilterExpr
	if uri != "" {
		filter = storage.Eq{Field: "uri", Value: uri}
	}

	records, err := s.store.Query(filter, limit, offset, "updated_at", true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type slimRecord struct {
		ID          string `json:"id"`
		URI         string `json:"uri"`
		ContextType string `json:"context_type"`
		Category    string `json:"category"`
		Abstract    string `json:"abstract"`
		UpdatedAt   string `json:"updated_at"`
	}

	items := make([]slimRecord, 0, len(records))
	for _, c := range records {
		items = append(items, slimRecord{
			ID:          c.ID,
			URI:         c.URI,
			ContextType: c.ContextType,
			Category:    c.Category,
			Abstract:    c.Abstract,
			UpdatedAt:   c.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	nextOffset := ""
	if len(records) == limit {
		nextOffset = strconv.Itoa(offset + limit)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"records":     items,
			"next_offset": nextOffset,
		},
	})
}

func (s *Server) handleDebugVectorCount(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")

	var filter storage.FilterExpr
	if uri != "" {
		filter = storage.Eq{Field: "uri", Value: uri}
	}

	count, err := s.store.Count(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{"count": count},
	})
}
