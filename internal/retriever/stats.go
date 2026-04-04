package retriever

import (
	"math"
	"sync"
)

// RetrievalStats collects per-query metrics for health monitoring.
type RetrievalStats struct {
	mu                sync.Mutex
	TotalQueries      int64              `json:"total_queries"`
	TotalResults      int64              `json:"total_results"`
	ZeroResultQueries int64              `json:"zero_result_queries"`
	TotalScoreSum     float64            `json:"total_score_sum"`
	MaxScore          float64            `json:"max_score"`
	MinScore          float64            `json:"min_score"`
	QueriesByType     map[string]int64   `json:"queries_by_type"`
	RerankUsed        int64              `json:"rerank_used"`
	RerankFallback    int64              `json:"rerank_fallback"`
	TotalLatencyMS    float64            `json:"total_latency_ms"`
	MaxLatencyMS      float64            `json:"max_latency_ms"`
}

var (
	globalStats     *RetrievalStats
	globalStatsOnce sync.Once
)

// GetStatsCollector returns the global singleton stats collector.
func GetStatsCollector() *RetrievalStats {
	globalStatsOnce.Do(func() {
		globalStats = &RetrievalStats{
			MinScore:      math.Inf(1),
			QueriesByType: make(map[string]int64),
		}
	})
	return globalStats
}

// RecordQuery records metrics from a single retrieval query.
func (s *RetrievalStats) RecordQuery(queryType string, resultCount int, scores []float64, latencyMS float64, usedRerank bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.TotalQueries++
	s.TotalResults += int64(resultCount)
	if resultCount == 0 {
		s.ZeroResultQueries++
	}

	for _, score := range scores {
		s.TotalScoreSum += score
		if score > s.MaxScore {
			s.MaxScore = score
		}
		if score < s.MinScore {
			s.MinScore = score
		}
	}

	s.QueriesByType[queryType]++
	if usedRerank {
		s.RerankUsed++
	}

	s.TotalLatencyMS += latencyMS
	if latencyMS > s.MaxLatencyMS {
		s.MaxLatencyMS = latencyMS
	}
}

// AvgResultsPerQuery returns the mean number of results per query.
func (s *RetrievalStats) AvgResultsPerQuery() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.avgResultsPerQueryLocked()
}

func (s *RetrievalStats) avgResultsPerQueryLocked() float64 {
	if s.TotalQueries == 0 {
		return 0
	}
	return float64(s.TotalResults) / float64(s.TotalQueries)
}

// ZeroResultRate returns the fraction of queries that returned 0 results.
func (s *RetrievalStats) ZeroResultRate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zeroResultRateLocked()
}

func (s *RetrievalStats) zeroResultRateLocked() float64 {
	if s.TotalQueries == 0 {
		return 0
	}
	return float64(s.ZeroResultQueries) / float64(s.TotalQueries)
}

// AvgScore returns the mean relevance score across all results.
func (s *RetrievalStats) AvgScore() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.avgScoreLocked()
}

func (s *RetrievalStats) avgScoreLocked() float64 {
	if s.TotalResults == 0 {
		return 0
	}
	return s.TotalScoreSum / float64(s.TotalResults)
}

// AvgLatencyMS returns the mean query latency.
func (s *RetrievalStats) AvgLatencyMS() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.avgLatencyMSLocked()
}

func (s *RetrievalStats) avgLatencyMSLocked() float64 {
	if s.TotalQueries == 0 {
		return 0
	}
	return s.TotalLatencyMS / float64(s.TotalQueries)
}

// ToDict returns a JSON-serializable snapshot of the stats.
func (s *RetrievalStats) ToDict() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	minScore := s.MinScore
	if math.IsInf(minScore, 1) {
		minScore = 0
	}

	return map[string]any{
		"total_queries":         s.TotalQueries,
		"total_results":         s.TotalResults,
		"zero_result_queries":   s.ZeroResultQueries,
		"zero_result_rate":      s.zeroResultRateLocked(),
		"avg_results_per_query": s.avgResultsPerQueryLocked(),
		"avg_score":             s.avgScoreLocked(),
		"max_score":             s.MaxScore,
		"min_score":             minScore,
		"queries_by_type":       s.QueriesByType,
		"rerank_used":           s.RerankUsed,
		"rerank_fallback":       s.RerankFallback,
		"total_latency_ms":      s.TotalLatencyMS,
		"max_latency_ms":        s.MaxLatencyMS,
		"avg_latency_ms":        s.avgLatencyMSLocked(),
	}
}
