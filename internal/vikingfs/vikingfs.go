package vikingfs

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
	vikinguri "github.com/ximilala/viking-go/pkg/uri"
)

// VikingFS is a local-filesystem-backed implementation of the OpenViking
// file system. It maps viking:// URIs to a local directory tree and
// manages L0/L1/L2 content, relations, and access control.
type VikingFS struct {
	rootDir string
	mu      sync.RWMutex
}

// New creates a VikingFS rooted at the given directory.
// The directory is created if it does not exist.
func New(rootDir string) (*VikingFS, error) {
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, fmt.Errorf("create root dir: %w", err)
	}
	return &VikingFS{rootDir: rootDir}, nil
}

// RootDir returns the absolute root directory backing this filesystem.
func (vfs *VikingFS) RootDir() string { return vfs.rootDir }

// RelationEntry represents a single relation record stored in .relations.json.
type RelationEntry struct {
	ID        string   `json:"id"`
	URIs      []string `json:"uris"`
	Reason    string   `json:"reason,omitempty"`
	CreatedAt string   `json:"created_at"`
}

// DirEntry is a simplified directory entry returned by Ls and Tree.
type DirEntry struct {
	Name    string `json:"name"`
	URI     string `json:"uri"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"isDir"`
	ModTime string `json:"modTime"`
}

// TreeEntry extends DirEntry with a relative path for tree walks.
type TreeEntry struct {
	DirEntry
	RelPath  string `json:"rel_path"`
	Abstract string `json:"abstract,omitempty"`
}

const (
	abstractFile  = ".abstract.md"
	overviewFile  = ".overview.md"
	relationsFile = ".relations.json"
	maxFilenameBytes = 255
)

// URIToPath maps a viking:// URI to a local filesystem path under
// the account-isolated directory tree.
func (vfs *VikingFS) URIToPath(uri string, reqCtx *ctx.RequestContext) (string, error) {
	parsed, err := vikinguri.Parse(uri)
	if err != nil {
		return "", err
	}
	accountID := resolveAccountID(reqCtx)
	parts := make([]string, 0, len(parsed.Parts)+2)
	parts = append(parts, vfs.rootDir, accountID)
	for _, p := range parsed.Parts {
		parts = append(parts, shortenComponent(p, maxFilenameBytes))
	}
	return filepath.Join(parts...), nil
}

// PathToURI converts a local filesystem path back to a viking:// URI.
func (vfs *VikingFS) PathToURI(path string, reqCtx *ctx.RequestContext) string {
	accountID := resolveAccountID(reqCtx)
	prefix := filepath.Join(vfs.rootDir, accountID)
	rel, err := filepath.Rel(prefix, path)
	if err != nil || rel == "." {
		return "viking://"
	}
	rel = filepath.ToSlash(rel)
	return "viking://" + rel
}

