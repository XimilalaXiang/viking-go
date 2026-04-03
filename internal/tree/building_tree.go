package tree

import (
	ctx "github.com/ximilala/viking-go/internal/context"
)

// BuildingTree is an in-memory container for a context hierarchy.
// It maintains parent-child relationships and supports tree traversal.
type BuildingTree struct {
	SourcePath   string
	SourceFormat string

	contexts     []*ctx.Context
	uriMap       map[string]*ctx.Context
	rootURI      string
	candidateURI string
}

// New creates an empty BuildingTree.
func New(sourcePath, sourceFormat string) *BuildingTree {
	return &BuildingTree{
		SourcePath:   sourcePath,
		SourceFormat: sourceFormat,
		uriMap:       make(map[string]*ctx.Context),
	}
}

// Add adds a context to the tree.
func (t *BuildingTree) Add(c *ctx.Context) {
	t.contexts = append(t.contexts, c)
	t.uriMap[c.URI] = c
}

// SetRoot sets the root URI for the tree.
func (t *BuildingTree) SetRoot(uri string) {
	t.rootURI = uri
}

// SetCandidate sets the candidate URI (e.g., newly created).
func (t *BuildingTree) SetCandidate(uri string) {
	t.candidateURI = uri
}

// Root returns the root context, or nil if not set.
func (t *BuildingTree) Root() *ctx.Context {
	if t.rootURI == "" {
		return nil
	}
	return t.uriMap[t.rootURI]
}

// CandidateURI returns the candidate URI.
func (t *BuildingTree) CandidateURI() string {
	return t.candidateURI
}

// Contexts returns all contexts in the tree.
func (t *BuildingTree) Contexts() []*ctx.Context {
	return t.contexts
}

// Get returns a context by URI.
func (t *BuildingTree) Get(uri string) *ctx.Context {
	return t.uriMap[uri]
}

// Parent returns the parent context of a given URI.
func (t *BuildingTree) Parent(uri string) *ctx.Context {
	c := t.uriMap[uri]
	if c == nil || c.ParentURI == "" {
		return nil
	}
	return t.uriMap[c.ParentURI]
}

// Children returns all contexts whose parent is the given URI.
func (t *BuildingTree) Children(uri string) []*ctx.Context {
	var children []*ctx.Context
	for _, c := range t.contexts {
		if c.ParentURI == uri {
			children = append(children, c)
		}
	}
	return children
}

// PathToRoot returns contexts from uri up to the root.
func (t *BuildingTree) PathToRoot(uri string) []*ctx.Context {
	var path []*ctx.Context
	current := uri
	for current != "" {
		c := t.uriMap[current]
		if c == nil {
			break
		}
		path = append(path, c)
		current = c.ParentURI
	}
	return path
}

// DirEntry is a node in the directory structure.
type DirEntry struct {
	URI      string     `json:"uri"`
	Title    string     `json:"title"`
	Type     string     `json:"type"`
	Children []DirEntry `json:"children,omitempty"`
}

// ToDirectoryStructure converts the tree to a nested directory representation.
func (t *BuildingTree) ToDirectoryStructure() *DirEntry {
	if t.rootURI == "" {
		return nil
	}
	return t.buildDir(t.rootURI)
}

func (t *BuildingTree) buildDir(uri string) *DirEntry {
	c := t.uriMap[uri]
	if c == nil {
		return nil
	}

	title := "Untitled"
	if c.Meta != nil {
		if st, ok := c.Meta["semantic_title"].(string); ok && st != "" {
			title = st
		} else if st, ok := c.Meta["source_title"].(string); ok && st != "" {
			title = st
		}
	}

	entry := &DirEntry{
		URI:   uri,
		Title: title,
		Type:  c.ContextType,
	}

	for _, child := range t.Children(uri) {
		if childEntry := t.buildDir(child.URI); childEntry != nil {
			entry.Children = append(entry.Children, *childEntry)
		}
	}

	return entry
}

// Len returns the number of contexts in the tree.
func (t *BuildingTree) Len() int {
	return len(t.contexts)
}
