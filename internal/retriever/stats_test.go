package retriever

import (
	"math"
	"sync"
	"testing"
)

func TestRetrievalStatsBasic(t *testing.T) {
	s := &RetrievalStats{
		MinScore:      math.Inf(1),
		QueriesByType: make(map[string]int64),
	}

	s.RecordQuery("memory", 3, []float64{0.9, 0.8, 0.7}, 50.0, false)
	s.RecordQuery("resource", 0, nil, 30.0, true)

	if s.TotalQueries != 2 {
		t.Errorf("expected 2 queries, got %d", s.TotalQueries)
	}
	if s.TotalResults != 3 {
		t.Errorf("expected 3 results, got %d", s.TotalResults)
	}
	if s.ZeroResultQueries != 1 {
		t.Errorf("expected 1 zero-result query, got %d", s.ZeroResultQueries)
	}
	if s.MaxScore != 0.9 {
		t.Errorf("expected max_score=0.9, got %f", s.MaxScore)
	}
	if s.MinScore != 0.7 {
		t.Errorf("expected min_score=0.7, got %f", s.MinScore)
	}
	if s.RerankUsed != 1 {
		t.Errorf("expected rerank_used=1, got %d", s.RerankUsed)
	}
	if s.QueriesByType["memory"] != 1 {
		t.Errorf("expected memory count=1, got %d", s.QueriesByType["memory"])
	}
	if s.QueriesByType["resource"] != 1 {
		t.Errorf("expected resource count=1, got %d", s.QueriesByType["resource"])
	}
}

func TestRetrievalStatsAverages(t *testing.T) {
	s := &RetrievalStats{
		MinScore:      math.Inf(1),
		QueriesByType: make(map[string]int64),
	}

	s.RecordQuery("test", 2, []float64{1.0, 0.5}, 100.0, false)
	s.RecordQuery("test", 2, []float64{0.8, 0.3}, 200.0, false)

	avg := s.AvgResultsPerQuery()
	if avg != 2.0 {
		t.Errorf("expected avg 2.0, got %f", avg)
	}

	avgScore := s.AvgScore()
	expected := (1.0 + 0.5 + 0.8 + 0.3) / 4.0
	if math.Abs(avgScore-expected) > 0.001 {
		t.Errorf("expected avg score %f, got %f", expected, avgScore)
	}

	avgLatency := s.AvgLatencyMS()
	if avgLatency != 150.0 {
		t.Errorf("expected avg latency 150, got %f", avgLatency)
	}
}

func TestRetrievalStatsZeroQueries(t *testing.T) {
	s := &RetrievalStats{
		MinScore:      math.Inf(1),
		QueriesByType: make(map[string]int64),
	}

	if s.AvgResultsPerQuery() != 0 {
		t.Error("expected 0 for empty stats")
	}
	if s.ZeroResultRate() != 0 {
		t.Error("expected 0 for empty stats")
	}
	if s.AvgScore() != 0 {
		t.Error("expected 0 for empty stats")
	}
	if s.AvgLatencyMS() != 0 {
		t.Error("expected 0 for empty stats")
	}
}

func TestRetrievalStatsConcurrency(t *testing.T) {
	s := &RetrievalStats{
		MinScore:      math.Inf(1),
		QueriesByType: make(map[string]int64),
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.RecordQuery("test", 1, []float64{0.5}, 10.0, false)
		}()
	}
	wg.Wait()

	if s.TotalQueries != 100 {
		t.Errorf("expected 100 queries, got %d", s.TotalQueries)
	}
}

func TestRetrievalStatsToDict(t *testing.T) {
	s := &RetrievalStats{
		MinScore:      math.Inf(1),
		QueriesByType: make(map[string]int64),
	}

	d := s.ToDict()
	if d["min_score"].(float64) != 0 {
		t.Error("expected min_score=0 for empty stats in ToDict")
	}

	s.RecordQuery("test", 1, []float64{0.5}, 10.0, false)
	d = s.ToDict()
	if d["total_queries"].(int64) != 1 {
		t.Error("expected total_queries=1")
	}
}

func TestGetStatsCollector(t *testing.T) {
	c := GetStatsCollector()
	if c == nil {
		t.Error("expected non-nil stats collector")
	}
	c2 := GetStatsCollector()
	if c != c2 {
		t.Error("expected singleton")
	}
}
