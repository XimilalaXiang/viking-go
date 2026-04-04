package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
)

// Store is the SQLite-backed storage for contexts and vectors.
type Store struct {
	db        *sql.DB
	mu        sync.RWMutex
	dimension int
	hasVecTbl bool
}

// NewStore opens or creates a SQLite database at the given path.
// Use ":memory:" for an in-memory database.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(1)

	if _, err := db.Exec(coreSchemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize schema: %w", err)
	}

	// FTS5 is optional — not all SQLite builds include it.
	hasFTS := false
	if _, err := db.Exec(fts5SchemaSQL); err == nil {
		hasFTS = true
	}
	_ = hasFTS

	return &Store{db: db}, nil
}

// InitVectorTable creates the sqlite-vec virtual table for the given dimension.
// Must be called once when the embedding dimension is known.
func (s *Store) InitVectorTable(dimension int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasVecTbl {
		return nil
	}
	if _, err := s.db.Exec(vectorTableSQL(dimension)); err != nil {
		return fmt.Errorf("create vector table: %w", err)
	}
	s.dimension = dimension
	s.hasVecTbl = true
	return nil
}

// Upsert inserts or updates a context record and its vector.
func (s *Store) Upsert(c *ctx.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	metaJSON, _ := json.Marshal(c.Meta)
	relJSON, _ := json.Marshal(c.RelatedURI)

	var level *int
	if c.Level != nil {
		level = c.Level
	}

	_, err := s.db.Exec(`
		INSERT INTO contexts (id, uri, parent_uri, is_leaf, abstract, context_type, category,
			created_at, updated_at, active_count, level, session_id, account_id, owner_space,
			meta_json, related_uri_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			uri=excluded.uri, parent_uri=excluded.parent_uri, is_leaf=excluded.is_leaf,
			abstract=excluded.abstract, context_type=excluded.context_type,
			category=excluded.category, updated_at=excluded.updated_at,
			active_count=excluded.active_count, level=excluded.level,
			session_id=excluded.session_id, account_id=excluded.account_id,
			owner_space=excluded.owner_space, meta_json=excluded.meta_json,
			related_uri_json=excluded.related_uri_json`,
		c.ID, c.URI, c.ParentURI, c.IsLeaf, c.Abstract, c.ContextType, c.Category,
		c.CreatedAt.Format(time.RFC3339Nano), c.UpdatedAt.Format(time.RFC3339Nano),
		c.ActiveCount, level, c.SessionID, c.AccountID, c.OwnerSpace,
		string(metaJSON), string(relJSON),
	)
	if err != nil {
		return fmt.Errorf("upsert context: %w", err)
	}

	if s.hasVecTbl && len(c.Vector) > 0 {
		if err := s.upsertVector(c.ID, c.Vector); err != nil {
			return fmt.Errorf("upsert vector: %w", err)
		}
	}

	return nil
}

func (s *Store) upsertVector(id string, vector []float32) error {
	blob := float32SliceToBytes(vector)
	_, err := s.db.Exec(`
		INSERT INTO context_vectors (id, embedding)
		VALUES (?, ?)
		ON CONFLICT(id) DO UPDATE SET embedding=excluded.embedding`,
		id, blob,
	)
	return err
}

// Get retrieves contexts by IDs.
func (s *Store) Get(ids []string) ([]*ctx.Context, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := "SELECT * FROM contexts WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	return s.queryContexts(query, args...)
}

