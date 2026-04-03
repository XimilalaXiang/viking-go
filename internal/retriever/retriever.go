package retriever

import (
	"container/heap"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/embedder"
	"github.com/ximilala/viking-go/internal/storage"
)

const (
	MaxConvergenceRounds = 3
	MaxRelations         = 5
	ScorePropAlpha       = 0.5
	GlobalSearchTopK     = 10
	HotnessAlpha         = 0.2
)

// TypedQuery represents a retrieval query with type and scope.
type TypedQuery struct {
	Query            string
	ContextType      string   // "memory", "skill", "resource", or "" for all
	Intent           string
	Priority         int
	TargetDirectories []string
}

// MatchedContext is a single search result.
type MatchedContext struct {
	URI         string           `json:"uri"`
	ContextType string           `json:"context_type"`
	Level       int              `json:"level"`
	Abstract    string           `json:"abstract"`
	Category    string           `json:"category"`
	Score       float64          `json:"score"`
	Relations   []RelatedContext `json:"relations,omitempty"`
}

// RelatedContext holds a related URI and its abstract.
type RelatedContext struct {
	URI      string `json:"uri"`
	Abstract string `json:"abstract"`
}

// QueryResult is the output of a retrieval operation.
type QueryResult struct {
	Query              TypedQuery       `json:"query"`
	MatchedContexts    []MatchedContext `json:"matched_contexts"`
	SearchedDirectories []string        `json:"searched_directories"`
}

// FindResult aggregates results by context type.
type FindResult struct {
	Memories  []MatchedContext `json:"memories"`
	Resources []MatchedContext `json:"resources"`
	Skills    []MatchedContext `json:"skills"`
}

// Total returns the total number of results across all types.
func (fr *FindResult) Total() int {
	return len(fr.Memories) + len(fr.Resources) + len(fr.Skills)
}

// HierarchicalRetriever implements directory-based hierarchical retrieval
// with BFS, score propagation, and optional reranking.
type HierarchicalRetriever struct {
	store    *storage.Store
	embedder embedder.Embedder
	reranker embedder.Reranker
	threshold float64
}

// NewHierarchicalRetriever creates a new retriever.
func NewHierarchicalRetriever(store *storage.Store, emb embedder.Embedder, reranker embedder.Reranker, threshold float64) *HierarchicalRetriever {
	return &HierarchicalRetriever{
		store:     store,
		embedder:  emb,
		reranker:  reranker,
		threshold: threshold,
	}
}

// Retrieve executes hierarchical retrieval.
func (hr *HierarchicalRetriever) Retrieve(query TypedQuery, reqCtx *ctx.RequestContext, limit int) (*QueryResult, error) {
	if limit <= 0 {
		limit = 5
	}

	if !hr.store.CollectionExists() {
		return &QueryResult{Query: query}, nil
	}

	// Step 1: Embed query
	var queryVec []float32
	if hr.embedder != nil {
		result, err := hr.embedder.Embed(query.Query, true)
		if err != nil {
			return nil, fmt.Errorf("embed query: %w", err)
		}
		queryVec = result.DenseVector
	}
	if len(queryVec) == 0 {
		return &QueryResult{Query: query}, nil
	}

	// Step 2: Build scope filter
	scopeFilter := hr.buildScopeFilter(query.ContextType, reqCtx)

	// Step 3: Global vector search
	globalK := limit
	if globalK < GlobalSearchTopK {
		globalK = GlobalSearchTopK
	}
	globalResults, err := hr.store.VectorSearch(queryVec, scopeFilter, globalK*3, nil)
	if err != nil {
		return nil, fmt.Errorf("global vector search: %w", err)
	}

	// Step 4: Determine starting points (L0/L1) and initial candidates (L2)
	targetDirs := query.TargetDirectories
	rootURIs := hr.getRootURIs(query.ContextType, reqCtx)

	var startingPoints []scoredURI
	var initialCandidates []candidateRecord
	seen := make(map[string]bool)

	for _, sr := range globalResults {
		level := sr.Context.GetLevel()
		uri := sr.Context.URI

		if level == ctx.LevelDetail {
			initialCandidates = append(initialCandidates, candidateRecord{
				uri:        uri,
				abstract:   sr.Context.Abstract,
				score:      sr.Score,
				finalScore: sr.Score,
				context:    sr.Context,
			})
		} else {
			if !seen[uri] {
				startingPoints = append(startingPoints, scoredURI{uri: uri, score: sr.Score})
				seen[uri] = true
			}
		}
	}

	// Add explicit root URIs
	for _, u := range rootURIs {
		if !seen[u] {
			startingPoints = append(startingPoints, scoredURI{uri: u, score: 0.0})
			seen[u] = true
		}
	}
	for _, u := range targetDirs {
		if u != "" && !seen[u] {
			startingPoints = append(startingPoints, scoredURI{uri: u, score: 0.0})
			seen[u] = true
		}
	}

	// Step 5: Rerank starting points and initial candidates
	if hr.reranker != nil && len(startingPoints) > 0 {
		startingPoints = hr.rerankScoredURIs(query.Query, startingPoints, globalResults)
	}
	if hr.reranker != nil && len(initialCandidates) > 0 {
		initialCandidates = hr.rerankCandidates(query.Query, initialCandidates)
	}

	// Step 6: BFS recursive search
	collected := hr.recursiveSearch(
		queryVec, query.Query, startingPoints, initialCandidates,
		limit, scopeFilter,
	)

	// Step 7: Convert to matched contexts with hotness boost
	matched := hr.toMatchedContexts(collected, limit)

	return &QueryResult{
		Query:              query,
		MatchedContexts:    matched,
		SearchedDirectories: rootURIs,
	}, nil
}

