package memory

import (
	"fmt"
	"log"
	"math"
	"regexp"
	"strings"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/embedder"
	"github.com/ximilala/viking-go/internal/llm"
	"github.com/ximilala/viking-go/internal/storage"
)

// DedupDecision represents the outcome of deduplication.
type DedupDecision string

const (
	DecisionSkip   DedupDecision = "skip"
	DecisionCreate DedupDecision = "create"
	DecisionNone   DedupDecision = "none"
)

// MemoryAction is the action to take on an existing memory.
type MemoryAction string

const (
	ActionMerge  MemoryAction = "merge"
	ActionDelete MemoryAction = "delete"
)

// ExistingMemoryAction describes what to do with one existing memory.
type ExistingMemoryAction struct {
	Memory   *ctx.Context
	Decision MemoryAction
	Reason   string
}

// DedupResult is the full result of a deduplication decision.
type DedupResult struct {
	Decision        DedupDecision
	Candidate       CandidateMemory
	SimilarMemories []*ctx.Context
	Actions         []ExistingMemoryAction
	Reason          string
	QueryVector     []float32
}

// Deduplicator handles memory deduplication via vector search + LLM.
type Deduplicator struct {
	store    *storage.Store
	embedder embedder.Embedder
	llm      llm.Client
}

const (
	similarityThreshold    = 0.0
	maxPromptSimilar       = 5
)

// NewDeduplicator creates a new deduplicator.
func NewDeduplicator(store *storage.Store, emb embedder.Embedder, llmClient llm.Client) *Deduplicator {
	return &Deduplicator{store: store, embedder: emb, llm: llmClient}
}

// Deduplicate decides how to handle a candidate memory against existing ones.
func (d *Deduplicator) Deduplicate(
	candidate CandidateMemory,
	reqCtx *ctx.RequestContext,
) (*DedupResult, error) {
	similar, queryVec, err := d.findSimilar(candidate, reqCtx)
	if err != nil {
		return nil, err
	}

	if len(similar) == 0 {
		return &DedupResult{
			Decision:        DecisionCreate,
			Candidate:       candidate,
			SimilarMemories: nil,
			Actions:         nil,
			Reason:          "No similar memories found",
			QueryVector:     queryVec,
		}, nil
	}

	decision, reason, actions := d.llmDecision(candidate, similar)

	return &DedupResult{
		Decision:        decision,
		Candidate:       candidate,
		SimilarMemories: similar,
		Actions:         actions,
		Reason:          reason,
		QueryVector:     queryVec,
	}, nil
}

func (d *Deduplicator) findSimilar(
	candidate CandidateMemory,
	reqCtx *ctx.RequestContext,
) ([]*ctx.Context, []float32, error) {
	if d.embedder == nil {
		return nil, nil, nil
	}

	queryText := candidate.Abstract + " " + candidate.Content
	result, err := d.embedder.Embed(queryText, true)
	if err != nil {
		return nil, nil, fmt.Errorf("embed candidate: %w", err)
	}
	queryVec := result.DenseVector

	catPrefix := categoryURIPrefix(string(candidate.Category), reqCtx)

	var filters []storage.FilterExpr
	if catPrefix != "" {
		filters = append(filters, storage.PathScope{Field: "uri", BasePath: catPrefix, Depth: -1})
	}

	ownerSpace := getOwnerSpace(candidate.Category, reqCtx)
	if ownerSpace != "" {
		filters = append(filters, storage.Eq{Field: "owner_space", Value: ownerSpace})
	}

	filter := storage.MergeFilters(filters...)

	results, err := d.store.VectorSearch(queryVec, filter, 10, nil)
	if err != nil {
		log.Printf("Vector search failed for dedup: %v", err)
		return nil, queryVec, nil
	}

	var similar []*ctx.Context
	for _, r := range results {
		similar = append(similar, r.Context)
	}

	return similar, queryVec, nil
}

func (d *Deduplicator) llmDecision(
	candidate CandidateMemory,
	similar []*ctx.Context,
) (DedupDecision, string, []ExistingMemoryAction) {
	if d.llm == nil {
		return DecisionCreate, "LLM not available", nil
	}

	limit := maxPromptSimilar
	if len(similar) < limit {
		limit = len(similar)
	}

	var existingLines []string
	for i, mem := range similar[:limit] {
		abstract := mem.Abstract
		existingLines = append(existingLines, fmt.Sprintf("%d. uri=%s\n   abstract=%s", i+1, mem.URI, abstract))
	}

	prompt := fmt.Sprintf(`You are a memory deduplication engine. Decide how to handle this new memory candidate.

New Candidate:
- Abstract: %s
- Overview: %s
- Content: %s

Existing Similar Memories:
%s

Decisions:
- "skip": The candidate is redundant, don't create it
- "create": The candidate adds new information, create it
- "none": Don't create candidate, but take actions on existing memories

For each existing memory, you can also specify actions:
- "merge": Merge candidate info into this existing memory
- "delete": Delete this outdated/conflicting memory

Return JSON:
{"decision": "skip|create|none", "reason": "...", "list": [{"uri": "...", "decide": "merge|delete", "reason": "..."}]}`,
		candidate.Abstract, candidate.Overview, candidate.Content,
		strings.Join(existingLines, "\n"))

	resp, err := d.llm.CompleteWithPrompt(prompt)
	if err != nil {
		log.Printf("LLM dedup decision failed: %v", err)
		return DecisionCreate, fmt.Sprintf("LLM failed: %v", err), nil
	}

	data, err := ParseJSONFromResponse(resp.Content)
	if err != nil {
		log.Printf("Parse dedup response failed: %v", err)
		return DecisionCreate, "Failed to parse LLM response", nil
	}

	return parseDecisionPayload(data, similar)
}

