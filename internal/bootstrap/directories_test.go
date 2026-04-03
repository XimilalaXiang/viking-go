package bootstrap

import (
	"testing"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

func TestInitializeAccount(t *testing.T) {
	dir := t.TempDir()
	vfs, err := vikingfs.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	di := NewDirectoryInitializer(vfs, nil)
	rc := ctx.RootContext()

	count, err := di.InitializeAccount(rc)
	if err != nil {
		t.Fatalf("InitializeAccount: %v", err)
	}
	if count == 0 {
		t.Error("expected directories to be created")
	}

	// Second call should create nothing (already exists)
	count2, err := di.InitializeAccount(rc)
	if err != nil {
		t.Fatalf("second InitializeAccount: %v", err)
	}
	if count2 != 0 {
		t.Errorf("second call created %d dirs, want 0", count2)
	}
}

func TestInitializeUserSpace(t *testing.T) {
	dir := t.TempDir()
	vfs, err := vikingfs.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	di := NewDirectoryInitializer(vfs, nil)
	rc := ctx.DefaultContext()

	count, err := di.InitializeUserSpace(rc)
	if err != nil {
		t.Fatalf("InitializeUserSpace: %v", err)
	}
	if count == 0 {
		t.Error("expected user directories to be created")
	}

	// Verify user memories directory exists
	userSpace := rc.User.UserSpaceName()
	memURI := "viking://user/" + userSpace + "/memories"
	if !vfs.Exists(memURI, rc) {
		t.Errorf("memories directory not found at %s", memURI)
	}
}

func TestInferContextTypeForURI(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"viking://user/alice/memories/profile.md", "memory"},
		{"viking://agent/alice-agent/skills/code.md", "skill"},
		{"viking://resources/docs/readme.md", "resource"},
	}

	for _, tt := range tests {
		got := inferContextTypeForURI(tt.uri)
		if got != tt.want {
			t.Errorf("inferContextTypeForURI(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}
