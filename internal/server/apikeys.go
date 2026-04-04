package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
)

// APIKeyManager handles multi-tenant API key authentication.
// Keys are stored hashed (HMAC-SHA256) in a JSON file alongside the VikingFS root.
type APIKeyManager struct {
	mu       sync.RWMutex
	rootKey  string
	dataDir  string
	accounts map[string]*accountRecord
	keyIndex map[string]*keyEntry // prefix -> keyEntry for fast lookup
}

type accountRecord struct {
	AccountID string                `json:"account_id"`
	CreatedAt string                `json:"created_at"`
	Users     map[string]*userRecord `json:"users"`
}

type userRecord struct {
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	KeyHash   string `json:"key_hash"`
	KeyPrefix string `json:"key_prefix"`
	CreatedAt string `json:"created_at"`
}

type keyEntry struct {
	AccountID string
	UserID    string
	Role      ctx.Role
	KeyHash   string
}

type apiKeysFile struct {
	Accounts map[string]*accountRecord `json:"accounts"`
}

// NewAPIKeyManager creates a new manager. rootKey is the global admin key.
// dataDir is the directory where the keys file will be stored.
func NewAPIKeyManager(rootKey, dataDir string) *APIKeyManager {
	m := &APIKeyManager{
		rootKey:  rootKey,
		dataDir:  dataDir,
		accounts: make(map[string]*accountRecord),
		keyIndex: make(map[string]*keyEntry),
	}
	return m
}

// Load reads persisted accounts from disk.
func (m *APIKeyManager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.keysPath())
	if err != nil {
		if os.IsNotExist(err) {
			m.accounts["default"] = &accountRecord{
				AccountID: "default",
				CreatedAt: now(),
				Users:     make(map[string]*userRecord),
			}
			return m.saveLocked()
		}
		return fmt.Errorf("read keys file: %w", err)
	}

	var f apiKeysFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse keys file: %w", err)
	}
	m.accounts = f.Accounts
	if m.accounts == nil {
		m.accounts = make(map[string]*accountRecord)
	}

	m.rebuildIndex()
	return nil
}

// Authenticate resolves an API key to an identity. Returns nil if invalid.
func (m *APIKeyManager) Authenticate(rawKey string) *keyEntry {
	if rawKey == "" {
		return nil
	}

	if hmac.Equal([]byte(rawKey), []byte(m.rootKey)) {
		return &keyEntry{
			AccountID: "",
			UserID:    "",
			Role:      ctx.RoleRoot,
		}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	prefix := keyPrefix(rawKey)
	entry, ok := m.keyIndex[prefix]
	if !ok {
		return nil
	}

	if verifyKeyHash(rawKey, entry.KeyHash) {
		return entry
	}
	return nil
}

// CreateAccount creates a new tenant account.
func (m *APIKeyManager) CreateAccount(accountID, adminUserID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.accounts[accountID]; exists {
		return "", fmt.Errorf("account %q already exists", accountID)
	}

	key := generateAPIKey()
	hash := hashKey(key)
	prefix := keyPrefix(key)

	m.accounts[accountID] = &accountRecord{
		AccountID: accountID,
		CreatedAt: now(),
		Users: map[string]*userRecord{
			adminUserID: {
				UserID:    adminUserID,
				Role:      string(ctx.RoleAdmin),
				KeyHash:   hash,
				KeyPrefix: prefix,
				CreatedAt: now(),
			},
		},
	}

	m.keyIndex[prefix] = &keyEntry{
		AccountID: accountID,
		UserID:    adminUserID,
		Role:      ctx.RoleAdmin,
		KeyHash:   hash,
	}

	if err := m.saveLocked(); err != nil {
		return "", err
	}
	return key, nil
}

// RegisterUser creates a user key within an existing account.
func (m *APIKeyManager) RegisterUser(accountID, userID, role string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	acc, exists := m.accounts[accountID]
	if !exists {
		return "", fmt.Errorf("account %q not found", accountID)
	}
	if _, exists := acc.Users[userID]; exists {
		return "", fmt.Errorf("user %q already exists in account %q", userID, accountID)
	}

	r := ctx.RoleUser
	if role == string(ctx.RoleAdmin) {
		r = ctx.RoleAdmin
	}

	key := generateAPIKey()
	hash := hashKey(key)
	prefix := keyPrefix(key)

	acc.Users[userID] = &userRecord{
		UserID:    userID,
		Role:      string(r),
		KeyHash:   hash,
		KeyPrefix: prefix,
		CreatedAt: now(),
	}

	m.keyIndex[prefix] = &keyEntry{
		AccountID: accountID,
		UserID:    userID,
		Role:      r,
		KeyHash:   hash,
	}

	if err := m.saveLocked(); err != nil {
		return "", err
	}
	return key, nil
}

// SetUserRole changes a user's role.
func (m *APIKeyManager) SetUserRole(accountID, userID, role string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	acc, exists := m.accounts[accountID]
	if !exists {
		return fmt.Errorf("account %q not found", accountID)
	}
	user, exists := acc.Users[userID]
	if !exists {
		return fmt.Errorf("user %q not found in account %q", userID, accountID)
	}

	user.Role = role

	m.rebuildIndex()
	return m.saveLocked()
}

// ListAccounts returns all account IDs.
func (m *APIKeyManager) ListAccounts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.accounts))
	for id := range m.accounts {
		ids = append(ids, id)
	}
	return ids
}

