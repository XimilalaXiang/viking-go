package memory

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

// ToolStats tracks usage statistics for a tool memory.
type ToolStats struct {
	CallCount    int     `json:"call_count"`
	SuccessCount int     `json:"success_count"`
	ErrorCount   int     `json:"error_count"`
	TotalLatency float64 `json:"total_latency_ms"`
	LastUsed     string  `json:"last_used"`
}

// SkillGuideline stores a skill's usage guideline with metadata.
type SkillGuideline struct {
	Guideline   string `json:"guideline"`
	Source      string `json:"source"`
	LastUpdated string `json:"last_updated"`
}

// ToolSkillMemory extends CandidateMemory with tool/skill specific fields.
type ToolSkillMemory struct {
	CandidateMemory
	ToolName   string          `json:"tool_name,omitempty"`
	ToolID     string          `json:"tool_id,omitempty"`
	SkillURI   string          `json:"skill_uri,omitempty"`
	Stats      *ToolStats      `json:"stats,omitempty"`
	Guidelines []SkillGuideline `json:"guidelines,omitempty"`
}

// MergeToolStats merges new stats into existing stats, accumulating counts.
func MergeToolStats(existing, new *ToolStats) *ToolStats {
	if existing == nil {
		return new
	}
	if new == nil {
		return existing
	}
	return &ToolStats{
		CallCount:    existing.CallCount + new.CallCount,
		SuccessCount: existing.SuccessCount + new.SuccessCount,
		ErrorCount:   existing.ErrorCount + new.ErrorCount,
		TotalLatency: existing.TotalLatency + new.TotalLatency,
		LastUsed:     new.LastUsed,
	}
}

// MergeGuidelines appends new guidelines, deduplicating by guideline text.
func MergeGuidelines(existing, new []SkillGuideline) []SkillGuideline {
	seen := make(map[string]bool)
	for _, g := range existing {
		seen[g.Guideline] = true
	}
	merged := make([]SkillGuideline, len(existing))
	copy(merged, existing)
	for _, g := range new {
		if !seen[g.Guideline] {
			merged = append(merged, g)
			seen[g.Guideline] = true
		}
	}
	return merged
}

// CreateOrMergeToolMemory creates a new tool memory or merges with existing.
func CreateOrMergeToolMemory(
	candidate ToolSkillMemory,
	vfs *vikingfs.VikingFS,
	reqCtx *ctx.RequestContext,
) (*ctx.Context, error) {
	agentSpace := reqCtx.User.AgentSpaceName()
	catDir := categoryDirs[candidate.Category]
	parentURI := fmt.Sprintf("viking://agent/%s/%s", agentSpace, catDir)

	toolID := candidate.ToolID
	if toolID == "" {
		toolID = sanitizeName(candidate.ToolName)
	}
	memURI := parentURI + "/" + toolID + ".md"

	existing, err := vfs.ReadFile(memURI, reqCtx)
	if err == nil && strings.TrimSpace(existing) != "" {
		merged, mergeErr := mergeToolSkillContent(existing, candidate)
		if mergeErr != nil {
			log.Printf("Warning: merge tool memory failed: %v", mergeErr)
		} else {
			if writeErr := vfs.WriteString(memURI, merged, reqCtx); writeErr != nil {
				return nil, fmt.Errorf("write merged tool memory: %w", writeErr)
			}

			memory := ctx.NewContext(memURI,
				ctx.WithParentURI(parentURI),
				ctx.WithIsLeaf(true),
				ctx.WithAbstract(candidate.Abstract),
				ctx.WithContextType(string(ctx.TypeMemory)),
				ctx.WithCategory(string(candidate.Category)),
				ctx.WithAccountID(reqCtx.AccountID),
				ctx.WithOwnerSpace(agentSpace),
			)
			memory.VectorizeText = merged
			return memory, nil
		}
	}

	content := buildToolSkillContent(candidate)
	if writeErr := vfs.WriteString(memURI, content, reqCtx); writeErr != nil {
		return nil, fmt.Errorf("write tool memory: %w", writeErr)
	}

	memory := ctx.NewContext(memURI,
		ctx.WithParentURI(parentURI),
		ctx.WithIsLeaf(true),
		ctx.WithAbstract(candidate.Abstract),
		ctx.WithContextType(string(ctx.TypeMemory)),
		ctx.WithCategory(string(candidate.Category)),
		ctx.WithAccountID(reqCtx.AccountID),
		ctx.WithOwnerSpace(agentSpace),
	)
	memory.VectorizeText = content
	return memory, nil
}

func buildToolSkillContent(m ToolSkillMemory) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", m.Abstract))
	b.WriteString(m.Content)

	if m.Stats != nil {
		b.WriteString("\n\n## Statistics\n\n")
		statsJSON, _ := json.MarshalIndent(m.Stats, "", "  ")
		b.WriteString("```json\n")
		b.Write(statsJSON)
		b.WriteString("\n```\n")
	}

	if len(m.Guidelines) > 0 {
		b.WriteString("\n\n## Guidelines\n\n")
		for _, g := range m.Guidelines {
			b.WriteString(fmt.Sprintf("- %s (source: %s, updated: %s)\n", g.Guideline, g.Source, g.LastUpdated))
		}
	}

	return b.String()
}

func mergeToolSkillContent(existing string, candidate ToolSkillMemory) (string, error) {
	var existingStats *ToolStats
	if idx := strings.Index(existing, "```json\n"); idx >= 0 {
		end := strings.Index(existing[idx+8:], "\n```")
		if end > 0 {
			statsJSON := existing[idx+8 : idx+8+end]
			var stats ToolStats
			if json.Unmarshal([]byte(statsJSON), &stats) == nil {
				existingStats = &stats
			}
		}
	}

	merged := ToolSkillMemory{
		CandidateMemory: candidate.CandidateMemory,
		ToolName:        candidate.ToolName,
		ToolID:          candidate.ToolID,
		SkillURI:        candidate.SkillURI,
		Stats:           MergeToolStats(existingStats, candidate.Stats),
		Guidelines:      candidate.Guidelines,
	}

	return buildToolSkillContent(merged), nil
}

func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
	return name
}

// nowISO returns current time in ISO 8601 format.
func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}
