package fns

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListVaults(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("token") == "" {
			t.Error("expected token header")
		}
		if r.URL.Path != "/api/vault" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]any{
			"code":    1,
			"status":  true,
			"message": "Success",
			"data": []map[string]any{
				{"id": 1, "vault": "test-vault", "noteCount": 100, "fileCount": 10},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := NewClient(srv.URL, "test-token")
	vaults, err := client.ListVaults()
	if err != nil {
		t.Fatalf("ListVaults: %v", err)
	}
	if len(vaults) != 1 {
		t.Fatalf("expected 1 vault, got %d", len(vaults))
	}
	if vaults[0].Name != "test-vault" {
		t.Errorf("expected vault name 'test-vault', got %s", vaults[0].Name)
	}
	if vaults[0].NoteCount != 100 {
		t.Errorf("expected 100 notes, got %d", vaults[0].NoteCount)
	}
}

func TestGetNote(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/note" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		vault := r.URL.Query().Get("vault")
		path := r.URL.Query().Get("path")
		if vault != "test-vault" {
			t.Errorf("expected vault 'test-vault', got %s", vault)
		}
		if path != "folder/test.md" {
			t.Errorf("expected path 'folder/test.md', got %s", path)
		}
		resp := map[string]any{
			"code":    1,
			"status":  true,
			"message": "Success",
			"data": map[string]any{
				"path":        "folder/test.md",
				"pathHash":    "12345",
				"content":     "# Hello\nThis is a test note.",
				"contentHash": "abc123",
				"version":     1,
				"mtime":       1775290818130,
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := NewClient(srv.URL, "test-token")
	note, err := client.GetNote("test-vault", "folder/test.md")
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if note.Path != "folder/test.md" {
		t.Errorf("expected path 'folder/test.md', got %s", note.Path)
	}
	if note.ContentHash != "abc123" {
		t.Errorf("expected contentHash 'abc123', got %s", note.ContentHash)
	}
	if note.Content != "# Hello\nThis is a test note." {
		t.Errorf("unexpected content: %s", note.Content)
	}
}

func TestListNotes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/notes" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]any{
			"code":    1,
			"status":  true,
			"message": "Success",
			"data": map[string]any{
				"list": []map[string]any{
					{"path": "note1.md", "pathHash": "1", "mtime": 1775290818130, "version": 1, "size": 100},
					{"path": "note2.md", "pathHash": "2", "mtime": 1775290818131, "version": 2, "size": 200},
				},
				"pager": map[string]any{"page": 1, "pageSize": 100, "totalRows": 2},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := NewClient(srv.URL, "test-token")
	result, err := client.ListNotes("test-vault", 1, 100)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(result.List) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(result.List))
	}
	if result.Pager.TotalRows != 2 {
		t.Errorf("expected totalRows 2, got %d", result.Pager.TotalRows)
	}
}

func TestAPIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"code":    307,
			"status":  false,
			"message": "Not logged in. Please log in first.",
		}
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := NewClient(srv.URL, "bad-token")
	_, err := client.ListVaults()
	if err == nil {
		t.Fatal("expected error for unauthorized request")
	}
}

func TestHealth(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"code":    1,
			"status":  true,
			"message": "Success",
			"data": map[string]any{
				"status":   "healthy",
				"version":  "2.11.3",
				"database": "connected",
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := NewClient(srv.URL, "test-token")
	if err := client.Health(); err != nil {
		t.Fatalf("Health: %v", err)
	}
}