// ListUsers returns all user IDs in an account.
func (m *APIKeyManager) ListUsers(accountID string) ([]map[string]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	acc, exists := m.accounts[accountID]
	if !exists {
		return nil, fmt.Errorf("account %q not found", accountID)
	}

	users := make([]map[string]string, 0, len(acc.Users))
	for _, u := range acc.Users {
		users = append(users, map[string]string{
			"user_id":    u.UserID,
			"role":       u.Role,
			"created_at": u.CreatedAt,
		})
	}
	return users, nil
}

// DeleteUser removes a user from an account.
func (m *APIKeyManager) DeleteUser(accountID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	acc, exists := m.accounts[accountID]
	if !exists {
		return fmt.Errorf("account %q not found", accountID)
	}
	user, exists := acc.Users[userID]
	if !exists {
		return fmt.Errorf("user %q not found in account %q", userID, accountID)
	}

	delete(m.keyIndex, user.KeyPrefix)
	delete(acc.Users, userID)

	return m.saveLocked()
}

// DeleteAccount removes an entire account and all its users.
func (m *APIKeyManager) DeleteAccount(accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	acc, exists := m.accounts[accountID]
	if !exists {
		return fmt.Errorf("account %q not found", accountID)
	}

	for _, u := range acc.Users {
		delete(m.keyIndex, u.KeyPrefix)
	}
	delete(m.accounts, accountID)

	return m.saveLocked()
}

// RegenerateKey generates a new API key for an existing user.
func (m *APIKeyManager) RegenerateKey(accountID, userID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	acc, exists := m.accounts[accountID]
	if !exists {
		return "", fmt.Errorf("account %q not found", accountID)
	}
	user, exists := acc.Users[userID]
	if !exists {
		return "", fmt.Errorf("user %q not found in account %q", userID, accountID)
	}

	delete(m.keyIndex, user.KeyPrefix)

	key := generateAPIKey()
	hash := hashKey(key)
	prefix := keyPrefix(key)

	user.KeyHash = hash
	user.KeyPrefix = prefix

	m.keyIndex[prefix] = &keyEntry{
		AccountID: accountID,
		UserID:    userID,
		Role:      ctx.Role(user.Role),
		KeyHash:   hash,
	}

	if err := m.saveLocked(); err != nil {
		return "", err
	}
	return key, nil
}

func (m *APIKeyManager) keysPath() string {
	return filepath.Join(m.dataDir, "_system", "apikeys.json")
}

func (m *APIKeyManager) saveLocked() error {
	dir := filepath.Dir(m.keysPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(apiKeysFile{Accounts: m.accounts}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.keysPath(), data, 0600)
}

func (m *APIKeyManager) rebuildIndex() {
	m.keyIndex = make(map[string]*keyEntry)
	for accountID, acc := range m.accounts {
		for _, u := range acc.Users {
			m.keyIndex[u.KeyPrefix] = &keyEntry{
				AccountID: accountID,
				UserID:    u.UserID,
				Role:      ctx.Role(u.Role),
				KeyHash:   u.KeyHash,
			}
		}
	}
}

func generateAPIKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "ovk_" + hex.EncodeToString(b)
}

func keyPrefix(key string) string {
	if len(key) >= 12 {
		return key[:12]
	}
	return key
}

func hashKey(key string) string {
	h := hmac.New(sha256.New, []byte("viking-go-key-v1"))
	h.Write([]byte(key))
	return hex.EncodeToString(h.Sum(nil))
}

func verifyKeyHash(rawKey, storedHash string) bool {
	computed := hashKey(rawKey)
	return hmac.Equal([]byte(computed), []byte(storedHash))
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// extractAPIKey extracts key from X-Api-Key header or Authorization: Bearer header.
func extractAPIKey(xApiKey, authorization string) string {
	if xApiKey != "" {
		return xApiKey
	}
	if strings.HasPrefix(authorization, "Bearer ") {
		return authorization[7:]
	}
	return ""
}