// Read reads the full content of a file identified by its URI.
func (vfs *VikingFS) Read(uri string, reqCtx *ctx.RequestContext) ([]byte, error) {
	vfs.mu.RLock()
	defer vfs.mu.RUnlock()
	if err := vfs.ensureAccess(uri, reqCtx); err != nil {
		return nil, err
	}
	p, err := vfs.URIToPath(uri, reqCtx)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// Write writes content to a file, creating parent directories as needed.
func (vfs *VikingFS) Write(uri string, data []byte, reqCtx *ctx.RequestContext) error {
	vfs.mu.Lock()
	defer vfs.mu.Unlock()
	if err := vfs.ensureAccess(uri, reqCtx); err != nil {
		return err
	}
	p, err := vfs.URIToPath(uri, reqCtx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return fmt.Errorf("create parent dirs: %w", err)
	}
	return os.WriteFile(p, data, 0644)
}

// WriteString is a convenience wrapper around Write for string content.
func (vfs *VikingFS) WriteString(uri, content string, reqCtx *ctx.RequestContext) error {
	return vfs.Write(uri, []byte(content), reqCtx)
}

// Mkdir creates a directory for the given URI.
func (vfs *VikingFS) Mkdir(uri string, reqCtx *ctx.RequestContext) error {
	vfs.mu.Lock()
	defer vfs.mu.Unlock()
	if err := vfs.ensureAccess(uri, reqCtx); err != nil {
		return err
	}
	p, err := vfs.URIToPath(uri, reqCtx)
	if err != nil {
		return err
	}
	return os.MkdirAll(p, 0755)
}

// Rm removes a file or directory. If recursive is true, directories are
// removed along with all their contents.
func (vfs *VikingFS) Rm(uri string, recursive bool, reqCtx *ctx.RequestContext) error {
	vfs.mu.Lock()
	defer vfs.mu.Unlock()
	if err := vfs.ensureAccess(uri, reqCtx); err != nil {
		return err
	}
	p, err := vfs.URIToPath(uri, reqCtx)
	if err != nil {
		return err
	}
	if recursive {
		return os.RemoveAll(p)
	}
	return os.Remove(p)
}

// Exists checks whether a URI points to an existing file or directory.
func (vfs *VikingFS) Exists(uri string, reqCtx *ctx.RequestContext) bool {
	p, err := vfs.URIToPath(uri, reqCtx)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Stat returns os.FileInfo for a URI.
func (vfs *VikingFS) Stat(uri string, reqCtx *ctx.RequestContext) (os.FileInfo, error) {
	if err := vfs.ensureAccess(uri, reqCtx); err != nil {
		return nil, err
	}
	p, err := vfs.URIToPath(uri, reqCtx)
	if err != nil {
		return nil, err
	}
	return os.Stat(p)
}

// Ls lists the contents of a directory URI.
func (vfs *VikingFS) Ls(uri string, reqCtx *ctx.RequestContext) ([]DirEntry, error) {
	vfs.mu.RLock()
	defer vfs.mu.RUnlock()
	if err := vfs.ensureAccess(uri, reqCtx); err != nil {
		return nil, err
	}
	p, err := vfs.URIToPath(uri, reqCtx)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, fmt.Errorf("ls %s: %w", uri, err)
	}

	result := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		childURI := strings.TrimRight(vikinguri.Normalize(uri), "/") + "/" + name
		result = append(result, DirEntry{
			Name:    name,
			URI:     childURI,
			Size:    info.Size(),
			IsDir:   info.IsDir(),
			ModTime: info.ModTime().Format(time.RFC3339),
		})
	}
	return result, nil
}