type scoredURI struct {
	uri   string
	score float64
}

type candidateRecord struct {
	uri        string
	abstract   string
	score      float64
	finalScore float64
	context    *ctx.Context
}

func (hr *HierarchicalRetriever) recursiveSearch(
	queryVec []float32,
	queryText string,
	startingPoints []scoredURI,
	initialCandidates []candidateRecord,
	limit int,
	scopeFilter storage.FilterExpr,
) map[string]candidateRecord {

	collected := make(map[string]candidateRecord)
	visited := make(map[string]bool)

	// Seed with initial L2 candidates
	for _, c := range initialCandidates {
		if c.uri != "" {
			prev, exists := collected[c.uri]
			if !exists || c.finalScore > prev.finalScore {
				collected[c.uri] = c
			}
		}
	}

	// BFS priority queue
	pq := &priorityQueue{}
	heap.Init(pq)
	for _, sp := range startingPoints {
		heap.Push(pq, &pqItem{uri: sp.uri, score: sp.score})
	}

	prevTopK := make(map[string]bool)
	convergenceRounds := 0

	for pq.Len() > 0 {
		item := heap.Pop(pq).(*pqItem)
		if visited[item.uri] {
			continue
		}
		visited[item.uri] = true
		parentScore := item.score

		// Search children of this directory
		childFilter := storage.MergeFilters(
			storage.Eq{Field: "parent_uri", Value: item.uri},
			scopeFilter,
		)
		preFilterLimit := limit * 2
		if preFilterLimit < 20 {
			preFilterLimit = 20
		}
		children, err := hr.store.VectorSearch(queryVec, childFilter, preFilterLimit, nil)
		if err != nil {
			log.Printf("[Retriever] search children of %s: %v", item.uri, err)
			continue
		}
		if len(children) == 0 {
			continue
		}

		// Rerank children if available
		childScores := make([]float64, len(children))
		for i, c := range children {
			childScores[i] = c.Score
		}
		if hr.reranker != nil {
			docs := make([]string, len(children))
			for i, c := range children {
				docs[i] = c.Context.Abstract
			}
			reranked, err := hr.reranker.Rerank(queryText, docs)
			if err == nil && len(reranked) == len(children) {
				childScores = reranked
			}
		}

		for i, sr := range children {
			uri := sr.Context.URI
			rawScore := childScores[i]

			// Score propagation: blend parent score
			finalScore := rawScore
			if parentScore > 0 {
				finalScore = ScorePropAlpha*rawScore + (1-ScorePropAlpha)*parentScore
			}

			if !isFinite(finalScore) {
				finalScore = 0
			}
			if finalScore <= hr.threshold {
				continue
			}

			prev, exists := collected[uri]
			if !exists || finalScore > prev.finalScore {
				collected[uri] = candidateRecord{
					uri:        uri,
					abstract:   sr.Context.Abstract,
					score:      rawScore,
					finalScore: finalScore,
					context:    sr.Context,
				}
			}

			// Recurse into directories (L0/L1)
			if !visited[uri] && sr.Context.GetLevel() != ctx.LevelDetail {
				heap.Push(pq, &pqItem{uri: uri, score: finalScore})
			}
		}

		// Convergence check
		topK := topKURIs(collected, limit)
		if setsEqual(topK, prevTopK) && len(topK) >= limit {
			convergenceRounds++
			if convergenceRounds >= MaxConvergenceRounds {
				break
			}
		} else {
			convergenceRounds = 0
			prevTopK = topK
		}
	}

	return collected
}

func (hr *HierarchicalRetriever) toMatchedContexts(collected map[string]candidateRecord, limit int) []MatchedContext {
	type scored struct {
		mc    MatchedContext
		final float64
	}
	var items []scored
	for _, c := range collected {
		semanticScore := c.finalScore
		if !isFinite(semanticScore) {
			semanticScore = 0
		}

		hScore := hotnessScore(c.context)
		final := (1-HotnessAlpha)*semanticScore + HotnessAlpha*hScore
		if !isFinite(final) {
			final = 0
		}

		level := 2
		if c.context != nil && c.context.Level != nil {
			level = *c.context.Level
		}

		mc := MatchedContext{
			URI:         appendLevelSuffix(c.uri, level),
			ContextType: c.context.ContextType,
			Level:       level,
			Abstract:    c.abstract,
			Category:    c.context.Category,
			Score:       final,
		}
		items = append(items, scored{mc: mc, final: final})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].final > items[j].final
	})

	if len(items) > limit {
		items = items[:limit]
	}
	result := make([]MatchedContext, len(items))
	for i, s := range items {
		result[i] = s.mc
	}
	return result
}

