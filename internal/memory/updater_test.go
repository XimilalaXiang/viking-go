package memory

import (
	"os"
	"testing"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/memory/mergeop"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

func testVFS(t *testing.T) *vikingfs.VikingFS {
	t.Helper()
	dir := t.TempDir()
	vfs, err := vikingfs.New(dir)
	if err != nil {
		t.Fatalf("NewVikingFS: %v", err)
	}
	return vfs
}

func testRegistry() *MemoryTypeRegistry {
	r := NewMemoryTypeRegistry()
	r.Register(&MemoryTypeSchema{
		MemoryType:  "profile",
		Description: "User profile",
		Fields: []MemoryField{
			{Name: "content", FieldType: mergeop.FieldString, MergeOp: mergeop.OpPatch},
			{Name: "name", FieldType: mergeop.FieldString, MergeOp: mergeop.OpImmutable},
			{Name: "visit_count", FieldType: mergeop.FieldInt64, MergeOp: mergeop.OpSum},
		},
		Directory: "viking://user/memories/profile",
		Enabled:   true,
	})
	return r
}

func TestUpdater_WriteOperation(t *testing.T) {
	vfs := testVFS(t)
	registry := testRegistry()
	updater := NewMemoryUpdater(registry, vfs)
	rc := ctx.RootContext()

	vfs.Mkdir("viking://user/memories/profile", rc)

	ops := &MemoryOperations{
		WriteOps: []MemoryOperation{
			{
				Type:       "write",
				URI:        "viking://user/memories/profile/john.md",
				MemoryType: "profile",
				Content:    "John is a software engineer",
				Fields: map[string]any{
					"name":        "John",
					"visit_count": 1,
				},
			},
		},
	}

	result := updater.ApplyOperations(ops, rc)
	if len(result.WrittenURIs) != 1 {
		t.Fatalf("written=%d, want 1", len(result.WrittenURIs))
	}
	if len(result.Errors) > 0 {
		t.Fatalf("errors: %v", result.Errors)
	}

	content, err := vfs.ReadFile("viking://user/memories/profile/john.md", rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if content == "" {
		t.Fatal("content is empty")
	}
	t.Logf("Written content: %s", content)
}

func TestUpdater_EditWithMergeOps(t *testing.T) {
	vfs := testVFS(t)
	registry := testRegistry()
	updater := NewMemoryUpdater(registry, vfs)
	rc := ctx.RootContext()

	vfs.Mkdir("viking://user/memories/profile", rc)

	writeOps := &MemoryOperations{
		WriteOps: []MemoryOperation{
			{
				URI:        "viking://user/memories/profile/jane.md",
				MemoryType: "profile",
				Content:    "Jane is a designer",
				Fields: map[string]any{
					"name":        "Jane",
					"visit_count": 3,
				},
			},
		},
	}
	updater.ApplyOperations(writeOps, rc)

	editOps := &MemoryOperations{
		EditOps: []MemoryOperation{
			{
				URI:        "viking://user/memories/profile/jane.md",
				MemoryType: "profile",
				Fields: map[string]any{
					"name":        "Jane Smith",      // immutable: should keep "Jane"
					"visit_count": int64(2),          // sum: 3 + 2 = 5
					"content":     "Jane is a senior designer", // patch: full replace
				},
			},
		},
	}
	result := updater.ApplyOperations(editOps, rc)
	if len(result.EditedURIs) != 1 {
		t.Fatalf("edited=%d, want 1", len(result.EditedURIs))
	}

	content, err := vfs.ReadFile("viking://user/memories/profile/jane.md", rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	_, meta := deserializeFull(content)
	if meta["name"] != "Jane" {
		t.Errorf("name should be immutable: got %v", meta["name"])
	}

	vc, _ := meta["visit_count"].(float64)
	if int(vc) != 5 {
		t.Errorf("visit_count should be 5 (sum), got %v", meta["visit_count"])
	}
}

func TestUpdater_DeleteOperation(t *testing.T) {
	vfs := testVFS(t)
	registry := testRegistry()
	updater := NewMemoryUpdater(registry, vfs)
	rc := ctx.RootContext()

	vfs.Mkdir("viking://user/memories/profile", rc)
	vfs.WriteString("viking://user/memories/profile/del.md", "to be deleted", rc)

	ops := &MemoryOperations{
		DeleteURIs: []string{"viking://user/memories/profile/del.md"},
	}
	result := updater.ApplyOperations(ops, rc)
	if len(result.DeletedURIs) != 1 {
		t.Fatalf("deleted=%d, want 1", len(result.DeletedURIs))
	}

	if vfs.Exists("viking://user/memories/profile/del.md", rc) {
		t.Error("file should have been deleted")
	}
}

func TestSerializeDeserialize(t *testing.T) {
	meta := map[string]any{
		"name":  "Test",
		"count": 42,
	}
	full := serializeWithMetadata("hello world", meta)
	content, parsedMeta := deserializeFull(full)

	if content != "hello world" {
		t.Errorf("content = %q, want 'hello world'", content)
	}
	if parsedMeta["name"] != "Test" {
		t.Errorf("name = %v", parsedMeta["name"])
	}
}

func TestSchemaLoadFromYAML(t *testing.T) {
	r := NewMemoryTypeRegistry()
	yaml := `
memory_type: test_type
description: A test memory type
fields:
  - name: content
    field_type: string
    merge_op: patch
  - name: priority
    field_type: int64
    merge_op: sum
directory: viking://test/memories
enabled: true
`
	if err := r.LoadFromYAML([]byte(yaml)); err != nil {
		t.Fatalf("LoadFromYAML: %v", err)
	}

	schema := r.Get("test_type")
	if schema == nil {
		t.Fatal("schema not found")
	}
	if len(schema.Fields) != 2 {
		t.Errorf("fields=%d, want 2", len(schema.Fields))
	}
	if schema.Fields[1].MergeOp != mergeop.OpSum {
		t.Errorf("priority merge_op = %v", schema.Fields[1].MergeOp)
	}
}

func TestSchemaLoadFromDir(t *testing.T) {
	dir := t.TempDir()
	yaml1 := `
memory_type: entities
description: Named entities
fields:
  - name: content
    field_type: string
    merge_op: patch
directory: viking://user/memories/entities
enabled: true
`
	os.WriteFile(dir+"/entities.yaml", []byte(yaml1), 0644)

	r := NewMemoryTypeRegistry()
	if err := r.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	if r.Get("entities") == nil {
		t.Error("entities schema not loaded")
	}
}