// Tree recursively lists all entries under a URI.
func (vfs *VikingFS) Tree(uri string, nodeLimit, levelLimit int, reqCtx *ctx.RequestContext) ([]TreeEntry, error) {
	vfs.mu.RLock()
	defer vfs.mu.RUnlock()
	if err := vfs.ensureAccess(uri, reqCtx); err != nil {
		return nil, err
	}
	p, err := vfs.URIToPath(uri, reqCtx)
	if err != nil {
		return nil, err
	}

	var results []TreeEntry
	baseURI := strings.TrimRight(vikinguri.Normalize(uri), "/")

	var walk func(dir, relPrefix string, depth int)
	walk = func(dir, relPrefix string, depth int) {
		if len(results) >= nodeLimit || depth >= levelLimit {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if len(results) >= nodeLimit {
				return
			}
			name := e.Name()
			if name == "." || name == ".." {
				continue
			}
			relPath := name
			if relPrefix != "" {
				relPath = relPrefix + "/" + name
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			childURI := baseURI + "/" + relPath
			if !vfs.isAccessible(childURI, reqCtx) {
				continue
			}
			entry := TreeEntry{
				DirEntry: DirEntry{
					Name:    name,
					URI:     childURI,
					Size:    info.Size(),
					IsDir:   info.IsDir(),
					ModTime: info.ModTime().Format(time.RFC3339),
				},
				RelPath: relPath,
			}
			if info.IsDir() {
				results = append(results, entry)
				walk(filepath.Join(dir, name), relPath, depth+1)
			} else if !strings.HasPrefix(name, ".") {
				results = append(results, entry)
			}
		}
	}

	walk(p, "", 0)
	return results, nil
}

// Abstract reads the L0 summary (.abstract.md) for a directory URI.
func (vfs *VikingFS) Abstract(uri string, reqCtx *ctx.RequestContext) (string, error) {
	absURI := strings.TrimRight(vikinguri.Normalize(uri), "/") + "/" + abstractFile
	data, err := vfs.Read(absURI, reqCtx)
	if err != nil {
		return "", fmt.Errorf("read abstract for %s: %w", uri, err)
	}
	return string(data), nil
}

// Overview reads the L1 overview (.overview.md) for a directory URI.
func (vfs *VikingFS) Overview(uri string, reqCtx *ctx.RequestContext) (string, error) {
	ovURI := strings.TrimRight(vikinguri.Normalize(uri), "/") + "/" + overviewFile
	data, err := vfs.Read(ovURI, reqCtx)
	if err != nil {
		return "", fmt.Errorf("read overview for %s: %w", uri, err)
	}
	return string(data), nil
}

// WriteContext writes a context node to the filesystem, including its
// abstract (L0), overview (L1), and content (L2) files.
func (vfs *VikingFS) WriteContext(uri string, abstract, overview, content, contentFilename string, reqCtx *ctx.RequestContext) error {
	if err := vfs.Mkdir(uri, reqCtx); err != nil {
		return err
	}
	normalizedURI := strings.TrimRight(vikinguri.Normalize(uri), "/")
	if abstract != "" {
		if err := vfs.WriteString(normalizedURI+"/"+abstractFile, abstract, reqCtx); err != nil {
			return fmt.Errorf("write abstract: %w", err)
		}
	}
	if overview != "" {
		if err := vfs.WriteString(normalizedURI+"/"+overviewFile, overview, reqCtx); err != nil {
			return fmt.Errorf("write overview: %w", err)
		}
	}
	if content != "" {
		if contentFilename == "" {
			contentFilename = "content.md"
		}
		if err := vfs.WriteString(normalizedURI+"/"+contentFilename, content, reqCtx); err != nil {
			return fmt.Errorf("write content: %w", err)
		}
	}
	return nil
}

// ReadFile reads a file and returns its content as a string.
func (vfs *VikingFS) ReadFile(uri string, reqCtx *ctx.RequestContext) (string, error) {
	data, err := vfs.Read(uri, reqCtx)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// AppendFile appends content to an existing file, creating it if needed.
func (vfs *VikingFS) AppendFile(uri, content string, reqCtx *ctx.RequestContext) error {
	existing, err := vfs.ReadFile(uri, reqCtx)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return vfs.WriteString(uri, existing+content, reqCtx)
}

// Mv moves a file or directory from one URI to another.
// For simplicity the Go version uses os.Rename when possible,
// falling back to copy-and-delete for cross-device moves.
func (vfs *VikingFS) Mv(oldURI, newURI string, reqCtx *ctx.RequestContext) error {
	vfs.mu.Lock()
	defer vfs.mu.Unlock()
	if err := vfs.ensureAccess(oldURI, reqCtx); err != nil {
		return err
	}
	if err := vfs.ensureAccess(newURI, reqCtx); err != nil {
		return err
	}

	oldPath, err := vfs.URIToPath(oldURI, reqCtx)
	if err != nil {
		return err
	}
	newPath, err := vfs.URIToPath(newURI, reqCtx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return fmt.Errorf("create target parent: %w", err)
	}
	return os.Rename(oldPath, newPath)
}

// CollectURIs recursively collects all URIs under a given path, used
// for batch vector store updates on rm/mv.
func (vfs *VikingFS) CollectURIs(uri string, recursive bool, reqCtx *ctx.RequestContext) ([]string, error) {
	p, err := vfs.URIToPath(uri, reqCtx)
	if err != nil {
		return nil, err
	}
	var uris []string
	normalizedBase := strings.TrimRight(vikinguri.Normalize(uri), "/")

	err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == p {
			return nil
		}
		if !recursive && filepath.Dir(path) != p {
			return fs.SkipDir
		}
		rel, _ := filepath.Rel(p, path)
		rel = filepath.ToSlash(rel)
		uris = append(uris, normalizedBase+"/"+rel)
		return nil
	})
	return uris, err
}

