package storage

import (
	"fmt"

	ctx "github.com/ximilala/viking-go/internal/context"
)

// Backend defines the interface that all storage backends must implement.
// This allows swapping between SQLite (default), HTTP remote, or other vector DBs.
type Backend interface {
	// Upsert inserts or updates context records.
	Upsert(c *ctx.Context) error

	// Get retrieves contexts by IDs.
	Get(ids []string) ([]*ctx.Context, error)

	// Delete removes contexts by IDs, returning how many were deleted.
	Delete(ids []string) (int, error)

	// DeleteByFilter removes contexts matching a filter expression.
	DeleteByFilter(filter FilterExpr) (int, error)

	// Query returns contexts matching filter, with pagination and ordering.
	Query(filter FilterExpr, limit, offset int, orderBy string, desc bool) ([]*ctx.Context, error)

	// Count returns the number of contexts matching a filter.
	Count(filter FilterExpr) (int, error)

	// VectorSearch performs vector similarity search.
	VectorSearch(queryVec []float32, filter FilterExpr, limit int, outputFields []string) ([]SearchResult, error)

	// CollectionExists checks if the underlying storage is initialized.
	CollectionExists() bool

	// Stats returns backend statistics.
	Stats() (map[string]any, error)

	// Close releases resources.
	Close() error

	// Name returns the backend identifier.
	Name() string
}

// BackendConfig holds configuration for creating a storage backend.
type BackendConfig struct {
	Type     string `json:"type" yaml:"type"` // "sqlite", "http", "memory"
	DSN      string `json:"dsn" yaml:"dsn"`
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
}

// BackendFactory creates a Backend from config.
type BackendFactory func(cfg BackendConfig) (Backend, error)

var backendRegistry = map[string]BackendFactory{
	"sqlite": func(cfg BackendConfig) (Backend, error) {
		store, err := NewStore(cfg.DSN)
		if err != nil {
			return nil, err
		}
		return &sqliteBackend{store: store}, nil
	},
	"memory": func(cfg BackendConfig) (Backend, error) {
		store, err := NewStore(":memory:")
		if err != nil {
			return nil, err
		}
		return &sqliteBackend{store: store}, nil
	},
}

// RegisterBackend registers a new backend factory.
func RegisterBackend(name string, factory BackendFactory) {
	backendRegistry[name] = factory
}

// CreateBackend creates a Backend from config.
func CreateBackend(cfg BackendConfig) (Backend, error) {
	factory, ok := backendRegistry[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("unknown storage backend: %q (available: %v)", cfg.Type, availableBackends())
	}
	return factory(cfg)
}

func availableBackends() []string {
	names := make([]string, 0, len(backendRegistry))
	for name := range backendRegistry {
		names = append(names, name)
	}
	return names
}

// sqliteBackend wraps the existing Store to implement Backend interface.
type sqliteBackend struct {
	store *Store
}

func (b *sqliteBackend) Name() string { return "sqlite" }

func (b *sqliteBackend) Upsert(c *ctx.Context) error {
	return b.store.Upsert(c)
}

func (b *sqliteBackend) Get(ids []string) ([]*ctx.Context, error) {
	return b.store.Get(ids)
}

func (b *sqliteBackend) Delete(ids []string) (int, error) {
	return b.store.Delete(ids)
}

func (b *sqliteBackend) DeleteByFilter(filter FilterExpr) (int, error) {
	return b.store.DeleteByFilter(filter)
}

func (b *sqliteBackend) Query(filter FilterExpr, limit, offset int, orderBy string, desc bool) ([]*ctx.Context, error) {
	return b.store.Query(filter, limit, offset, orderBy, desc)
}

func (b *sqliteBackend) Count(filter FilterExpr) (int, error) {
	return b.store.Count(filter)
}

func (b *sqliteBackend) VectorSearch(queryVec []float32, filter FilterExpr, limit int, outputFields []string) ([]SearchResult, error) {
	return b.store.VectorSearch(queryVec, filter, limit, outputFields)
}

func (b *sqliteBackend) CollectionExists() bool {
	return b.store.CollectionExists()
}

func (b *sqliteBackend) Stats() (map[string]any, error) {
	return b.store.Stats()
}

func (b *sqliteBackend) Close() error {
	return b.store.Close()
}

// Store returns the underlying Store for direct access (backward compat).
func (b *sqliteBackend) Store() *Store {
	return b.store
}
