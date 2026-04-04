package storage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
)

func TestCreateBackend_SQLite(t *testing.T) {
	b, err := CreateBackend(BackendConfig{Type: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer b.Close()

	if b.Name() != "sqlite" {
		t.Errorf("name = %s", b.Name())
	}
	if !b.CollectionExists() {
		t.Error("collection should exist for in-memory SQLite")
	}
}

func TestCreateBackend_Memory(t *testing.T) {
	b, err := CreateBackend(BackendConfig{Type: "memory"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer b.Close()

	if b.Name() != "sqlite" {
		t.Errorf("name = %s", b.Name())
	}
}

func TestCreateBackend_Unknown(t *testing.T) {
	_, err := CreateBackend(BackendConfig{Type: "nonexistent"})
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestSQLiteBackend_CRUD(t *testing.T) {
	b, _ := CreateBackend(BackendConfig{Type: "memory"})
	defer b.Close()

	c := &ctx.Context{
		ID:          "test-1",
		URI:         "viking://resources/test.md",
		ContextType: "resource",
		Category:    "document",
		Abstract:    "Test document",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := b.Upsert(c); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	results, err := b.Get([]string{"test-1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("get returned %d results", len(results))
	}
	if results[0].URI != "viking://resources/test.md" {
		t.Errorf("uri = %s", results[0].URI)
	}

	count, err := b.Count(nil)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d", count)
	}

	deleted, err := b.Delete([]string{"test-1"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d", deleted)
	}

	count, _ = b.Count(nil)
	if count != 0 {
		t.Errorf("count after delete = %d", count)
	}
}

func TestSQLiteBackend_Stats(t *testing.T) {
	b, _ := CreateBackend(BackendConfig{Type: "memory"})
	defer b.Close()

	stats, err := b.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats == nil {
		t.Error("stats should not be nil")
	}
}

func TestHTTPBackend_CollectionExists(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}
	}))
	defer ts.Close()

	b, err := CreateBackend(BackendConfig{Type: "http", Endpoint: ts.URL})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if !b.CollectionExists() {
		t.Error("should exist (mock health endpoint)")
	}
	if b.Name() != "http" {
		t.Errorf("name = %s", b.Name())
	}
}

func TestHTTPBackend_MissingEndpoint(t *testing.T) {
	_, err := CreateBackend(BackendConfig{Type: "http"})
	if err == nil {
		t.Error("expected error for missing endpoint")
	}
}

func TestRegisterBackend(t *testing.T) {
	RegisterBackend("test_custom", func(cfg BackendConfig) (Backend, error) {
		return &sqliteBackend{store: nil}, nil
	})

	b, err := CreateBackend(BackendConfig{Type: "test_custom"})
	if err != nil {
		t.Fatalf("create custom: %v", err)
	}
	_ = b
}

func TestFilterToMap(t *testing.T) {
	m := filterToMap(Eq{Field: "uri", Value: "test"})
	if m["op"] != "eq" {
		t.Errorf("op = %v", m["op"])
	}
	if m["field"] != "uri" {
		t.Errorf("field = %v", m["field"])
	}

	m2 := filterToMap(And{Filters: []FilterExpr{Eq{Field: "a", Value: "b"}}})
	if m2["op"] != "and" {
		t.Errorf("op = %v", m2["op"])
	}

	m3 := filterToMap(PathScope{Field: "uri", BasePath: "/test", Depth: 1})
	if m3["op"] != "path_scope" {
		t.Errorf("op = %v", m3["op"])
	}
}
