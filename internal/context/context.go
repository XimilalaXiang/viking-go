package context

import (
	"time"

	"github.com/google/uuid"
	vikinguri "github.com/ximilala/viking-go/pkg/uri"
)

// ContextType represents the type of context.
type ContextType string

const (
	TypeSkill    ContextType = "skill"
	TypeMemory   ContextType = "memory"
	TypeResource ContextType = "resource"
)

// ContextLevel represents the hierarchy level (L0/L1/L2) for vector indexing.
type ContextLevel int

const (
	LevelAbstract ContextLevel = 0 // L0: directory abstract
	LevelOverview ContextLevel = 1 // L1: directory overview
	LevelDetail   ContextLevel = 2 // L2: file detail/content
)

// Context is the unified context class for all context types.
type Context struct {
	ID          string            `json:"id"`
	URI         string            `json:"uri"`
	ParentURI   string            `json:"parent_uri,omitempty"`
	TempURI     string            `json:"temp_uri,omitempty"`
	IsLeaf      bool              `json:"is_leaf"`
	Abstract    string            `json:"abstract"`
	ContextType string            `json:"context_type"`
	Category    string            `json:"category,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	ActiveCount int               `json:"active_count"`
	RelatedURI  []string          `json:"related_uri,omitempty"`
	Meta        map[string]any    `json:"meta,omitempty"`
	Level       *int              `json:"level,omitempty"`
	SessionID   string            `json:"session_id,omitempty"`
	AccountID   string            `json:"account_id"`
	OwnerSpace  string            `json:"owner_space,omitempty"`
	Vector      []float32         `json:"vector,omitempty"`
	SparseVec   map[string]float32 `json:"-"`

	// Transient fields not persisted to storage
	VectorizeText string `json:"-"`
}

// NewContext creates a new Context with sensible defaults.
func NewContext(uri string, opts ...ContextOption) *Context {
	now := time.Now().UTC()
	ctx := &Context{
		ID:        uuid.New().String(),
		URI:       uri,
		CreatedAt: now,
		UpdatedAt: now,
		Meta:      make(map[string]any),
	}

	for _, opt := range opts {
		opt(ctx)
	}

	if ctx.ContextType == "" {
		ctx.ContextType = deriveContextType(uri)
	}
	if ctx.Category == "" {
		ctx.Category = deriveCategory(uri)
	}
	if ctx.ParentURI == "" {
		ctx.ParentURI = deriveParentURI(uri)
	}
	if ctx.VectorizeText == "" {
		ctx.VectorizeText = ctx.Abstract
	}

	return ctx
}

// ContextOption is a functional option for creating Context.
type ContextOption func(*Context)

func WithParentURI(parentURI string) ContextOption {
	return func(c *Context) { c.ParentURI = parentURI }
}

func WithAbstract(abstract string) ContextOption {
	return func(c *Context) { c.Abstract = abstract }
}

func WithContextType(ct string) ContextOption {
	return func(c *Context) { c.ContextType = ct }
}

func WithCategory(cat string) ContextOption {
	return func(c *Context) { c.Category = cat }
}

func WithLevel(level int) ContextOption {
	return func(c *Context) { c.Level = &level }
}

func WithAccountID(id string) ContextOption {
	return func(c *Context) { c.AccountID = id }
}

func WithOwnerSpace(space string) ContextOption {
	return func(c *Context) { c.OwnerSpace = space }
}

func WithSessionID(sid string) ContextOption {
	return func(c *Context) { c.SessionID = sid }
}

func WithIsLeaf(isLeaf bool) ContextOption {
	return func(c *Context) { c.IsLeaf = isLeaf }
}

func WithMeta(meta map[string]any) ContextOption {
	return func(c *Context) { c.Meta = meta }
}

// UpdateActivity increments active_count and refreshes updated_at.
func (c *Context) UpdateActivity() {
	c.ActiveCount++
	c.UpdatedAt = time.Now().UTC()
}

// GetContextType returns the context type as ContextType.
func (c *Context) GetContextType() ContextType {
	return ContextType(c.ContextType)
}

// GetLevel returns the level as ContextLevel, defaulting to LevelDetail.
func (c *Context) GetLevel() ContextLevel {
	if c.Level == nil {
		return LevelDetail
	}
	return ContextLevel(*c.Level)
}

func deriveContextType(uri string) string {
	parsed, err := vikinguri.Parse(uri)
	if err != nil {
		return "resource"
	}
	ct := parsed.InferContextType()
	if ct == "" {
		return "resource"
	}
	return ct
}

func deriveCategory(uri string) string {
	parsed, err := vikinguri.Parse(uri)
	if err != nil {
		return ""
	}
	return parsed.InferCategory()
}

func deriveParentURI(uri string) string {
	parsed, err := vikinguri.Parse(uri)
	if err != nil {
		return ""
	}
	if parsed.Parent != nil {
		return parsed.Parent.URI()
	}
	return ""
}