// Delete removes contexts by IDs.
func (s *Store) Delete(ids []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(ids) == 0 {
		return 0, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	result, err := s.db.Exec(
		"DELETE FROM contexts WHERE id IN ("+strings.Join(placeholders, ",")+")", args...)
	if err != nil {
		return 0, err
	}

	if s.hasVecTbl {
		s.db.Exec("DELETE FROM context_vectors WHERE id IN ("+strings.Join(placeholders, ",")+
			")", args...)
	}

	n, _ := result.RowsAffected()
	return int(n), nil
}

// DeleteByFilter deletes contexts matching the filter.
func (s *Store) DeleteByFilter(filter FilterExpr) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	clause, args := filter.ToSQL()
	result, err := s.db.Exec("DELETE FROM contexts WHERE "+clause, args...)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// SearchResult holds a context with its similarity score.
type SearchResult struct {
	Context *ctx.Context
	Score   float64
}

// VectorSearch performs a vector similarity search with optional filters.
func (s *Store) VectorSearch(queryVec []float32, filter FilterExpr, limit int, outputFields []string) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.hasVecTbl || len(queryVec) == 0 {
		return nil, nil
	}

	blob := float32SliceToBytes(queryVec)

	// Build the query: join vector search with contexts table and apply filter
	baseQuery := `
		SELECT c.*, v.distance
		FROM context_vectors v
		JOIN contexts c ON c.id = v.id
	`
	var whereClause string
	var args []any
	args = append(args, blob)

	if filter != nil {
		fc, fa := filter.ToSQL()
		if fc != "" {
			whereClause = " WHERE " + fc
			args = append(args, fa...)
		}
	}

	// sqlite-vec uses KNN query syntax
	query := fmt.Sprintf(`
		SELECT c.*, v.distance
		FROM (
			SELECT id, distance FROM context_vectors
			WHERE embedding MATCH ? AND k = ?
		) v
		JOIN contexts c ON c.id = v.id
		%s
		ORDER BY v.distance ASC
		LIMIT ?`,
		whereClause,
	)

	// k should be larger than limit to account for post-filtering
	k := limit * 3
	if k < 50 {
		k = 50
	}
	// Build final args: blob, k, [filter args...], limit
	finalArgs := []any{blob, k}
	if filter != nil {
		_, fa := filter.ToSQL()
		finalArgs = append(finalArgs, fa...)
	}
	finalArgs = append(finalArgs, limit)

	_ = baseQuery // unused, we use the KNN query above

	rows, err := s.db.Query(query, finalArgs...)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		c, distance, err := scanContextWithDistance(rows)
		if err != nil {
			continue
		}
		// Convert distance to similarity score (cosine distance → similarity)
		score := 1.0 - distance
		if math.IsNaN(score) || math.IsInf(score, 0) {
			score = 0
		}
		results = append(results, SearchResult{Context: c, Score: score})
	}

	return results, nil
}

// Query retrieves contexts matching a filter with optional ordering.
func (s *Store) Query(filter FilterExpr, limit, offset int, orderBy string, orderDesc bool) ([]*ctx.Context, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT * FROM contexts"
	var args []any

	if filter != nil {
		clause, fa := filter.ToSQL()
		if clause != "" {
			query += " WHERE " + clause
			args = fa
		}
	}

	if orderBy != "" {
		dir := "ASC"
		if orderDesc {
			dir = "DESC"
		}
		query += fmt.Sprintf(" ORDER BY %s %s", orderBy, dir)
	}

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	if offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", offset)
	}

	return s.queryContexts(query, args...)
}

// Count returns the number of contexts matching a filter.
func (s *Store) Count(filter FilterExpr) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT COUNT(*) FROM contexts"
	var args []any
	if filter != nil {
		clause, fa := filter.ToSQL()
		if clause != "" {
			query += " WHERE " + clause
			args = fa
		}
	}

	var count int
	err := s.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// CollectionExists checks if the contexts table has any data.
