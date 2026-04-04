package server

import (
	"os"
	"path/filepath"
	"testing"

	ctx "github.com/ximilala/viking-go/internal/context"
)

func TestAPIKeyManager_CreateAndAuth(t *testing.T) {
	dir := t.TempDir()
	mgr := NewAPIKeyManager("root-secret-key", dir)
	if err := mgr.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Root key should authenticate as root
	entry := mgr.Authenticate("root-secret-key")
	if entry == nil {
		t.Fatal("root key should authenticate")
	}
	if entry.Role != ctx.RoleRoot {
		t.Errorf("root role = %q", entry.Role)
	}

	// Bad key should not authenticate
	if mgr.Authenticate("wrong-key") != nil {
		t.Error("bad key should not authenticate")
	}

	// Create account and get admin key
	key, err := mgr.CreateAccount("acme", "admin1")
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if key == "" {
		t.Fatal("expected non-empty key")
	}

	// Admin key should authenticate
	entry = mgr.Authenticate(key)
	if entry == nil {
		t.Fatal("admin key should authenticate")
	}
	if entry.AccountID != "acme" {
		t.Errorf("AccountID = %q", entry.AccountID)
	}
	if entry.UserID != "admin1" {
		t.Errorf("UserID = %q", entry.UserID)
	}
	if entry.Role != ctx.RoleAdmin {
		t.Errorf("Role = %q", entry.Role)
	}

	// Duplicate account should fail
	_, err = mgr.CreateAccount("acme", "admin2")
	if err == nil {
		t.Error("duplicate account should fail")
	}
}

func TestAPIKeyManager_RegisterUser(t *testing.T) {
	dir := t.TempDir()
	mgr := NewAPIKeyManager("root", dir)
	mgr.Load()

	_, err := mgr.CreateAccount("org1", "admin")
	if err != nil {
		t.Fatal(err)
	}

	userKey, err := mgr.RegisterUser("org1", "bob", "user")
	if err != nil {
		t.Fatalf("RegisterUser: %v", err)
	}

	entry := mgr.Authenticate(userKey)
	if entry == nil {
		t.Fatal("user key should authenticate")
	}
	if entry.Role != ctx.RoleUser {
		t.Errorf("Role = %q, want user", entry.Role)
	}
	if entry.AccountID != "org1" {
		t.Errorf("AccountID = %q", entry.AccountID)
	}

	// Duplicate user should fail
	_, err = mgr.RegisterUser("org1", "bob", "user")
	if err == nil {
		t.Error("duplicate user should fail")
	}

	// Non-existent account should fail
	_, err = mgr.RegisterUser("nonexistent", "x", "user")
	if err == nil {
		t.Error("non-existent account should fail")
	}
}

func TestAPIKeyManager_SetRole(t *testing.T) {
	dir := t.TempDir()
	mgr := NewAPIKeyManager("root", dir)
	mgr.Load()
	mgr.CreateAccount("org1", "admin")
	userKey, _ := mgr.RegisterUser("org1", "alice", "user")

	entry := mgr.Authenticate(userKey)
	if entry.Role != ctx.RoleUser {
		t.Fatalf("initial role = %q", entry.Role)
	}

	if err := mgr.SetUserRole("org1", "alice", "admin"); err != nil {
		t.Fatal(err)
	}

	entry = mgr.Authenticate(userKey)
	if entry.Role != ctx.RoleAdmin {
		t.Errorf("updated role = %q, want admin", entry.Role)
	}
}

func TestAPIKeyManager_DeleteUser(t *testing.T) {
	dir := t.TempDir()
	mgr := NewAPIKeyManager("root", dir)
	mgr.Load()
	mgr.CreateAccount("org1", "admin")
	userKey, _ := mgr.RegisterUser("org1", "bob", "user")

	if mgr.Authenticate(userKey) == nil {
		t.Fatal("bob should exist")
	}

	if err := mgr.DeleteUser("org1", "bob"); err != nil {
		t.Fatal(err)
	}

	if mgr.Authenticate(userKey) != nil {
		t.Error("deleted user should not authenticate")
	}
}

func TestAPIKeyManager_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Create and persist
	mgr1 := NewAPIKeyManager("root", dir)
	mgr1.Load()
	key, _ := mgr1.CreateAccount("test-acct", "admin")

	// Load in new manager
	mgr2 := NewAPIKeyManager("root", dir)
	if err := mgr2.Load(); err != nil {
		t.Fatalf("Load 2: %v", err)
	}

	entry := mgr2.Authenticate(key)
	if entry == nil {
		t.Fatal("key should survive persistence")
	}
	if entry.AccountID != "test-acct" {
		t.Errorf("AccountID = %q", entry.AccountID)
	}

	// Verify file exists
	keysPath := filepath.Join(dir, "_system", "apikeys.json")
	if !fileExists(keysPath) {
		t.Error("keys file should exist")
	}
}

func TestAPIKeyManager_ListAccounts(t *testing.T) {
	dir := t.TempDir()
	mgr := NewAPIKeyManager("root", dir)
	mgr.Load()
	mgr.CreateAccount("org1", "a")
	mgr.CreateAccount("org2", "b")

	accounts := mgr.ListAccounts()
	if len(accounts) < 3 { // default + org1 + org2
		t.Errorf("expected >= 3 accounts, got %d", len(accounts))
	}
}

func TestAPIKeyManager_ListUsers(t *testing.T) {
	dir := t.TempDir()
	mgr := NewAPIKeyManager("root", dir)
	mgr.Load()
	mgr.CreateAccount("org1", "admin")
	mgr.RegisterUser("org1", "bob", "user")
	mgr.RegisterUser("org1", "alice", "admin")

	users, err := mgr.ListUsers("org1")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 3 { // admin + bob + alice
		t.Errorf("expected 3 users, got %d", len(users))
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
