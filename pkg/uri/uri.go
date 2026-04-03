package uri

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ValidScopes are the allowed top-level scopes in viking:// URIs.
var ValidScopes = map[string]bool{
	"user":      true,
	"agent":     true,
	"resources": true,
	"session":   true,
	"temp":      true,
}

// userStructureDirs are known structural directories under user scope.
var userStructureDirs = map[string]bool{"memories": true}

// agentStructureDirs are known structural directories under agent scope.
var agentStructureDirs = map[string]bool{
	"memories":     true,
	"skills":       true,
	"instructions": true,
	"workspaces":   true,
}

// VikingURI represents a parsed viking:// URI.
type VikingURI struct {
	Raw    string
	Scope  string   // first segment: user, agent, resources, session, temp
	Parts  []string // all segments after viking://
	Parent *VikingURI
}

// Parse parses a viking:// URI string.
func Parse(raw string) (*VikingURI, error) {
	normalized := Normalize(raw)
	if !strings.HasPrefix(normalized, "viking://") {
		return nil, fmt.Errorf("invalid viking URI: %s", raw)
	}

	remainder := normalized[len("viking://"):]
	remainder = strings.Trim(remainder, "/")
	var parts []string
	if remainder != "" {
		parts = strings.Split(remainder, "/")
	}

	for _, p := range parts {
		if p == "." || p == ".." {
			return nil, fmt.Errorf("unsafe URI traversal segment '%s' in %s", p, normalized)
		}
		if strings.Contains(p, "\\") {
			return nil, fmt.Errorf("unsafe URI path separator in %s", normalized)
		}
	}

	vu := &VikingURI{
		Raw:   normalized,
		Parts: parts,
	}
	if len(parts) > 0 {
		vu.Scope = parts[0]
	}

	if len(parts) > 1 {
		parentParts := parts[:len(parts)-1]
		vu.Parent = &VikingURI{
			Raw:   "viking://" + strings.Join(parentParts, "/"),
			Parts: parentParts,
			Scope: parts[0],
		}
	}

	return vu, nil
}

// Normalize converts short-format URIs to canonical viking:// form.
func Normalize(uri string) string {
	if strings.HasPrefix(uri, "viking://") {
		return uri
	}
	uri = strings.TrimPrefix(uri, "viking:/")
	uri = strings.TrimPrefix(uri, "/")
	if uri == "" {
		return "viking://"
	}
	return "viking://" + uri
}

// URI returns the full URI string.
func (v *VikingURI) URI() string {
	return v.Raw
}

// ExtractSpace returns the space segment from the URI if present.
func (v *VikingURI) ExtractSpace() string {
	if len(v.Parts) < 2 {
		return ""
	}
	second := v.Parts[1]

	// Metadata files at scope root don't have a space segment.
	if len(v.Parts) == 2 && (second == ".abstract.md" || second == ".overview.md") {
		return ""
	}

	switch v.Scope {
	case "user":
		if !userStructureDirs[second] {
			return second
		}
	case "agent":
		if !agentStructureDirs[second] {
			return second
		}
	case "session":
		return second
	}
	return ""
}

// InferContextType derives the context type from the URI path.
func (v *VikingURI) InferContextType() string {
	for _, p := range v.Parts {
		switch {
		case p == "skills" || strings.Contains(p, "skills"):
			return "skill"
		case p == "memories" || strings.Contains(p, "memories"):
			return "memory"
		case p == "resources" || strings.Contains(p, "resources"):
			return "resource"
		}
	}
	return ""
}

// InferCategory derives the category from the URI path.
func (v *VikingURI) InferCategory() string {
	for _, p := range v.Parts {
		switch p {
		case "patterns":
			return "patterns"
		case "cases":
			return "cases"
		case "profile":
			return "profile"
		case "preferences":
			return "preferences"
		case "entities":
			return "entities"
		case "events":
			return "events"
		case "tools":
			return "tools"
		case "skills":
			return "skills"
		}
	}
	return ""
}

// CreateTempURI generates a new temporary URI.
func CreateTempURI() string {
	return "viking://temp/" + uuid.New().String()
}