// Link creates a relation entry in .relations.json of the source directory.
func (vfs *VikingFS) Link(fromURI string, targetURIs []string, reason string, reqCtx *ctx.RequestContext) error {
	vfs.mu.Lock()
	defer vfs.mu.Unlock()
	if err := vfs.ensureAccess(fromURI, reqCtx); err != nil {
		return err
	}

	entries, err := vfs.readRelationTable(fromURI, reqCtx)
	if err != nil {
		return err
	}

	existingIDs := make(map[string]bool, len(entries))
	for _, e := range entries {
		existingIDs[e.ID] = true
	}

	linkID := ""
	for i := 1; i < 10000; i++ {
		candidate := fmt.Sprintf("link_%d", i)
		if !existingIDs[candidate] {
			linkID = candidate
			break
		}
	}

	entries = append(entries, RelationEntry{
		ID:        linkID,
		URIs:      targetURIs,
		Reason:    reason,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	return vfs.writeRelationTable(fromURI, entries, reqCtx)
}

// Unlink removes a target URI from the relation entries of a source directory.
func (vfs *VikingFS) Unlink(fromURI, targetURI string, reqCtx *ctx.RequestContext) error {
	vfs.mu.Lock()
	defer vfs.mu.Unlock()
	if err := vfs.ensureAccess(fromURI, reqCtx); err != nil {
		return err
	}

	entries, err := vfs.readRelationTable(fromURI, reqCtx)
	if err != nil {
		return err
	}

	for i, entry := range entries {
		for j, u := range entry.URIs {
			if u == targetURI {
				entries[i].URIs = append(entry.URIs[:j], entry.URIs[j+1:]...)
				break
			}
		}
	}

	// Remove entries with empty URI lists.
	filtered := entries[:0]
	for _, e := range entries {
		if len(e.URIs) > 0 {
			filtered = append(filtered, e)
		}
	}

	return vfs.writeRelationTable(fromURI, filtered, reqCtx)
}

// Relations returns the flattened list of related URIs for a directory.
func (vfs *VikingFS) Relations(uri string, reqCtx *ctx.RequestContext) ([]RelationEntry, error) {
	vfs.mu.RLock()
	defer vfs.mu.RUnlock()
	return vfs.readRelationTable(uri, reqCtx)
}

// RelatedURIs returns just the URI strings for all relations from a directory.
func (vfs *VikingFS) RelatedURIs(uri string, reqCtx *ctx.RequestContext) ([]string, error) {
	entries, err := vfs.Relations(uri, reqCtx)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, e := range entries {
		for _, u := range e.URIs {
			if vfs.isAccessible(u, reqCtx) {
				result = append(result, u)
			}
		}
	}
	return result, nil
}

// Grep searches for a pattern in all files under a URI, returning matches.
func (vfs *VikingFS) Grep(uri, pattern string, caseInsensitive bool, nodeLimit int, reqCtx *ctx.RequestContext) ([]GrepMatch, error) {
	vfs.mu.RLock()
	defer vfs.mu.RUnlock()

	p, err := vfs.URIToPath(uri, reqCtx)
	if err != nil {
		return nil, err
	}

	var matches []GrepMatch
	baseURI := strings.TrimRight(vikinguri.Normalize(uri), "/")
	target := pattern
	if caseInsensitive {
		target = strings.ToLower(target)
	}

	_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if nodeLimit > 0 && len(matches) >= nodeLimit {
			return filepath.SkipAll
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(p, path)
		rel = filepath.ToSlash(rel)
		entryURI := baseURI + "/" + rel

		lines := strings.Split(string(data), "\n")
		for lineNum, line := range lines {
			haystack := line
			if caseInsensitive {
				haystack = strings.ToLower(line)
			}
			if strings.Contains(haystack, target) {
				matches = append(matches, GrepMatch{
					Line:    lineNum + 1,
					URI:     entryURI,
					Content: line,
				})
				if nodeLimit > 0 && len(matches) >= nodeLimit {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})

	return matches, nil
}

// GrepMatch represents a single grep hit.
type GrepMatch struct {
	Line    int    `json:"line"`
	URI     string `json:"uri"`
	Content string `json:"content"`
}

// Glob returns all URIs matching a glob pattern under a base URI.
func (vfs *VikingFS) Glob(pattern, baseURI string, nodeLimit int, reqCtx *ctx.RequestContext) ([]string, error) {
	treeEntries, err := vfs.Tree(baseURI, 1_000_000, 100, reqCtx)
	if err != nil {
		return nil, err
	}

	var matches []string
	for _, e := range treeEntries {
		matched, _ := filepath.Match(pattern, filepath.Base(e.RelPath))
		if !matched {
			matched, _ = filepath.Match(pattern, e.RelPath)
		}
		if matched {
			matches = append(matches, e.URI)
			if nodeLimit > 0 && len(matches) >= nodeLimit {
				break
			}
		}
	}
	return matches, nil
}

// CreateTempURI generates a new temporary directory URI and creates it.
func (vfs *VikingFS) CreateTempURI(reqCtx *ctx.RequestContext) (string, error) {
	tempURI := vikinguri.CreateTempURI()
	if err := vfs.Mkdir(tempURI, reqCtx); err != nil {
		return "", err
	}
	return tempURI, nil
}

// ReadBatch reads L0 or L1 content from multiple URIs.
func (vfs *VikingFS) ReadBatch(uris []string, level string, reqCtx *ctx.RequestContext) map[string]string {
	results := make(map[string]string, len(uris))
	for _, u := range uris {
		var content string
		var err error
		switch level {
		case "l0":
			content, err = vfs.Abstract(u, reqCtx)
		case "l1":
			content, err = vfs.Overview(u, reqCtx)
		}
		if err == nil {
			results[u] = content
		}
	}
	return results
}

// SpaceInfo lists the top-level scope directories for an account.
func (vfs *VikingFS) SpaceInfo(reqCtx *ctx.RequestContext) ([]DirEntry, error) {
	return vfs.Ls("viking://", reqCtx)
}

// InferContextType derives the context type from a URI.
func InferContextType(uri string) string {
	if strings.Contains(uri, "/memories") {
		return "memory"
	}
	if strings.Contains(uri, "/skills") {
		return "skill"
	}
	if strings.Contains(uri, "/resources") {
		return "resource"
	}
	return ""
}

// DiskUsage returns the total bytes used by an account's data directory.
func (vfs *VikingFS) DiskUsage(reqCtx *ctx.RequestContext) (int64, error) {
	accountDir := filepath.Join(vfs.rootDir, resolveAccountID(reqCtx))
	var total int64
	err := filepath.WalkDir(accountDir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

// RecentlyModified returns URIs sorted by modification time (newest first).
func (vfs *VikingFS) RecentlyModified(uri string, limit int, reqCtx *ctx.RequestContext) ([]TreeEntry, error) {
	all, err := vfs.Tree(uri, 10000, 10, reqCtx)
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].ModTime > all[j].ModTime
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// --- internal helpers ---

func resolveAccountID(reqCtx *ctx.RequestContext) string {
	if reqCtx != nil && reqCtx.AccountID != "" {
		return reqCtx.AccountID
	}
	return "default"
}

func (vfs *VikingFS) ensureAccess(uri string, reqCtx *ctx.RequestContext) error {
	if !vfs.isAccessible(uri, reqCtx) {
		return fmt.Errorf("access denied for %s", uri)
	}
	return nil
}

func (vfs *VikingFS) isAccessible(uri string, reqCtx *ctx.RequestContext) bool {
	if reqCtx == nil || reqCtx.Role == ctx.RoleRoot {
		return true
	}
	parsed, err := vikinguri.Parse(uri)
	if err != nil || len(parsed.Parts) == 0 {
		return true
	}

	scope := parsed.Parts[0]
	switch scope {
	case "resources", "temp":
		return true
	case "_system":
		return false
	}

	space := parsed.ExtractSpace()
	if space == "" {
		return true
	}

	if reqCtx.User == nil {
		return false
	}
	switch scope {
	case "user", "session":
		return space == reqCtx.User.UserSpaceName()
	case "agent":
		return space == reqCtx.User.AgentSpaceName()
	}
	return true
}

func (vfs *VikingFS) readRelationTable(dirURI string, reqCtx *ctx.RequestContext) ([]RelationEntry, error) {
	relURI := strings.TrimRight(vikinguri.Normalize(dirURI), "/") + "/" + relationsFile
	p, err := vfs.URIToPath(relURI, reqCtx)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil
	}

	// Try flat list format first.
	var entries []RelationEntry
	if err := json.Unmarshal(data, &entries); err == nil {
		return entries, nil
	}

	// Fall back to nested format for compatibility.
	var nested map[string]map[string][]RelationEntry
	if err := json.Unmarshal(data, &nested); err != nil {
		return nil, nil
	}
	var flat []RelationEntry
	for _, userMap := range nested {
		for _, list := range userMap {
			flat = append(flat, list...)
		}
	}
	return flat, nil
}

func (vfs *VikingFS) writeRelationTable(dirURI string, entries []RelationEntry, reqCtx *ctx.RequestContext) error {
	relURI := strings.TrimRight(vikinguri.Normalize(dirURI), "/") + "/" + relationsFile
	p, err := vfs.URIToPath(relURI, reqCtx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

func shortenComponent(component string, maxBytes int) string {
	if len([]byte(component)) <= maxBytes {
		return component
	}
	hash := sha256.Sum256([]byte(component))
	suffix := fmt.Sprintf("_%x", hash[:4])
	prefix := component
	for len([]byte(prefix))+len([]byte(suffix)) > maxBytes && len(prefix) > 0 {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix + suffix
}
