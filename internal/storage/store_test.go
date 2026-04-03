package storage

import (
	"testing"

	ctx "github.com/ximilala/viking-go/internal/context"

	_ "github.com/mattn/go-sqlite3"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNewStoreAndSchema(t *testing.T) {
	s := testStore(t)
	if !s.CollectionExists() {
		t.Error("collection should exist after init")
	}
}

func TestUpsertAndGet(t *testing.T) {
	s := testStore(t)
	c := ctx.NewContext("viking://resources/test",
		ctx.WithAbstract("test abstract"),
		ctx.WithAccountID("acct1"),
	)

	if err := s.Upsert(c); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := s.Get([]string{c.ID})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Get returned %d results, want 1", len(results))
	}
	if results[0].URI != "viking://resources/test" {
		t.Errorf("URI = %q", results[0].URI)
	}
	if results[0].Abstract != "test abstract" {
		t.Errorf("Abstract = %q", results[0].Abstract)
	}
}

func TestDelete(t *testing.T) {
	s := testStore(t)
	c := ctx.NewContext("viking://resources/del")
	s.Upsert(c)

	n, err := s.Delete([]string{c.ID})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n != 1 {
		t.Errorf("Delete returned %d, want 1", n)
	}

	results, _ := s.Get([]string{c.ID})
	if len(results) != 0 {
		t.Errorf("after delete, Get returned %d results", len(results))
	}
}

func TestQueryWithFilter(t *testing.T) {
	s := testStore(t)

	c1 := ctx.NewContext("viking://resources/a", ctx.WithContextType("resource"), ctx.WithAccountID("acct"))
	c2 := ctx.NewContext("viking://user/memories/b", ctx.WithContextType("memory"), ctx.WithAccountID("acct"))
	c3 := ctx.NewContext("viking://resources/c", ctx.WithContextType("resource"), ctx.WithAccountID("other"))
	s.Upsert(c1)
	s.Upsert(c2)
	s.Upsert(c3)

	results, err := s.Query(And{Filters: []FilterExpr{
		Eq{Field: "context_type", Value: "resource"},
		Eq{Field: "account_id", Value: "acct"},
	}}, 10, 0, "", false)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Query returned %d, want 1", len(results))
	}
	if results[0].URI != "viking://resources/a" {
		t.Errorf("Query result URI = %q", results[0].URI)
	}
}

func TestCount(t *testing.T) {
	s := testStore(t)
	s.Upsert(ctx.NewContext("viking://a"))
	s.Upsert(ctx.NewContext("viking://b"))
	s.Upsert(ctx.NewContext("viking://c"))

	n, err := s.Count(nil)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 3 {
		t.Errorf("Count = %d, want 3", n)
	}
}

func TestDeleteByFilter(t *testing.T) {
	s := testStore(t)
	s.Upsert(ctx.NewContext("viking://resources/x", ctx.WithAccountID("del_acct"), ctx.WithContextType("resource")))
	s.Upsert(ctx.NewContext("viking://resources/y", ctx.WithAccountID("del_acct"), ctx.WithContextType("resource")))
	s.Upsert(ctx.NewContext("viking://resources/z", ctx.WithAccountID("keep_acct"), ctx.WithContextType("resource")))

	n, err := s.DeleteByFilter(Eq{Field: "account_id", Value: "del_acct"})
	if err != nil {
		t.Fatalf("DeleteByFilter: %v", err)
	}
	if n != 2 {
		t.Errorf("DeleteByFilter = %d, want 2", n)
	}

	total, _ := s.Count(nil)
	if total != 1 {
		t.Errorf("remaining count = %d, want 1", total)
	}
}

func TestPathScopeFilter(t *testing.T) {
	s := testStore(t)
	s.Upsert(ctx.NewContext("viking://resources/docs/a"))
	s.Upsert(ctx.NewContext("viking://resources/docs/b"))
	s.Upsert(ctx.NewContext("viking://resources/other/c"))

	results, err := s.Query(PathScope{
		Field:    "uri",
		BasePath: "viking://resources/docs",
		Depth:    -1,
	}, 10, 0, "", false)
	if err != nil {
		t.Fatalf("Query with PathScope: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("PathScope results = %d, want 2", len(results))
	}
}

func TestStats(t *testing.T) {
	s := testStore(t)
	s.Upsert(ctx.NewContext("viking://a"))

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats["total_records"] != 1 {
		t.Errorf("Stats total = %v, want 1", stats["total_records"])
	}
	if stats["backend"] != "sqlite" {
		t.Errorf("Stats backend = %v", stats["backend"])
	}
}
