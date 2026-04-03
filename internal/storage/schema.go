package storage

// SQL schema definitions for the viking-go SQLite backend.

const coreSchemaSQL = `
CREATE TABLE IF NOT EXISTS contexts (
    id          TEXT PRIMARY KEY,
    uri         TEXT NOT NULL,
    parent_uri  TEXT,
    is_leaf     INTEGER NOT NULL DEFAULT 0,
    abstract    TEXT NOT NULL DEFAULT '',
    context_type TEXT NOT NULL DEFAULT 'resource',
    category    TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    active_count INTEGER NOT NULL DEFAULT 0,
    level       INTEGER,
    session_id  TEXT,
    account_id  TEXT NOT NULL DEFAULT 'default',
    owner_space TEXT NOT NULL DEFAULT '',
    meta_json   TEXT,
    related_uri_json TEXT
);

CREATE INDEX IF NOT EXISTS idx_contexts_uri ON contexts(uri);
CREATE INDEX IF NOT EXISTS idx_contexts_parent_uri ON contexts(parent_uri);
CREATE INDEX IF NOT EXISTS idx_contexts_account_id ON contexts(account_id);
CREATE INDEX IF NOT EXISTS idx_contexts_owner_space ON contexts(owner_space);
CREATE INDEX IF NOT EXISTS idx_contexts_context_type ON contexts(context_type);
CREATE INDEX IF NOT EXISTS idx_contexts_level ON contexts(level);
CREATE INDEX IF NOT EXISTS idx_contexts_account_type ON contexts(account_id, context_type);
CREATE INDEX IF NOT EXISTS idx_contexts_account_space ON contexts(account_id, owner_space);
`

const fts5SchemaSQL = `
CREATE VIRTUAL TABLE IF NOT EXISTS contexts_fts USING fts5(
    uri,
    abstract,
    content='contexts',
    content_rowid='rowid',
    tokenize='porter unicode61'
);

CREATE TRIGGER IF NOT EXISTS contexts_ai AFTER INSERT ON contexts BEGIN
    INSERT INTO contexts_fts(rowid, uri, abstract) VALUES (new.rowid, new.uri, new.abstract);
END;
CREATE TRIGGER IF NOT EXISTS contexts_ad AFTER DELETE ON contexts BEGIN
    INSERT INTO contexts_fts(contexts_fts, rowid, uri, abstract) VALUES('delete', old.rowid, old.uri, old.abstract);
END;
CREATE TRIGGER IF NOT EXISTS contexts_au AFTER UPDATE ON contexts BEGIN
    INSERT INTO contexts_fts(contexts_fts, rowid, uri, abstract) VALUES('delete', old.rowid, old.uri, old.abstract);
    INSERT INTO contexts_fts(rowid, uri, abstract) VALUES (new.rowid, new.uri, new.abstract);
END;
`

// vectorTableSQL creates the sqlite-vec virtual table with a given dimension.
func vectorTableSQL(dimension int) string {
	return `CREATE VIRTUAL TABLE IF NOT EXISTS context_vectors USING vec0(
    id TEXT PRIMARY KEY,
    embedding float[` + itoa(dimension) + `]
);`
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}