func parseDecisionPayload(
	data map[string]any,
	similar []*ctx.Context,
) (DedupDecision, string, []ExistingMemoryAction) {
	decisionStr := strings.ToLower(strings.TrimSpace(getStr(data, "decision")))
	reason := getStr(data, "reason")

	decisionMap := map[string]DedupDecision{
		"skip":   DecisionSkip,
		"create": DecisionCreate,
		"none":   DecisionNone,
		"merge":  DecisionNone,
	}
	decision, ok := decisionMap[decisionStr]
	if !ok {
		decision = DecisionCreate
	}

	rawActions, _ := data["list"].([]any)

	if decisionStr == "merge" && len(rawActions) == 0 && len(similar) > 0 {
		rawActions = []any{
			map[string]any{
				"uri":    similar[0].URI,
				"decide": "merge",
				"reason": "Legacy merge mapped to none",
			},
		}
	}

	actionMap := map[string]MemoryAction{
		"merge":  ActionMerge,
		"delete": ActionDelete,
	}

	similarByURI := make(map[string]*ctx.Context)
	for _, m := range similar {
		similarByURI[m.URI] = m
	}

	var actions []ExistingMemoryAction
	seen := make(map[string]MemoryAction)

	for _, itemRaw := range rawActions {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			continue
		}

		actionStr := strings.ToLower(strings.TrimSpace(getStr(item, "decide")))
		action, ok := actionMap[actionStr]
		if !ok {
			continue
		}

		var memory *ctx.Context
		if uri := getStr(item, "uri"); uri != "" {
			memory = similarByURI[uri]
		}
		if memory == nil {
			if idxRaw, ok := item["index"]; ok {
				if idx, ok := idxRaw.(float64); ok {
					i := int(idx)
					if i >= 1 && i <= len(similar) {
						memory = similar[i-1]
					} else if i >= 0 && i < len(similar) {
						memory = similar[i]
					}
				}
			}
		}
		if memory == nil {
			continue
		}

		prev, exists := seen[memory.URI]
		if exists && prev != action {
			var filtered []ExistingMemoryAction
			for _, a := range actions {
				if a.Memory.URI != memory.URI {
					filtered = append(filtered, a)
				}
			}
			actions = filtered
			delete(seen, memory.URI)
			continue
		}
		if exists {
			continue
		}

		seen[memory.URI] = action
		actions = append(actions, ExistingMemoryAction{
			Memory:   memory,
			Decision: action,
			Reason:   getStr(item, "reason"),
		})
	}

	if decision == DecisionSkip {
		return decision, reason, nil
	}

	hasMerge := false
	for _, a := range actions {
		if a.Decision == ActionMerge {
			hasMerge = true
			break
		}
	}

	if decision == DecisionCreate && hasMerge {
		decision = DecisionNone
		reason += " | normalized:create+merge->none"
	}

	if decision == DecisionCreate {
		var filtered []ExistingMemoryAction
		for _, a := range actions {
			if a.Decision == ActionDelete {
				filtered = append(filtered, a)
			}
		}
		actions = filtered
	}

	return decision, reason, actions
}

func categoryURIPrefix(category string, reqCtx *ctx.RequestContext) string {
	userCats := map[string]bool{
		"preferences": true,
		"entities":    true,
		"events":      true,
	}
	agentCats := map[string]bool{
		"cases":    true,
		"patterns": true,
		"tools":    true,
		"skills":   true,
	}

	if userCats[category] {
		return fmt.Sprintf("viking://user/%s/memories/%s/", reqCtx.User.UserSpaceName(), category)
	}
	if agentCats[category] {
		return fmt.Sprintf("viking://agent/%s/memories/%s/", reqCtx.User.AgentSpaceName(), category)
	}
	return ""
}

// CosineSimilarity computes cosine similarity between two float32 vectors.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		magA += float64(a[i]) * float64(a[i])
		magB += float64(b[i]) * float64(b[i])
	}
	magA = math.Sqrt(magA)
	magB = math.Sqrt(magB)
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (magA * magB)
}

// ExtractFacetKey extracts normalized facet key from abstract text.
func ExtractFacetKey(text string) string {
	if text == "" {
		return ""
	}
	normalized := strings.Join(strings.Fields(text), " ")
	for _, sep := range []string{"：", ":", "-", "—"} {
		if i := strings.Index(normalized, sep); i >= 0 {
			left := strings.TrimSpace(strings.ToLower(normalized[:i]))
			if left != "" {
				return left
			}
		}
	}
	re := regexp.MustCompile(`^(.{1,24})\s`)
	if m := re.FindStringSubmatch(strings.ToLower(normalized)); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	if len(normalized) > 24 {
		return strings.ToLower(normalized[:24])
	}
	return strings.ToLower(normalized)
}
