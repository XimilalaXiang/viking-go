package parse

import (
	"testing"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

func newSyncTestVFS(t *testing.T) *vikingfs.VikingFS {
	t.Helper()
	vfs, err := vikingfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return vfs
}

func TestSyncTopDownNewTarget(t *testing.T) {
	vfs := newSyncTestVFS(t)
	rc := &ctx.RequestContext{}

	vfs.Mkdir("viking://tmp/src", rc)
	vfs.Mkdir("viking://tmp/src/docs", rc)
	vfs.WriteString("viking://tmp/src/docs/readme.md", "Hello", rc)

	diff, err := SyncTopDownRecursive(SyncConfig{
		VFS:       vfs,
		RootURI:   "viking://tmp/src/docs",
		TargetURI: "viking://resources/docs",
		ReqCtx:    rc,
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(diff.AddedDirs) != 1 {
		t.Errorf("added dirs = %d", len(diff.AddedDirs))
	}

	content, err := vfs.ReadFile("viking://resources/docs/readme.md", rc)
	if err != nil || content != "Hello" {
		t.Errorf("content = %q, err = %v", content, err)
	}
}

func TestSyncTopDownAddedFile(t *testing.T) {
	vfs := newSyncTestVFS(t)
	rc := &ctx.RequestContext{}

	vfs.Mkdir("viking://resources/docs", rc)
	vfs.WriteString("viking://resources/docs/old.md", "old", rc)

	vfs.Mkdir("viking://tmp/src", rc)
	vfs.WriteString("viking://tmp/src/old.md", "old", rc)
	vfs.WriteString("viking://tmp/src/new.md", "new content", rc)

	diff, err := SyncTopDownRecursive(SyncConfig{
		VFS:       vfs,
		RootURI:   "viking://tmp/src",
		TargetURI: "viking://resources/docs",
		ReqCtx:    rc,
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(diff.AddedFiles) != 1 {
		t.Errorf("added files = %d, want 1", len(diff.AddedFiles))
	}

	if !vfs.Exists("viking://resources/docs/new.md", rc) {
		t.Error("new.md should exist in target")
	}
}

func TestSyncTopDownDeletedFile(t *testing.T) {
	vfs := newSyncTestVFS(t)
	rc := &ctx.RequestContext{}

	vfs.Mkdir("viking://resources/docs", rc)
	vfs.WriteString("viking://resources/docs/keep.md", "keep", rc)
	vfs.WriteString("viking://resources/docs/remove.md", "old", rc)

	vfs.Mkdir("viking://tmp/src", rc)
	vfs.WriteString("viking://tmp/src/keep.md", "keep", rc)

	diff, err := SyncTopDownRecursive(SyncConfig{
		VFS:       vfs,
		RootURI:   "viking://tmp/src",
		TargetURI: "viking://resources/docs",
		ReqCtx:    rc,
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(diff.DeletedFiles) != 1 {
		t.Errorf("deleted files = %d", len(diff.DeletedFiles))
	}
	if vfs.Exists("viking://resources/docs/remove.md", rc) {
		t.Error("remove.md should be deleted")
	}
}

func TestSyncTopDownUpdatedFile(t *testing.T) {
	vfs := newSyncTestVFS(t)
	rc := &ctx.RequestContext{}

	vfs.Mkdir("viking://resources/docs", rc)
	vfs.WriteString("viking://resources/docs/readme.md", "old content", rc)

	vfs.Mkdir("viking://tmp/src", rc)
	vfs.WriteString("viking://tmp/src/readme.md", "new content!!", rc)

	diff, err := SyncTopDownRecursive(SyncConfig{
		VFS:       vfs,
		RootURI:   "viking://tmp/src",
		TargetURI: "viking://resources/docs",
		ReqCtx:    rc,
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(diff.UpdatedFiles) != 1 {
		t.Errorf("updated files = %d", len(diff.UpdatedFiles))
	}

	content, _ := vfs.ReadFile("viking://resources/docs/readme.md", rc)
	if content != "new content!!" {
		t.Errorf("content = %q", content)
	}
}

func TestSyncTopDownNoChanges(t *testing.T) {
	vfs := newSyncTestVFS(t)
	rc := &ctx.RequestContext{}

	vfs.Mkdir("viking://resources/docs", rc)
	vfs.WriteString("viking://resources/docs/same.md", "same", rc)

	vfs.Mkdir("viking://tmp/src", rc)
	vfs.WriteString("viking://tmp/src/same.md", "same", rc)

	diff, err := SyncTopDownRecursive(SyncConfig{
		VFS:       vfs,
		RootURI:   "viking://tmp/src",
		TargetURI: "viking://resources/docs",
		ReqCtx:    rc,
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !diff.IsEmpty() {
		t.Errorf("expected no changes, got %d", diff.TotalChanges())
	}
}

func TestDiffResultHelpers(t *testing.T) {
	d := &DiffResult{}
	if !d.IsEmpty() {
		t.Error("empty diff should be empty")
	}
	d.AddedFiles = append(d.AddedFiles, "a")
	d.DeletedDirs = append(d.DeletedDirs, "b")
	if d.TotalChanges() != 2 {
		t.Errorf("total = %d", d.TotalChanges())
	}
}

func TestParentOf(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"viking://resources/docs/readme.md", "viking://resources/docs"},
		{"viking://resources", "viking:/"},
		{"x", ""},
	}
	for _, tc := range tests {
		got := parentOf(tc.uri)
		if got != tc.want {
			t.Errorf("parentOf(%q) = %q, want %q", tc.uri, got, tc.want)
		}
	}
}
