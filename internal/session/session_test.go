package session

import (
	"testing"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

func testMgr(t *testing.T) (*Manager, *ctx.RequestContext) {
	t.Helper()
	dir := t.TempDir()
	vfs, err := vikingfs.New(dir)
	if err != nil {
		t.Fatalf("NewVikingFS: %v", err)
	}
	return NewManager(vfs), ctx.RootContext()
}

func TestCreateAndGet(t *testing.T) {
	mgr, rc := testMgr(t)

	id, err := mgr.Create(rc)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == "" {
		t.Fatal("session ID is empty")
	}

	info, err := mgr.Get(id, rc)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if info.ID != id {
		t.Errorf("ID = %q, want %q", info.ID, id)
	}
	if info.Status != "active" {
		t.Errorf("Status = %q, want active", info.Status)
	}
}

func TestAddMessageAndGetContext(t *testing.T) {
	mgr, rc := testMgr(t)

	id, _ := mgr.Create(rc)
	mgr.AddMessage(id, "user", "Hello!", rc)
	mgr.AddMessage(id, "assistant", "Hi there!", rc)

	data, err := mgr.GetContext(id, rc)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if len(data.Messages) != 2 {
		t.Fatalf("Messages count = %d, want 2", len(data.Messages))
	}
	if data.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role = %q", data.Messages[0].Role)
	}
	if data.Info.Title != "Hello!" {
		t.Errorf("Title = %q, want 'Hello!'", data.Info.Title)
	}
}

func TestCommit(t *testing.T) {
	mgr, rc := testMgr(t)

	id, _ := mgr.Create(rc)
	mgr.AddMessage(id, "user", "test message", rc)

	archive, err := mgr.Commit(id, "Test summary", rc)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if archive.MsgCount != 1 {
		t.Errorf("MsgCount = %d, want 1", archive.MsgCount)
	}
	if archive.Summary != "Test summary" {
		t.Errorf("Summary = %q", archive.Summary)
	}

	// Messages should be cleared after commit
	data, _ := mgr.GetContext(id, rc)
	if len(data.Messages) != 0 {
		t.Errorf("Messages after commit = %d, want 0", len(data.Messages))
	}

	// Archive should be readable
	got, err := mgr.GetArchive(id, archive.ID, rc)
	if err != nil {
		t.Fatalf("GetArchive: %v", err)
	}
	if got.Summary != "Test summary" {
		t.Errorf("Archive Summary = %q", got.Summary)
	}
}

func TestDelete(t *testing.T) {
	mgr, rc := testMgr(t)

	id, _ := mgr.Create(rc)
	if err := mgr.Delete(id, rc); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := mgr.Get(id, rc)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestRecentMessages(t *testing.T) {
	mgr, rc := testMgr(t)

	id, _ := mgr.Create(rc)
	for i := 0; i < 10; i++ {
		mgr.AddMessage(id, "user", "msg", rc)
	}

	msgs, err := mgr.RecentMessages(id, 3, rc)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("RecentMessages count = %d, want 3", len(msgs))
	}
}

func TestList(t *testing.T) {
	mgr, rc := testMgr(t)

	mgr.Create(rc)
	mgr.Create(rc)

	list, err := mgr.List(rc)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("List count = %d, want 2", len(list))
	}
}