func (s *Store) CollectionExists() bool {
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='contexts'").Scan(&count)
	return count > 0
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Stats returns database statistics.
func (s *Store) Stats() (map[string]any, error) {
	total, err := s.Count(nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"total_records": total,
		"backend":       "sqlite",
		"has_vectors":   s.hasVecTbl,
		"dimension":     s.dimension,
	}, nil
}

// DB returns the underlying sql.DB for advanced operations.
// ListByFilter returns contexts matching a filter, up to limit.
func (s *Store) ListByFilter(filter FilterExpr, limit int) ([]*ctx.Context, error) {
	return s.Query(filter, limit, 0, "updated_at", true)
}

// GetByURI returns a single context by URI, or nil if not found.
func (s *Store) GetByURI(uri string) (*ctx.Context, error) {
	results, err := s.Query(Eq{Field: "uri", Value: uri}, 1, 0, "", false)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

func (s *Store) DB() *sql.DB {
	return s.db
}

// queryContexts executes a query and scans results into Context objects.
func (s *Store) queryContexts(query string, args ...any) ([]*ctx.Context, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*ctx.Context
	for rows.Next() {
		c, err := scanContext(rows)
		if err != nil {
			continue
		}
		results = append(results, c)
	}
	return results, nil
}

func scanContext(rows *sql.Rows) (*ctx.Context, error) {
	var (
		c            ctx.Context
		isLeaf       int
		createdAt    string
		updatedAt    string
		level        sql.NullInt64
		sessionID    sql.NullString
		parentURI    sql.NullString
		metaJSON     sql.NullString
		relJSON      sql.NullString
	)

	err := rows.Scan(
		&c.ID, &c.URI, &parentURI, &isLeaf, &c.Abstract, &c.ContextType, &c.Category,
		&createdAt, &updatedAt, &c.ActiveCount, &level, &sessionID,
		&c.AccountID, &c.OwnerSpace, &metaJSON, &relJSON,
	)
	if err != nil {
		return nil, err
	}

	c.IsLeaf = isLeaf != 0
	if parentURI.Valid {
		c.ParentURI = parentURI.String
	}
	if sessionID.Valid {
		c.SessionID = sessionID.String
	}
	if level.Valid {
		l := int(level.Int64)
		c.Level = &l
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

	if metaJSON.Valid && metaJSON.String != "" {
		json.Unmarshal([]byte(metaJSON.String), &c.Meta)
	}
	if c.Meta == nil {
		c.Meta = make(map[string]any)
	}
	if relJSON.Valid && relJSON.String != "" {
		json.Unmarshal([]byte(relJSON.String), &c.RelatedURI)
	}

	return &c, nil
}

func scanContextWithDistance(rows *sql.Rows) (*ctx.Context, float64, error) {
	var (
		c            ctx.Context
		isLeaf       int
		createdAt    string
		updatedAt    string
		level        sql.NullInt64
		sessionID    sql.NullString
		parentURI    sql.NullString
		metaJSON     sql.NullString
		relJSON      sql.NullString
		distance     float64
	)

	err := rows.Scan(
		&c.ID, &c.URI, &parentURI, &isLeaf, &c.Abstract, &c.ContextType, &c.Category,
		&createdAt, &updatedAt, &c.ActiveCount, &level, &sessionID,
		&c.AccountID, &c.OwnerSpace, &metaJSON, &relJSON, &distance,
	)
	if err != nil {
		return nil, 0, err
	}

	c.IsLeaf = isLeaf != 0
	if parentURI.Valid {
		c.ParentURI = parentURI.String
	}
	if sessionID.Valid {
		c.SessionID = sessionID.String
	}
	if level.Valid {
		l := int(level.Int64)
		c.Level = &l
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

	if metaJSON.Valid && metaJSON.String != "" {
		json.Unmarshal([]byte(metaJSON.String), &c.Meta)
	}
	if c.Meta == nil {
		c.Meta = make(map[string]any)
	}
	if relJSON.Valid && relJSON.String != "" {
		json.Unmarshal([]byte(relJSON.String), &c.RelatedURI)
	}

	return &c, distance, nil
}

// float32SliceToBytes converts a float32 slice to raw bytes for sqlite-vec.
func float32SliceToBytes(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		bits := math.Float32bits(f)
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}
	return buf
}