func (hr *HierarchicalRetriever) buildScopeFilter(contextType string, reqCtx *ctx.RequestContext) storage.FilterExpr {
	var filters []storage.FilterExpr

	if contextType != "" {
		filters = append(filters, storage.Eq{Field: "context_type", Value: contextType})
	}

	if reqCtx != nil && reqCtx.AccountID != "" && reqCtx.Role != ctx.RoleRoot {
		filters = append(filters, storage.Eq{Field: "account_id", Value: reqCtx.AccountID})
	}

	return storage.MergeFilters(filters...)
}

func (hr *HierarchicalRetriever) getRootURIs(contextType string, reqCtx *ctx.RequestContext) []string {
	if reqCtx == nil || reqCtx.Role == ctx.RoleRoot {
		return nil
	}
	if reqCtx.User == nil {
		return nil
	}

	userSpace := reqCtx.User.UserSpaceName()
	agentSpace := reqCtx.User.AgentSpaceName()

	switch contextType {
	case "memory":
		return []string{
			"viking://user/" + userSpace + "/memories",
			"viking://agent/" + agentSpace + "/memories",
		}
	case "resource":
		return []string{"viking://resources"}
	case "skill":
		return []string{"viking://agent/" + agentSpace + "/skills"}
	default:
		return []string{
			"viking://user/" + userSpace + "/memories",
			"viking://agent/" + agentSpace + "/memories",
			"viking://resources",
			"viking://agent/" + agentSpace + "/skills",
		}
	}
}

func (hr *HierarchicalRetriever) rerankScoredURIs(query string, points []scoredURI, globalResults []storage.SearchResult) []scoredURI {
	docs := make([]string, len(points))
	fallback := make([]float64, len(points))
	uriToAbstract := make(map[string]string)
	for _, gr := range globalResults {
		uriToAbstract[gr.Context.URI] = gr.Context.Abstract
	}
	for i, p := range points {
		docs[i] = uriToAbstract[p.uri]
		fallback[i] = p.score
	}
	scores, err := hr.reranker.Rerank(query, docs)
	if err != nil || len(scores) != len(points) {
		return points
	}
	for i := range points {
		points[i].score = scores[i]
	}
	return points
}

func (hr *HierarchicalRetriever) rerankCandidates(query string, candidates []candidateRecord) []candidateRecord {
	docs := make([]string, len(candidates))
	for i, c := range candidates {
		docs[i] = c.abstract
	}
	scores, err := hr.reranker.Rerank(query, docs)
	if err != nil || len(scores) != len(candidates) {
		return candidates
	}
	for i := range candidates {
		candidates[i].score = scores[i]
		candidates[i].finalScore = scores[i]
	}
	return candidates
}

func hotnessScore(c *ctx.Context) float64 {
	if c == nil {
		return 0
	}
	ac := float64(c.ActiveCount)
	recency := 0.0
	if !c.UpdatedAt.IsZero() {
		hoursSince := time.Since(c.UpdatedAt).Hours()
		if hoursSince < 1 {
			hoursSince = 1
		}
		recency = 1.0 / math.Log2(hoursSince+2)
	}
	return math.Min(1.0, 0.3*math.Log1p(ac)+0.7*recency)
}

var levelURISuffix = map[int]string{
	0: ".abstract.md",
	1: ".overview.md",
}

func appendLevelSuffix(uri string, level int) string {
	suffix, ok := levelURISuffix[level]
	if !ok || uri == "" {
		return uri
	}
	if strings.HasSuffix(uri, "/"+suffix) {
		return uri
	}
	if strings.HasSuffix(uri, "/.abstract.md") || strings.HasSuffix(uri, "/.overview.md") {
		return uri
	}
	uri = strings.TrimRight(uri, "/")
	return uri + "/" + suffix
}

func topKURIs(collected map[string]candidateRecord, k int) map[string]bool {
	type kv struct {
		uri   string
		score float64
	}
	var items []kv
	for u, c := range collected {
		items = append(items, kv{uri: u, score: c.finalScore})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].score > items[j].score
	})
	result := make(map[string]bool)
	for i, item := range items {
		if i >= k {
			break
		}
		result[item.uri] = true
	}
	return result
}

func setsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// --- Priority queue for BFS ---

type pqItem struct {
	uri   string
	score float64
	index int
}

type priorityQueue []*pqItem

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].score > pq[j].score } // max-heap
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}
func (pq *priorityQueue) Push(x any) {
	item := x.(*pqItem)
	item.index = len(*pq)
	*pq = append(*pq, item)
}
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[:n-1]
	return item
}

func isFinite(f float64) bool {
	return !math.IsInf(f, 0) && !math.IsNaN(f)
}
