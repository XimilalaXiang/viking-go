package storage

import (
	"testing"
	"time"
)

func TestLockManager_AcquireRelease(t *testing.T) {
	lm := NewLockManager(5 * time.Second)
	defer lm.Stop()

	h := lm.CreateHandle()
	if !lm.AcquirePoint(h, "/data/file1") {
		t.Fatal("failed to acquire point lock")
	}
	if !lm.IsLocked("/data/file1") {
		t.Error("path should be locked")
	}
	if !lm.IsLockedBy("/data/file1", h.ID) {
		t.Error("path should be locked by handle")
	}

	lm.Release(h)
	if lm.IsLocked("/data/file1") {
		t.Error("path should be unlocked after release")
	}
}

func TestLockManager_ConflictDetection(t *testing.T) {
	lm := NewLockManager(5 * time.Second)
	defer lm.Stop()

	h1 := lm.CreateHandle()
	h2 := lm.CreateHandle()

	if !lm.AcquirePoint(h1, "/data/file1") {
		t.Fatal("h1 should acquire")
	}

	if lm.AcquirePoint(h2, "/data/file1") {
		t.Error("h2 should not acquire (conflict)")
	}

	lm.Release(h1)

	if !lm.AcquirePoint(h2, "/data/file1") {
		t.Error("h2 should acquire after h1 release")
	}
}

func TestLockManager_SubtreeConflict(t *testing.T) {
	lm := NewLockManager(5 * time.Second)
	defer lm.Stop()

	h1 := lm.CreateHandle()
	h2 := lm.CreateHandle()

	if !lm.AcquireSubtree(h1, "/data") {
		t.Fatal("h1 should acquire subtree")
	}

	if lm.AcquirePoint(h2, "/data/child") {
		t.Error("h2 should not lock child of locked subtree")
	}

	if lm.AcquireSubtree(h2, "/data/sub") {
		t.Error("h2 should not lock sub-subtree of locked subtree")
	}

	lm.Release(h1)
	if !lm.AcquirePoint(h2, "/data/child") {
		t.Error("h2 should acquire after h1 release")
	}
}

func TestLockManager_SubtreeBatch_OrderedAcquisition(t *testing.T) {
	lm := NewLockManager(5 * time.Second)
	defer lm.Stop()

	h := lm.CreateHandle()
	paths := []string{"/a/b/c", "/a", "/a/b"}

	if !lm.AcquireSubtreeBatch(h, paths) {
		t.Fatal("batch acquire should succeed (same handle)")
	}

	stats := lm.Stats()
	if stats["active_handles"].(int) != 1 {
		t.Errorf("handles = %v", stats["active_handles"])
	}
}

func TestLockManager_SubtreeBatch_Conflict(t *testing.T) {
	lm := NewLockManager(5 * time.Second)
	defer lm.Stop()

	h1 := lm.CreateHandle()
	h2 := lm.CreateHandle()

	lm.AcquireSubtree(h1, "/data/memories")

	// h2 tries to batch-acquire including a conflicting path
	if lm.AcquireSubtreeBatch(h2, []string{"/data/resources", "/data/memories/profile"}) {
		t.Error("batch should fail due to conflict with h1")
	}

	// resources should NOT be locked either (rollback)
	if lm.IsLockedBy("/data/resources", h2.ID) {
		t.Error("resources should be rolled back")
	}
}

func TestLockManager_ReacquireSameHandle(t *testing.T) {
	lm := NewLockManager(5 * time.Second)
	defer lm.Stop()

	h := lm.CreateHandle()
	lm.AcquirePoint(h, "/data/file1")
	if !lm.AcquirePoint(h, "/data/file1") {
		t.Error("reacquire same handle should succeed")
	}
}

func TestLockManager_ExpiredLock(t *testing.T) {
	lm := NewLockManager(50 * time.Millisecond)
	defer lm.Stop()

	h1 := lm.CreateHandle()
	h2 := lm.CreateHandle()

	lm.AcquirePoint(h1, "/data/file1")
	time.Sleep(100 * time.Millisecond)

	if !lm.AcquirePoint(h2, "/data/file1") {
		t.Error("should acquire after expiry")
	}
}

func TestLockManager_RedoLog(t *testing.T) {
	lm := NewLockManager(5 * time.Second)
	defer lm.Stop()

	id := lm.WriteRedo("session_memory", map[string]any{
		"archive_uri": "viking://archives/session123",
	})

	pending := lm.PendingRedos()
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	if pending[0].ID != id {
		t.Errorf("id = %s, want %s", pending[0].ID, id)
	}

	lm.MarkRedoDone(id)
	pending = lm.PendingRedos()
	if len(pending) != 0 {
		t.Errorf("pending after done = %d", len(pending))
	}
}

func TestLockManager_ReleaseSelected(t *testing.T) {
	lm := NewLockManager(5 * time.Second)
	defer lm.Stop()

	h := lm.CreateHandle()
	lm.AcquirePoint(h, "/a")
	lm.AcquirePoint(h, "/b")
	lm.AcquirePoint(h, "/c")

	lm.ReleaseSelected(h, []string{"/b"})

	if lm.IsLocked("/b") {
		t.Error("/b should be released")
	}
	if !lm.IsLocked("/a") {
		t.Error("/a should still be locked")
	}
	if !lm.IsLocked("/c") {
		t.Error("/c should still be locked")
	}
}

func TestLockManager_Scope(t *testing.T) {
	lm := NewLockManager(5 * time.Second)
	defer lm.Stop()

	scope, err := lm.Scope([]string{"/data/a", "/data/b"}, "point")
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	if !lm.IsLocked("/data/a") {
		t.Error("/data/a should be locked")
	}
	if !lm.IsLocked("/data/b") {
		t.Error("/data/b should be locked")
	}

	scope.Release()
	if lm.IsLocked("/data/a") {
		t.Error("/data/a should be released")
	}
}

func TestLockManager_ScopeSubtreeConflict(t *testing.T) {
	lm := NewLockManager(5 * time.Second)
	defer lm.Stop()

	h := lm.CreateHandle()
	lm.AcquireSubtree(h, "/data")

	_, err := lm.Scope([]string{"/data/child"}, "point")
	if err == nil {
		t.Error("scope should fail due to conflict")
	}
}
