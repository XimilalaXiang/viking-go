package bootstrap

import (
	"fmt"
	"log"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/indexer"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

// DirectoryDef defines a preset directory with its L0/L1 content.
type DirectoryDef struct {
	Path     string
	Abstract string
	Overview string
	Children []DirectoryDef
}

// PresetDirectories defines the standard viking-go directory tree.
var PresetDirectories = map[string]DirectoryDef{
	"session": {
		Abstract: "Session scope. Stores complete context for a single conversation, including messages and summaries.",
		Overview: "Session-level temporary data, archived or cleaned after session ends.",
	},
	"user": {
		Abstract: "User scope. Stores user's long-term memory, persisted across sessions.",
		Overview: "User-level persistent data for profiles and private memories.",
		Children: []DirectoryDef{
			{
				Path:     "memories",
				Abstract: "User's long-term memory storage. Contains preferences, entities, and events.",
				Overview: "Access user's personalized memories: preferences, entities, events.",
				Children: []DirectoryDef{
					{
						Path:     "preferences",
						Abstract: "User preferences organized by topic (communication style, code standards, etc.).",
						Overview: "Access when adjusting output style or following user habits.",
					},
					{
						Path:     "entities",
						Abstract: "Entity memories from user's world: projects, people, concepts.",
						Overview: "Access when referencing user-related projects, people, concepts.",
					},
					{
						Path:     "events",
						Abstract: "Event records: decisions, milestones, historical records.",
						Overview: "Access when reviewing user history or tracking progress.",
					},
				},
			},
		},
	},
	"agent": {
		Abstract: "Agent scope. Stores agent's learning memories, instructions, and skills.",
		Overview: "Agent-level global data: memories, instructions, skills.",
		Children: []DirectoryDef{
			{
				Path:     "memories",
				Abstract: "Agent's long-term memory: cases and patterns.",
				Overview: "Access agent's learning memories: cases and patterns.",
				Children: []DirectoryDef{
					{
						Path:     "cases",
						Abstract: "Agent's case records: specific problems and solutions.",
						Overview: "Reference historical solutions when encountering similar problems.",
					},
					{
						Path:     "patterns",
						Abstract: "Agent's effective patterns: reusable processes and best practices.",
						Overview: "Access patterns for strategy selection or process determination.",
					},
					{
						Path:     "tools",
						Abstract: "Agent's tool usage memories: optimization, statistics, best practices.",
						Overview: "Access tool performance data and usage guidelines.",
					},
					{
						Path:     "skills",
						Abstract: "Agent's skill execution memories: workflow, strategy records.",
						Overview: "Access skill performance data and recommended flows.",
					},
				},
			},
			{
				Path:     "instructions",
				Abstract: "Agent instruction set: behavioral directives, rules, and constraints.",
				Overview: "Access when agent needs to follow specific rules.",
			},
			{
				Path:     "skills",
				Abstract: "Agent's skill registry: callable skill definitions.",
				Overview: "Access when agent needs to execute specific tasks.",
			},
		},
	},
	"resources": {
		Abstract: "Resources scope. Independent knowledge and resource storage.",
		Overview: "Globally shared resources, organized by project/topic.",
	},
}

// DirectoryInitializer creates the preset directory structure for accounts.
type DirectoryInitializer struct {
	vfs     *vikingfs.VikingFS
	indexer *indexer.Indexer
}

// NewDirectoryInitializer creates a new initializer.
func NewDirectoryInitializer(vfs *vikingfs.VikingFS, idx *indexer.Indexer) *DirectoryInitializer {
	return &DirectoryInitializer{vfs: vfs, indexer: idx}
}

// InitializeAccount creates the standard directory tree for an account.
func (di *DirectoryInitializer) InitializeAccount(reqCtx *ctx.RequestContext) (int, error) {
	count := 0
	for scope, def := range PresetDirectories {
		rootURI := "viking://" + scope
		n, err := di.ensureTree(rootURI, "", def, scope, reqCtx)
		if err != nil {
			return count, fmt.Errorf("init %s: %w", scope, err)
		}
		count += n
	}
	return count, nil
}

// InitializeUserSpace creates the user-specific directory tree.
func (di *DirectoryInitializer) InitializeUserSpace(reqCtx *ctx.RequestContext) (int, error) {
	def, ok := PresetDirectories["user"]
	if !ok {
		return 0, nil
	}
	userSpace := reqCtx.User.UserSpaceName()
	rootURI := fmt.Sprintf("viking://user/%s", userSpace)
	return di.ensureTree(rootURI, "viking://user", def, "user", reqCtx)
}

// InitializeAgentSpace creates the agent-specific directory tree.
func (di *DirectoryInitializer) InitializeAgentSpace(reqCtx *ctx.RequestContext) (int, error) {
	def, ok := PresetDirectories["agent"]
	if !ok {
		return 0, nil
	}
	agentSpace := reqCtx.User.AgentSpaceName()
	rootURI := fmt.Sprintf("viking://agent/%s", agentSpace)
	return di.ensureTree(rootURI, "viking://agent", def, "agent", reqCtx)
}

func (di *DirectoryInitializer) ensureTree(
	uri, parentURI string,
	def DirectoryDef,
	scope string,
	reqCtx *ctx.RequestContext,
) (int, error) {
	count := 0

	if !di.vfs.Exists(uri, reqCtx) {
		if err := di.vfs.WriteContext(uri, def.Abstract, def.Overview, "", "", reqCtx); err != nil {
			return 0, fmt.Errorf("create %s: %w", uri, err)
		}
		log.Printf("Created directory: %s", uri)
		count++

		if di.indexer != nil {
			di.indexVectors(uri, parentURI, def, scope, reqCtx)
		}
	}

	for _, child := range def.Children {
		childURI := uri + "/" + child.Path
		n, err := di.ensureTree(childURI, uri, child, scope, reqCtx)
		if err != nil {
			return count, err
		}
		count += n
	}

	return count, nil
}

func (di *DirectoryInitializer) indexVectors(
	uri, parentURI string,
	def DirectoryDef,
	scope string,
	reqCtx *ctx.RequestContext,
) {
	ownerSpace := ownerSpaceForScope(scope, reqCtx)

	for _, levelData := range []struct {
		level int
		text  string
	}{
		{0, def.Abstract},
		{1, def.Overview},
	} {
		if levelData.text == "" {
			continue
		}

		ctxType := inferContextTypeForURI(uri)
		c := ctx.NewContext(uri,
			ctx.WithParentURI(parentURI),
			ctx.WithIsLeaf(false),
			ctx.WithContextType(ctxType),
			ctx.WithAbstract(def.Abstract),
			ctx.WithLevel(levelData.level),
			ctx.WithAccountID(reqCtx.AccountID),
			ctx.WithOwnerSpace(ownerSpace),
		)
		c.VectorizeText = levelData.text

		if err := di.indexer.IndexContext(c); err != nil {
			log.Printf("Warning: failed to index %s L%d: %v", uri, levelData.level, err)
		}
	}
}

func ownerSpaceForScope(scope string, reqCtx *ctx.RequestContext) string {
	switch scope {
	case "user", "session":
		return reqCtx.User.UserSpaceName()
	case "agent":
		return reqCtx.User.AgentSpaceName()
	}
	return ""
}

func inferContextTypeForURI(uri string) string {
	if contains(uri, "/memories") {
		return string(ctx.TypeMemory)
	}
	if contains(uri, "/skills") {
		return string(ctx.TypeSkill)
	}
	return string(ctx.TypeResource)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchSubstring(s, sub)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
